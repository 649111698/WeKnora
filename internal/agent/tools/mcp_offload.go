package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
)

// MCP 大结果自动"落表"：MCP 工具返回的结构化大结果不再全量拼进 LLM
// 上下文，而是物化为 DuckDB 只读表。模型只拿到行数/列结构/样例行与分页
// 完整性信息，随后用 data_analysis 工具写 SQL 聚合分析，上下文里只回
// 小结果。同一查询的分页结果（去掉分页键后参数一致的调用）自动追加进
// 同一张表，模型不必 UNION 多张分页表。

const (
	// mcpOffloadMinBytes 与 mcpOffloadMinRows 同时满足才落表；小结果直接
	// 给模型反而更高效（省一轮 SQL 往返）。
	mcpOffloadMinBytes = 16 * 1024
	mcpOffloadMinRows  = 20
	// mcpOffloadSampleRows 是摘要里带给模型的样例行数。
	mcpOffloadSampleRows = 3
	// mcpOffloadMaxCellBytes 截断超长单元格，避免单字段巨文撑爆建表流程。
	mcpOffloadMaxCellBytes = 64 * 1024
	// mcpOffloadInsertBatch 是批量 INSERT 每批的行数。
	mcpOffloadInsertBatch = 200
	// mcpOffloadSearchDepth 是在外层 JSON 里递归寻找记录数组的最大深度
	// （真实 MCP 返回常是 {"code":0,"data":{"records":[...]}} 这类多层包装）。
	mcpOffloadSearchDepth = 4
	// mcpOffloadFlattenDepth 是行内嵌套对象摊平成点路径列的最大深度。
	mcpOffloadFlattenDepth = 3
	// mcpOffloadMaxColumns 限制摊平后的列数上限；超过则退化为"嵌套对象
	// 整体存 JSON 字符串"的浅摊平，防止宽对象炸出几百列。
	mcpOffloadMaxColumns = 128
	// mcpOffloadChildDepthMax 是数组列递归拆子表的硬安全上限（防自引用/
	// 病态深嵌套）；正常数据在到达它之前就按实际数据量决定去留。
	mcpOffloadChildDepthMax = 4
	// mcpOffloadChildMinBytes 子树序列化后低于该值就不单独建表：数据量
	// 这么小的嵌套，样例行 + JSON 原文列已够模型理解，拆表徒增噪声。
	mcpOffloadChildMinBytes = 2048
	// mcpOffloadCellBudget 全部物化表累计单元格数上限，防数组嵌套笛卡尔
	// 式膨胀拖垮内存/建表耗时。
	mcpOffloadCellBudget = 2_000_000
)

// paginationKeys 是"分页参数"键：指纹时剔除它们，同一查询的不同页就归
// 并到同一张表；页幂等键则保留全部参数。
var paginationKeys = map[string]bool{
	"pageNo": true, "page_no": true, "page": true, "pageIndex": true, "page_index": true,
	"pageNum": true, "page_num": true, "offset": true, "skip": true, "cursor": true,
	"pageSize": true, "page_size": true, "limit": true, "size": true,
}

type offloadedTable struct {
	name         string
	columns      []ColumnInfo
	rowKeys      []string // 与 columns 对齐的行内键名
	rows         int64
	fingerprint  string
	totalCount   int
	nextRow      int64
	childCols    []string
	childNextRow map[string]int64
}

// MCPOffloadStore 追踪一次问答请求内物化的 MCP 表。每个 agent engine
// 各建一个；实现 types.Cleanable，请求结束时由 ToolRegistry.Cleanup
// 统一 DROP（幂等，多个 MCP 工具共享同一 store 也只会清一次）。
type MCPOffloadStore struct {
	mu      sync.Mutex
	db      *sql.DB
	tables  map[string]*offloadedTable
	byQuery map[string]string          // toolName+query 指纹 → 表名
	pages   map[string]map[string]bool // 表名 → 已加载的页指纹
	seq     int
	cells   int64
	cleaned bool
}

// NewMCPOffloadStore 建立会话级落表存储；db 为 nil 时所有方法退化为空操作。
func NewMCPOffloadStore(db *sql.DB) *MCPOffloadStore {
	return &MCPOffloadStore{
		db:      db,
		tables:  map[string]*offloadedTable{},
		byQuery: map[string]string{},
		pages:   map[string]map[string]bool{},
	}
}

