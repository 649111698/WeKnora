package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
)

// MCP 大结果自动"落表"：MCP 工具返回的结构化大结果不再全量拼进 LLM
// 上下文，而是物化为 DuckDB 只读表。模型只拿到行数/列结构/样例行，
// 随后用 data_analysis 工具写 SQL 聚合分析，上下文里只回小结果。
// 这是应对"MCP 明细数据量一大就撑爆上下文/思考预算"的通用解法。

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

type offloadedTable struct {
	name    string
	columns []ColumnInfo
	rows    int64
}

// MCPOffloadStore 追踪一次问答请求内物化的 MCP 表。每个 agent engine
// 各建一个；实现 types.Cleanable，请求结束时由 ToolRegistry.Cleanup
// 统一 DROP（幂等，多个 MCP 工具共享同一 store 也只会清一次）。
type MCPOffloadStore struct {
	mu      sync.Mutex
	db      *sql.DB
	tables  map[string]*offloadedTable
	byTool  map[string][]string
	seq     int
	cells   int64 // 已物化累计单元格数（预算控制，防嵌套膨胀）
	cleaned bool
}

// NewMCPOffloadStore 建立会话级落表存储；db 为 nil 时所有方法退化为空操作。
func NewMCPOffloadStore(db *sql.DB) *MCPOffloadStore {
	return &MCPOffloadStore{
		db:     db,
		tables: map[string]*offloadedTable{},
		byTool: map[string][]string{},
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
	s.byTool = map[string][]string{}
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
// 模型的摘要；不适合落表时 ok=false，调用方保留原文。
func (s *MCPOffloadStore) TryOffload(ctx context.Context, toolName, text string) (string, bool) {
	if s == nil || s.db == nil {
		return "", false
	}
	if len(text) < mcpOffloadMinBytes {
		return "", false
	}
	rows, ok := extractTabularRows(text)
	if !ok || len(rows) < mcpOffloadMinRows {
		return "", false
	}
	summary, err := s.materialize(ctx, toolName, rows)
	if err != nil {
		logger.GetLogger(ctx).Warnf("MCP offload: materialize %s failed (%d rows), falling back to inline result: %v", toolName, len(rows), err)
		return "", false
	}
	return summary, true
}

// extractTabularRows 从 JSON 文本中抽出记录数组：顶层是数组，或在多层
// 包装对象里递归找最大的对象数组（{"code":0,"data":{"records":[...]}}、
// {"result":{"list":[...]}} 等真实 MCP 常见形态）。
func extractTabularRows(text string) ([]map[string]interface{}, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var top interface{}
	if err := json.Unmarshal([]byte(trimmed), &top); err != nil {
		return nil, false
	}
	best, bestLen := findLargestRowArray(top, 0)
	if bestLen == 0 {
		return nil, false
	}
	return best, true
}

// findLargestRowArray 深度优先递归（上限 mcpOffloadSearchDepth），返回节
// 点子树里行数最多的对象数组。
func findLargestRowArray(v interface{}, depth int) ([]map[string]interface{}, int) {
	if rows, ok := rowsFromArray(v); ok {
		return rows, len(rows)
	}
	if depth >= mcpOffloadSearchDepth {
		return nil, 0
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return nil, 0
	}
	best, bestLen := []map[string]interface{}{}, 0
	// map 遍历无序，按 key 排序保证同结构输入选出同一数组（确定性）。
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if rows, n := findLargestRowArray(obj[k], depth+1); n > bestLen {
			best, bestLen = rows, n
		}
	}
	return best, bestLen
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

// materialize 建表（含数组列递归拆子表）、灌数、登记并返回摘要文本。
func (s *MCPOffloadStore) materialize(ctx context.Context, toolName string, rows []map[string]interface{}) (string, error) {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	base := sanitizeName(toolName)
	if len(base) > 40 {
		base = base[:40]
	}
	table := fmt.Sprintf("%s_t%d", base, seq)

	var desc tableDesc
	if err := s.buildTable(ctx, table, rows, 0, &desc); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.byTool[toolName] = append(s.byTool[toolName], table)
	prev := append([]string{}, s.byTool[toolName]...)
	s.mu.Unlock()

	sampleN := mcpOffloadSampleRows
	if sampleN > len(rows) {
		sampleN = len(rows)
	}
	var sample strings.Builder
	for i := 0; i < sampleN; i++ {
		original, _ := json.Marshal(rows[i])
		sample.WriteString("  " + string(original) + "\n")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Offloaded data] This tool returned %d rows — too large to inline into the conversation. The full dataset is loaded into local read-only SQL tables:\n\n", len(rows))
	b.WriteString(desc.render(0))
	fmt.Fprintf(&b, "\nSample rows (first %d of %d, original JSON):\n%s\n", sampleN, len(rows), sample.String())
	fmt.Fprintf(&b, "Analyze with the `data_analysis` tool: knowledge_id = the table name, sql = SELECT-only statements. Do not fetch the raw rows again — they will not fit in context.")
	if len(prev) > 1 {
		fmt.Fprintf(&b, " Earlier offloads from this same tool: %s (UNION ALL them if they are pages of the same query).", strings.Join(prev[:len(prev)-1], ", "))
	}
	return b.String(), nil
}

// tableDesc 记录主表与子表的层级结构，用于渲染给模型的摘要。
type tableDesc struct {
	name     string
	rows     int
	cols     []ColumnInfo
	arrayCol string // 子表来源列（主表里保留同名列的 JSON 原文）
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
		fmt.Fprintf(&b, "%s  child of `%s` via `%s._prow` = parent `_row`:\n", pad, d.name, child.name)
		b.WriteString(child.render(indent + 1))
	}
	return b.String()
}

// buildTable 把一组记录物化为表；子记录列（值可能时为数组、时为单对象、
// 时为 null —— 真实 MCP 常见形态）按实际数据量决定是否递归拆成
// "__列名" 子表，用 _row（行号）/_prow（父行号）关联。
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
	s.tables[table] = &offloadedTable{name: table, columns: cols, rows: int64(len(flatRows))}
	s.cells += int64(len(flatRows)) * int64(len(cols))
	s.mu.Unlock()

	for _, col := range worthSplitting {
		childRows := collectChildRows(flatRows, col)
		childName := table + "__" + sanitizeName(col)
		if len(childName) > 63 {
			childName = childName[:63]
		}
		childDesc := &tableDesc{arrayCol: col}
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

// collectChildRows 把子记录列摊成子表行：每个元素摊平后带 _prow（父行
// 号）；数组/单对象/null 都已归一。自己的 _row 由子表阶段的
// appendRowOrdinal 补充（当子表还有下一层时）。
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
		tx, err := s.db.BeginTx(ctx, nil)
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