// Cleanup 实现 types.Cleanable：DROP 本次请求物化的全部表。
func (s *MCPOffloadStore) Cleanup(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleaned || s.db == nil {
		s.cleaned = true
		return
	}
	for name := range s.tables {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %q`, name)); err != nil {
			logger.GetLogger(ctx).Warnf("MCP offload: drop table %s failed: %v", name, err)
		}
	}
	s.tables = map[string]*offloadedTable{}
	s.byQuery = map[string]string{}
	s.pages = map[string]map[string]bool{}
	s.cleaned = true
}

// Has 报告 name 是否为本请求物化的 MCP 表（data_analysis 用它放行）。
func (s *MCPOffloadStore) Has(name string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tables[name]
	return ok
}

// Schema 返回物化表的表结构；不存在时返回 nil。
func (s *MCPOffloadStore) Schema(name string) *TableSchema {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[name]
	if !ok {
		return nil
	}
	return &TableSchema{TableName: t.name, Columns: t.columns, RowCount: t.rows}
}

// TryOffload 判断 text 是否"大而表格化"，是则物化为 DuckDB 表并返回给
// 模型的摘要；不适合落表时 ok=false，调用方保留原文。args 是本次工具
// 调用参数：去掉分页键后的指纹相同的多次调用视为同一查询，结果行追加
// 进同一张表（先探总数、再翻页拉数的两段式用法自然合并）。
func (s *MCPOffloadStore) TryOffload(ctx context.Context, toolName string, args json.RawMessage, text string) (string, bool) {
	if s == nil || s.db == nil {
		return "", false
	}
	if len(text) < mcpOffloadMinBytes {
		return "", false
	}
	rows, meta, ok := extractTabularRows(text)
	if !ok || len(rows) < mcpOffloadMinRows {
		return "", false
	}

	queryFP := argsFingerprint(args, true)
	pageKey := argsFingerprint(args, false)

	s.mu.Lock()
	existing := s.byQuery[toolName+"\x00"+queryFP]
	var tbl *offloadedTable
	if existing != "" {
		tbl = s.tables[existing]
	}
	if tbl != nil && s.pages[tbl.name][pageKey] {
		// 同一页重复加载：幂等跳过，防模型重试造成重复行。
		s.mu.Unlock()
		return alreadyLoadedSummary(tbl), true
	}
	if tbl != nil && tbl.totalCount > 0 && tbl.rows >= int64(tbl.totalCount) {
		// 表已含全量数据：拒绝继续拉页（配合完整性提示消除无谓翻页）。
		s.pages[tbl.name][pageKey] = true
		total := tbl.rows
		name := tbl.name
		s.mu.Unlock()
		return fmt.Sprintf("[Offloaded data] Table `%s` already contains ALL %d rows (totalCount=%d) — the dataset is complete. Do NOT fetch more pages; analyze `%s` with the `data_analysis` tool.", name, total, total, name), true
	}
	s.mu.Unlock()

	var summary string
	var err error
	if tbl != nil {
		summary, err = s.appendRows(ctx, tbl, rows, meta)
	} else {
		summary, err = s.createFresh(ctx, toolName, rows, meta, queryFP, pageKey)
	}
	if err != nil {
		logger.GetLogger(ctx).Warnf("MCP offload: materialize %s failed (%d rows), falling back to inline result: %v", toolName, len(rows), err)
		return "", false
	}
	return summary, true
}

// ---- 摘要 ----

func completenessLine(loaded int64, total int) string {
	if total > 0 {
		if loaded >= int64(total) {
			return fmt.Sprintf("Completeness: ALL %d rows of totalCount=%d are loaded — the dataset is COMPLETE. Do NOT fetch more pages.", loaded, total)
		}
		return fmt.Sprintf("Completeness: PARTIAL — %d of totalCount=%d rows loaded. Fetch the remaining pages with the SAME query; each page is appended into this same table automatically.", loaded, total)
	}
	return "Completeness unknown — if the source paginates, keep fetching pages with the same query; they append into this same table."
}

func analyzeHint(name string) string {
	return fmt.Sprintf("Analyze it with the `data_analysis` tool: knowledge_id = \"%s\", sql = SELECT-only statements. Do not fetch the raw rows again — they will not fit in context.", name)
}

func alreadyLoadedSummary(t *offloadedTable) string {
	return fmt.Sprintf("[Offloaded data] This page is already loaded into table `%s` (%d rows). Analyze the existing table; do not re-fetch this page.", t.name, t.rows)
}

// ---- 参数指纹 ----

// argsFingerprint 对调用参数做稳定哈希；stripPagination 为 true 时剔除分
// 页键（同一查询的不同页归并），否则保留全部参数（页幂等键）。
func argsFingerprint(args json.RawMessage, stripPagination bool) string {
	if len(args) == 0 {
		return "-"
	}
	var m map[string]interface{}
	if err := json.Unmarshal(args, &m); err == nil {
		if stripPagination {
			for k := range paginationKeys {
				delete(m, k)
			}
		}
		if b, err := json.Marshal(m); err == nil {
			args = b
		}
	}
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:])[:16]
}

// ---- 结构识别 ----

// offloadMeta 携带分页元信息（数组直接父对象上的 totalCount 等）。
type offloadMeta struct {
	totalCount int
}

// extractTabularRows 从 JSON 文本中抽出记录数组：顶层是数组，或在多层
// 包装对象里递归找最大的对象数组（{"code":0,"data":{"records":[...]}}、
// {"result":{"list":[...]}} 等真实 MCP 常见形态）。
func extractTabularRows(text string) ([]map[string]interface{}, offloadMeta, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		return nil, offloadMeta{}, false
	}
	var top interface{}
	if err := json.Unmarshal([]byte(trimmed), &top); err != nil {
		return nil, offloadMeta{}, false
	}
	best, bestLen, meta := findLargestRowArray(top, 0)
	if bestLen == 0 {
		return nil, offloadMeta{}, false
	}
	return best, meta, true
}

// findLargestRowArray 深度优先递归（上限 mcpOffloadSearchDepth），返回节
// 点子树里行数最多的对象数组；totalCount 取离数组最近的父对象上的值。
func findLargestRowArray(v interface{}, depth int) ([]map[string]interface{}, int, offloadMeta) {
	if rows, ok := rowsFromArray(v); ok {
		return rows, len(rows), offloadMeta{}
	}
	if depth >= mcpOffloadSearchDepth {
		return nil, 0, offloadMeta{}
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return nil, 0, offloadMeta{}
	}
	best, bestLen := []map[string]interface{}{}, 0
	var bestMeta offloadMeta
	// map 遍历无序，按 key 排序保证同结构输入选出同一数组（确定性）。
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rows, n, meta := findLargestRowArray(obj[k], depth+1)
		if n > bestLen {
			best, bestLen, bestMeta = rows, n, meta
			// 更深递归已捕获 totalCount 时保留（离数组更近的父对象）；
			// 否则当前对象可能正是数组的直接父对象。
			if bestMeta.totalCount == 0 {
				bestMeta.totalCount = siblingTotalCount(obj)
			}
		}
	}
	return best, bestLen, bestMeta
}

// siblingTotalCount 读包装对象上的分页总数字段（totalCount/total/...）。
func siblingTotalCount(obj map[string]interface{}) int {
	for _, k := range []string{"totalCount", "total_count", "total", "totalcount"} {
		if f, ok := obj[k].(float64); ok && f > 0 && f == float64(int(f)) {
			return int(f)
		}
	}
	return 0
}

// rowsFromArray 把 []interface{} 归一为记录切片：元素是对象则原样；
// 是标量则包成 {"value": x}（保持可 SQL 化）。
func rowsFromArray(v interface{}) ([]map[string]interface{}, bool) {
	arr, ok := v.([]interface{})
	if !ok || len(arr) == 0 {
		return nil, false
	}
	rows := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		switch row := item.(type) {
		case map[string]interface{}:
			rows = append(rows, row)
		case nil:
			return nil, false
		default:
			rows = append(rows, map[string]interface{}{"value": row})
		}
	}
	return rows, true
}

// childElements 归一取出某行某列的子记录：数组取其中的对象元素，单对象
// 视为一元素数组（真实 MCP 的同名字段可能时为数组、时为单对象、时为
// null），null/标量返回空。
func childElements(v interface{}) []map[string]interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return []map[string]interface{}{val}
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(val))
		for _, e := range val {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// detectChildCols 在原始行上识别"子记录列"：多数行有子记录（数组或单对
// 象归一），且满足其一——某行出现 ≥2 元素的数组（确有一对多），或子树
// 总量已达单独建表的数据量阈值（如每处单元素但内容多）。
// 每行只是单对象且数据量小的列是一对一小字段，走点路径摊平更合适。
func detectChildCols(rows []map[string]interface{}) []string {
	type colStat struct {
		count     int
		haveMulti bool
		elems     []map[string]interface{}
	}
	stats := map[string]*colStat{}
	for _, row := range rows {
		for k, v := range row {
			elems := childElements(v)
			if len(elems) == 0 {
				continue
			}
			st := stats[k]
			if st == nil {
				st = &colStat{}
				stats[k] = st
			}
			st.count++
			st.elems = append(st.elems, elems...)
			if arr, ok := v.([]interface{}); ok && len(arr) >= 2 {
				st.haveMulti = true
			}
		}
	}
	majority := len(rows)/2 + 1
	cols := make([]string, 0, len(stats))
	for k, st := range stats {
		if st.count >= majority && (st.haveMulti || estimateRowsBytes(st.elems) >= mcpOffloadChildMinBytes) {
			cols = append(cols, k)
		}
	}
	sort.Strings(cols)
	return cols
}

// ---- 建表 / 追加 ----

// createFresh 首次落表：建主表并按实际数据量递归拆子表，登记指纹与页。
func (s *MCPOffloadStore) createFresh(ctx context.Context, toolName string, rows []map[string]interface{}, meta offloadMeta, queryFP, pageKey string) (string, error) {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	base := sanitizeName(toolName)
	if len(base) > 40 {
		base = base[:40]
	}
	table := fmt.Sprintf("%s_t%d", base, seq)

	desc := tableDesc{}
	if err := s.buildTable(ctx, table, rows, 0, &desc); err != nil {
		return "", err
	}

	s.mu.Lock()
	t := s.tables[table]
	t.fingerprint = queryFP
	s.byQuery[toolName+"\x00"+queryFP] = table
	if t.totalCount < meta.totalCount {
		t.totalCount = meta.totalCount
	}
	s.markPageLocked(table, pageKey)
	s.mu.Unlock()

	logger.GetLogger(ctx).Infof("MCP offload: %s -> table %s (%d rows, totalCount=%d)", toolName, table, t.rows, t.totalCount)
	return s.renderFreshSummary(desc, rows, meta, t), nil
}

// appendRows 同一查询的后续分页：行追加进既有主表（新列 ALTER 补齐），
// 子记录列同样追加进既有子表；_row/_prow 序号接续。
func (s *MCPOffloadStore) appendRows(ctx context.Context, t *offloadedTable, rows []map[string]interface{}, meta offloadMeta) (string, error) {
	// 子记录列 = 既有列 ∪ 本批新列（新列懒建子表）。
	newChildCols := detectChildCols(rows)
	skip := map[string]bool{}
	for _, c := range t.childCols {
		skip[c] = true
	}
	for _, c := range newChildCols {
		skip[c] = true
	}
	flatRows, _, _ := flattenAll(rows, skip)

	// 补新列（ALTER ADD），保持 rowKeys 与 columns 对齐。
	s.mu.Lock()
	colIdx := map[string]int{}
	for i, k := range t.rowKeys {
		colIdx[k] = i
	}
	seenNew := make([]string, 0)
	_ = seenNew
	var newCols []ColumnInfo
	var newKeys []string
	for _, row := range flatRows {
		for k, v := range row {
			if _, ok := colIdx[k]; ok {
				continue
			}
			colIdx[k] = len(t.columns) + len(newKeys)
			kinds := map[string]bool{}
			switch v.(type) {
			case bool:
				kinds["bool"] = true
			case float64:
				if isIntegralFloat(v.(float64)) {
					kinds["int"] = true
				} else {
					kinds["float"] = true
				}
			case string:
				kinds["str"] = true
			}
			newCols = append(newCols, ColumnInfo{Name: k, Type: inferDuckType(kinds)})
			newKeys = append(newKeys, k)
		}
	}
	for i, c := range newCols {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteIdent(t.name), quoteIdent(c.Name), c.Type)); err != nil {
			s.mu.Unlock()
			return "", fmt.Errorf("alter table: %w", err)
		}
		t.columns = append(t.columns, c)
		t.rowKeys = append(t.rowKeys, newKeys[i])
	}
	total := meta.totalCount
	if t.totalCount > total {
		total = t.totalCount
	}
	t.totalCount = total
	// 行号接续。
	offset := t.nextRow
	for i, row := range flatRows {
		row["_row"] = float64(offset + int64(i) + 1)
	}
	s.mu.Unlock()

	cols, keys := t.columns, t.rowKeys
	if err := insertRows(ctx, s.db, t.name, cols, keys, flatRows); err != nil {
		return "", err
	}

	// 子表追加/懒建。
	for _, col := range append(append([]string{}, t.childCols...), newChildCols...) {
		childRows := collectChildRows(flatRows, col)
		if len(childRows) == 0 {
			continue
		}
		childName := childTableName(t.name, col)
		if s.Has(childName) {
			if err := s.appendChild(ctx, childName, childRows); err != nil {
				logger.GetLogger(ctx).Warnf("MCP offload: append child %s failed: %v", childName, err)
			}
			continue
		}
		childDesc := &tableDesc{}
		if err := s.buildTable(ctx, childName, childRows, 1, childDesc); err != nil {
			logger.GetLogger(ctx).Warnf("MCP offload: child table %s failed: %v", childName, err)
			continue
		}
	}

	s.mu.Lock()
	t.rows += int64(len(flatRows))
	t.nextRow = offset + int64(len(flatRows))
	if !containsString(t.childCols, "") {
		// no-op 占位，保持锁语义清晰
	}
	for _, c := range newChildCols {
		if !containsString(t.childCols, c) {
			t.childCols = append(t.childCols, c)
		}
	}
	rowCount := t.rows
	s.cells += int64(len(flatRows)) * int64(len(cols))
	s.mu.Unlock()

	logger.GetLogger(ctx).Infof("MCP offload: appended %d rows to %s (now %d, totalCount=%d)", len(flatRows), t.name, rowCount, total)
	var b strings.Builder
	fmt.Fprintf(&b, "[Offloaded data] Appended %d rows into existing table `%s` — now %d rows total.\n", len(flatRows), t.name, rowCount)
	fmt.Fprintf(&b, "%s\n%s", completenessLine(rowCount, total), analyzeHint(t.name))
	return b.String(), nil
}

// appendChild 把子记录行追加进既有子表（行号接续，新列 ALTER）。
func (s *MCPOffloadStore) appendChild(ctx context.Context, childName string, childRows []map[string]interface{}) error {
	s.mu.Lock()
	c := s.tables[childName]
	if c == nil {
		s.mu.Unlock()
		return fmt.Errorf("child table %s missing", childName)
	}
	colIdx := map[string]int{}
	for i, k := range c.rowKeys {
		colIdx[k] = i
	}
	var newCols []ColumnInfo
	var newKeys []string
	for _, row := range childRows {
		for k, v := range row {
			if _, ok := colIdx[k]; ok {
				continue
			}
			colIdx[k] = len(c.columns) + len(newKeys)
			kinds := map[string]bool{}
			switch v.(type) {
			case bool:
				kinds["bool"] = true
			case float64:
				if isIntegralFloat(v.(float64)) {
					kinds["int"] = true
				} else {
					kinds["float"] = true
				}
			case string:
				kinds["str"] = true
			}
			newCols = append(newCols, ColumnInfo{Name: k, Type: inferDuckType(kinds)})
			newKeys = append(newKeys, k)
		}
	}
	for i, col := range newCols {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteIdent(childName), quoteIdent(col.Name), col.Type)); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("alter child: %w", err)
		}
		c.columns = append(c.columns, col)
		c.rowKeys = append(c.rowKeys, newKeys[i])
	}
	offset := c.nextRow
	for i, row := range childRows {
		row["_row"] = float64(offset + int64(i) + 1)
	}
	s.mu.Unlock()

	if err := insertRows(ctx, s.db, childName, c.columns, c.rowKeys, childRows); err != nil {
		return err
	}
	s.mu.Lock()
	c.rows += int64(len(childRows))
	c.nextRow = offset + int64(len(childRows))
	s.cells += int64(len(childRows)) * int64(len(c.columns))
	s.mu.Unlock()
	return nil
}

func (s *MCPOffloadStore) markPageLocked(table, pageKey string) {
	if s.pages[table] == nil {
		s.pages[table] = map[string]bool{}
	}
	s.pages[table][pageKey] = true
}

func childTableName(parent, col string) string {
	name := parent + "__" + sanitizeName(col)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// tableDesc 记录主表与子表的层级结构，用于渲染给模型的摘要。
type tableDesc struct {
	name     string
	rows     int
	cols     []ColumnInfo
	children []*tableDesc
}

func (d *tableDesc) render(indent int) string {
	var b strings.Builder
	pad := strings.Repeat("  ", indent)
	colDesc := make([]string, 0, len(d.cols))
	for _, c := range d.cols {
		colDesc = append(colDesc, fmt.Sprintf("%s (%s)", c.Name, c.Type))
	}
	fmt.Fprintf(&b, "%s- `%s` (%d rows): %s\n", pad, d.name, d.rows, strings.Join(colDesc, ", "))
	for _, child := range d.children {
		fmt.Fprintf(&b, "%s  child table (join `%s._prow` = parent `_row`):\n", pad, child.name)
		b.WriteString(child.render(indent + 1))
	}
	return b.String()
}

// buildTable 把一组记录物化为新表；子记录列按实际数据量决定是否递归拆
// 子表，用 _row（行号）/_prow（父行号）关联。
// 停止条件（结合实际而非固定层数）：
//   - 子表行数 < 2 或序列化后 < mcpOffloadChildMinBytes：数据量太小，
//     保留 JSON 字符串列即可，样例行已够模型理解；
//   - 累计单元格数超 mcpOffloadCellBudget：防嵌套数组笛卡尔式膨胀；
//   - 深度达 mcpOffloadChildDepthMax：防自引用/病态深嵌套的硬上限。
func (s *MCPOffloadStore) buildTable(ctx context.Context, table string, rows []map[string]interface{}, depth int, desc *tableDesc) error {
	if depth >= mcpOffloadChildDepthMax {
		logger.GetLogger(ctx).Infof("MCP offload: stop splitting at %s (max depth)", table)
		return nil
	}
	// 先基于原始行识别子记录列（混合形态归一），摊平时这些列保持原值，
	// 不做点路径展开，避免父表 schema 被同一列的数组/对象两种形态污染。
	childCols := detectChildCols(rows)
	skip := make(map[string]bool, len(childCols))
	for _, c := range childCols {
		skip[c] = true
	}
	flatRows, cols, colOrder := flattenAll(rows, skip)
	if len(cols) == 0 {
		return fmt.Errorf("no columns detected for %s", table)
	}

	// 再按实际数据量筛"值得拆"的列。
	worthSplitting := make([]string, 0, len(childCols))
	for _, col := range childCols {
		childRows := collectChildRows(flatRows, col)
		if len(childRows) < 2 {
			continue
		}
		if estimateRowsBytes(childRows) < mcpOffloadChildMinBytes {
			continue
		}
		if s.cells+int64(len(childRows))*int64(maxInt(len(cols), 1)) > mcpOffloadCellBudget {
			logger.GetLogger(ctx).Warnf("MCP offload: skip child table %s__%s (cell budget)", table, col)
			continue
		}
		worthSplitting = append(worthSplitting, col)
	}
	if len(worthSplitting) > 0 {
		cols, colOrder, flatRows = appendRowOrdinal(cols, colOrder, flatRows)
	}
	if err := s.createAndLoad(ctx, table, cols, colOrder, flatRows); err != nil {
		return err
	}

	desc.name = table
	desc.rows = len(flatRows)
	desc.cols = cols

	s.mu.Lock()
	s.tables[table] = &offloadedTable{
		name:         table,
		columns:      cols,
		rowKeys:      colOrder,
		rows:         int64(len(flatRows)),
		nextRow:      int64(len(flatRows)),
		childCols:    worthSplitting,
		childNextRow: map[string]int64{},
	}
	s.cells += int64(len(flatRows)) * int64(len(cols))
	s.mu.Unlock()

	for _, col := range worthSplitting {
		childRows := collectChildRows(flatRows, col)
		childName := childTableName(table, col)
		childDesc := &tableDesc{}
		if err := s.buildTable(ctx, childName, childRows, depth+1, childDesc); err != nil {
			logger.GetLogger(ctx).Warnf("MCP offload: child table %s failed: %v", childName, err)
			continue
		}
		if childDesc.name != "" {
			desc.children = append(desc.children, childDesc)
		}
	}
	return nil
}

// renderFreshSummary 输出首次落表的完整摘要（结构 + 样例 + 完整性 + 指引）。
func (s *MCPOffloadStore) renderFreshSummary(desc tableDesc, originalRows []map[string]interface{}, meta offloadMeta, t *offloadedTable) string {
	sampleN := mcpOffloadSampleRows
	if sampleN > len(originalRows) {
		sampleN = len(originalRows)
	}
	var sample strings.Builder
	for i := 0; i < sampleN; i++ {
		b, _ := json.Marshal(originalRows[i])
		sample.WriteString("  " + string(b) + "\n")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Offloaded data] This tool returned %d rows — too large to inline into the conversation. The data is loaded into local read-only SQL tables:\n\n", len(originalRows))
	b.WriteString(desc.render(0))
	fmt.Fprintf(&b, "\nSample rows (first %d of %d, original JSON):\n%s\n", sampleN, len(originalRows), sample.String())
	fmt.Fprintf(&b, "%s\n%s", completenessLine(t.rows, maxInt(t.totalCount, meta.totalCount)), analyzeHint(t.name))
	return b.String()
}

// ---- 摊平 ----

// flattenAll 把每行摊平为标量列：嵌套对象递归成点路径列（customer.name），
// 数组/更深结构保留为 JSON 字符串列。skipKeys（子记录列）保持原值不摊
// 平。摊平后列数超过上限时退化为浅摊平（嵌套对象整体 JSON 字符串），
// 防止宽对象炸出过多列。
func flattenAll(rows []map[string]interface{}, skipKeys map[string]bool) ([]map[string]interface{}, []ColumnInfo, []string) {
	flat := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		flat[i] = flattenRow(row, "", mcpOffloadFlattenDepth, skipKeys)
	}
	if countColumns(flat) > mcpOffloadMaxColumns {
		for i, row := range rows {
			flat[i] = flattenRow(row, "", 0, skipKeys)
		}
	}
	cols, order := flattenColumns(flat)
	return flat, cols, order
}

// flattenRow 递归摊平一行：depth 用尽或遇到数组时，值整体落入当前列；
// 顶层命中 skipKeys 的子记录列保持原值（后续整体按 JSON 字符串入库）。
func flattenRow(row map[string]interface{}, prefix string, depth int, skipKeys map[string]bool) map[string]interface{} {
	out := make(map[string]interface{}, len(row))
	for k, v := range row {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		if child, ok := v.(map[string]interface{}); ok && depth > 0 &&
			!(prefix == "" && skipKeys != nil && skipKeys[k]) {
			for ck, cv := range flattenRow(child, name, depth-1, nil) {
				out[ck] = cv
			}
			continue
		}
		out[name] = v
	}
	return out
}

func countColumns(rows []map[string]interface{}) int {
	seen := map[string]bool{}
	for _, row := range rows {
		for k := range row {
			seen[k] = true
		}
	}
	return len(seen)
}

// flattenColumns 汇总所有行出现的字段：标量按值推断类型，数组序列化为
// JSON 字符串列。列序按名字排序，保证建表与摘要一致。
func flattenColumns(rows []map[string]interface{}) ([]ColumnInfo, []string) {
	kinds := map[string]map[string]bool{}
	for _, row := range rows {
		for k, v := range row {
			if kinds[k] == nil {
				kinds[k] = map[string]bool{}
			}
			switch v.(type) {
			case nil:
				// 不参与类型决策
			case bool:
				kinds[k]["bool"] = true
			case float64:
				if isIntegralFloat(v.(float64)) {
					kinds[k]["int"] = true
				} else {
					kinds[k]["float"] = true
				}
			case string:
				kinds[k]["str"] = true
			default:
				kinds[k]["json"] = true
			}
		}
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)

	// DuckDB 标识符大小写不敏感，同名异大小写的列会冲突，折叠去重。
	lower := map[string]string{}
	cols := make([]ColumnInfo, 0, len(names))
	order := make([]string, 0, len(names))
	for _, k := range names {
		name := k
		if name == "" {
			name = "col"
		}
		key := strings.ToLower(name)
		if prev, ok := lower[key]; ok {
			name = prev + "_2"
			key = strings.ToLower(name)
		}
		lower[key] = name
		cols = append(cols, ColumnInfo{Name: name, Type: inferDuckType(kinds[k])})
		order = append(order, k)
	}
	return cols, order
}

// collectChildRows 把子记录列摊成子表行：每个元素摊平后带 _prow（父行
// 号）；数组/单对象/null 都已归一。自己的 _row 由建表/追加阶段补。
func collectChildRows(flatRows []map[string]interface{}, col string) []map[string]interface{} {
	child := make([]map[string]interface{}, 0)
	for _, row := range flatRows {
		parentRow, _ := row["_row"].(float64)
		for _, elem := range childElements(row[col]) {
			flat := flattenRow(elem, "", mcpOffloadFlattenDepth, nil)
			flat["_prow"] = parentRow
			child = append(child, flat)
		}
	}
	return child
}

// appendRowOrdinal 为主表补 _row 行号列（1 起），供子表 _prow 关联。
func appendRowOrdinal(cols []ColumnInfo, colOrder []string, flatRows []map[string]interface{}) ([]ColumnInfo, []string, []map[string]interface{}) {
	if _, exists := colIndex(cols, "_row"); exists {
		return cols, colOrder, flatRows
	}
	cols = append(cols, ColumnInfo{Name: "_row", Type: "BIGINT"})
	colOrder = append(colOrder, "_row")
	for i, row := range flatRows {
		row["_row"] = float64(i + 1)
	}
	return cols, colOrder, flatRows
}

func colIndex(cols []ColumnInfo, name string) (int, bool) {
	for i, c := range cols {
		if strings.EqualFold(c.Name, name) {
			return i, true
		}
	}
	return -1, false
}

// estimateRowsBytes 粗估一组行的序列化字节数（子表拆分价值判断）。
func estimateRowsBytes(rows []map[string]interface{}) int {
	n := 0
	for i, row := range rows {
		if i >= 200 { // 抽样前 200 行足够判断量级
			n = n * len(rows) / i
			break
		}
		b, err := json.Marshal(row)
		if err != nil {
			continue
		}
		n += len(b)
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isIntegralFloat(f float64) bool {
	return f == float64(int64(f))
}

func inferDuckType(k map[string]bool) string {
	switch {
	case len(k) == 0:
		return "VARCHAR"
	case k["json"] || k["str"]:
		return "VARCHAR"
	case k["float"]:
		return "DOUBLE"
	case k["int"]:
		return "BIGINT"
	case k["bool"]:
		return "BOOLEAN"
	default:
		return "VARCHAR"
	}
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// createAndLoad 用显式列定义建表并分批 INSERT（无临时文件，清理只需
// DROP TABLE）。参数化写入，单元格超长截断。
func (s *MCPOffloadStore) createAndLoad(ctx context.Context, table string, cols []ColumnInfo, colOrder []string, rows []map[string]interface{}) error {
	var colDefs strings.Builder
	for i, c := range cols {
		if i > 0 {
			colDefs.WriteString(", ")
		}
		colDefs.WriteString(quoteIdent(c.Name) + " " + c.Type)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (%s)", quoteIdent(table), colDefs.String())); err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	return insertRows(ctx, s.db, table, cols, colOrder, rows)
}

// insertRows 按列序参数化批量写入（缺键按 NULL）。
func insertRows(ctx context.Context, db *sql.DB, table string, cols []ColumnInfo, colOrder []string, rows []map[string]interface{}) error {
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s VALUES (%s)", quoteIdent(table), strings.Join(placeholders, ","))

	for start := 0; start < len(rows); start += mcpOffloadInsertBatch {
		end := start + mcpOffloadInsertBatch
		if end > len(rows) {
			end = len(rows)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		for _, row := range rows[start:end] {
			vals := make([]interface{}, len(colOrder))
			for i, key := range colOrder {
				vals[i] = cellValue(row[key], cols[i].Type)
			}
			if _, err := stmt.ExecContext(ctx, vals...); err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				return fmt.Errorf("insert row: %w", err)
			}
		}
		if err := stmt.Close(); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// cellValue 把 JSON 值归一为列类型可接受的驱动值。
func cellValue(v interface{}, colType string) interface{} {
	switch val := v.(type) {
	case nil:
		return nil
	case bool:
		if colType == "BOOLEAN" {
			return val
		}
		return fmt.Sprintf("%v", val)
	case float64:
		switch colType {
		case "BIGINT":
			return int64(val)
		case "DOUBLE":
			return val
		default:
			return fmt.Sprintf("%v", val)
		}
	case string:
		if len(val) > mcpOffloadMaxCellBytes {
			val = val[:mcpOffloadMaxCellBytes]
		}
		return val
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		if len(b) > mcpOffloadMaxCellBytes {
			b = b[:mcpOffloadMaxCellBytes]
		}
		return string(b)
	}
}
