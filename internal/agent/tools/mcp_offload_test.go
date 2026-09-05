package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// newOffloadDuckDB 打开内存 DuckDB（落表路径只用内建能力，无需扩展）。
func newOffloadDuckDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Skipf("open duckdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func bigRowsJSON(n int) string {
	pad := strings.Repeat("描述详情字段内容。", 60)
	rows := make([]map[string]interface{}, n)
	for i := 0; i < n; i++ {
		rows[i] = map[string]interface{}{
			"问题类型": []string{"问题查询", "修改问题", "需求", "BUG"}[i%4],
			"数量":   i,
			"状态":   "closed",
			"描述":   pad,
		}
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

func TestMCPOffload_TryOffloadArray(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	summary, ok := store.TryOffload(ctx, "mcp_zhijietong_queryaftersale", bigRowsJSON(50))
	if !ok {
		t.Fatal("large array of objects should offload")
	}
	for _, want := range []string{"50 rows", "mcp_zhijietong_queryaftersale_t1", "问题类型", "data_analysis"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1").Scan(&n); err != nil {
		t.Fatalf("query offloaded table: %v", err)
	}
	if n != 50 {
		t.Fatalf("row count = %d, want 50", n)
	}
	var cnt int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1 WHERE "问题类型" = 'BUG'`).Scan(&cnt); err != nil {
		t.Fatalf("filter by CJK column: %v", err)
	}
	if cnt != 50/4 {
		t.Fatalf("BUG rows = %d, want %d", cnt, 50/4)
	}
	if !store.Has("mcp_zhijietong_queryaftersale_t1") {
		t.Fatal("store should track the table")
	}
}

func TestMCPOffload_WrappedRecords(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	wrapped := map[string]interface{}{
		"total":   92,
		"pageNo":  1,
		"records": json.RawMessage(bigRowsJSON(30)),
	}
	b, _ := json.Marshal(wrapped)

	summary, ok := store.TryOffload(context.Background(), "mcp_svc_tool", string(b))
	if !ok {
		t.Fatal("wrapped records array should offload")
	}
	if !strings.Contains(summary, "30 rows") {
		t.Fatalf("summary should report 30 rows:\n%s", summary)
	}
}

func TestMCPOffload_SkipsSmallOrNonTabular(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	if _, ok := store.TryOffload(ctx, "t", bigRowsJSON(10)); ok {
		t.Fatal("few rows should not offload")
	}
	small := `{"msg":"` + strings.Repeat("x", 20000) + `"}`
	if _, ok := store.TryOffload(ctx, "t", small); ok {
		t.Fatal("big non-tabular JSON should not offload")
	}
	prose := strings.Repeat("不是 JSON 的一大段文本。", 2000)
	if _, ok := store.TryOffload(ctx, "t", prose); ok {
		t.Fatal("plain text should not offload")
	}
}

func TestMCPOffload_DataAnalysisQueryAndCleanup(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	if _, ok := store.TryOffload(ctx, "mcp_zhijietong_queryaftersale", bigRowsJSON(40)); !ok {
		t.Fatal("offload failed")
	}

	da := NewDataAnalysisTool(nil, nil, nil, nil, db, "sess-test").WithMCPOffload(store)
	args, _ := json.Marshal(DataAnalysisInput{
		KnowledgeID: "mcp_zhijietong_queryaftersale_t1",
		Sql:         `SELECT "问题类型", COUNT(*) AS n FROM mcp_zhijietong_queryaftersale_t1 GROUP BY 1 ORDER BY n DESC`,
	})
	res, err := da.Execute(ctx, args)
	if err != nil || !res.Success {
		t.Fatalf("data_analysis on offloaded table: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Output, "问题查询") {
		t.Fatalf("aggregate result missing expected group:\n%s", res.Output)
	}

	// 写操作依旧被拒绝（只读防线）。
	writeArgs, _ := json.Marshal(DataAnalysisInput{
		KnowledgeID: "mcp_zhijietong_queryaftersale_t1",
		Sql:         "DELETE FROM mcp_zhijietong_queryaftersale_t1",
	})
	res, err = da.Execute(ctx, writeArgs)
	if err == nil && res.Success {
		t.Fatal("write SQL must be rejected on offloaded tables")
	}

	// Cleanup 后表被 DROP，且幂等可重复调用。
	store.Cleanup(ctx)
	store.Cleanup(ctx)
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1").Scan(&n); err == nil {
		t.Fatal("table should be dropped after cleanup")
	}
	if store.Has("mcp_zhijietong_queryaftersale_t1") {
		t.Fatal("store should forget tables after cleanup")
	}
}

func TestMCPOffload_SequentialTablesSameTool(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	s1, ok1 := store.TryOffload(ctx, "mcp_svc_tool", bigRowsJSON(25))
	s2, ok2 := store.TryOffload(ctx, "mcp_svc_tool", bigRowsJSON(25))
	if !ok1 || !ok2 {
		t.Fatalf("both pages should offload: %v %v", ok1, ok2)
	}
	if !strings.Contains(s1, "mcp_svc_tool_t1") || !strings.Contains(s2, "mcp_svc_tool_t2") {
		t.Fatalf("sequential tables expected:\n%s\n%s", s1, s2)
	}
	if !strings.Contains(s2, "mcp_svc_tool_t1") {
		t.Fatal("second page should hint at the first table for UNION ALL")
	}
	// UNION 两个分页表聚合。
	var total int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM mcp_svc_tool_t1 UNION ALL SELECT COUNT(*) FROM mcp_svc_tool_t2").Scan(&total)
	if err != nil || total != 25 {
		t.Fatalf("union across pages failed: err=%v total=%d", err, total)
	}
}

func TestMCPOffload_NestedObjectsFlattenedToDottedColumns(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	var rows []map[string]interface{}
	for i := 0; i < 25; i++ {
		rows = append(rows, map[string]interface{}{
			"id":   i,
			"meta": map[string]interface{}{"kind": "a", "score": 1.5},
			"备注":   strings.Repeat("详情。", 150),
		})
	}
	b, _ := json.Marshal(rows)
	if _, ok := store.TryOffload(ctx, "mcp_svc_tool", string(b)); !ok {
		t.Fatal("rows with nested objects should offload")
	}
	// 嵌套对象摊平成点路径列，可直接 SQL 查询。
	var kind string
	if err := db.QueryRowContext(ctx, `SELECT "meta.kind" FROM mcp_svc_tool_t1 WHERE "id" = 0`).Scan(&kind); err != nil {
		t.Fatalf("dotted column should be queryable: %v", err)
	}
	if kind != "a" {
		t.Fatalf("meta.kind = %q, want a", kind)
	}
	var score float64
	if err := db.QueryRowContext(ctx, `SELECT "meta.score" FROM mcp_svc_tool_t1 WHERE "id" = 3`).Scan(&score); err != nil {
		t.Fatalf("dotted numeric column should be queryable: %v", err)
	}
	if score != 1.5 {
		t.Fatalf("meta.score = %v, want 1.5", score)
	}
}

func TestMCPOffload_ArrayFieldsStayJSONString(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	var rows []map[string]interface{}
	for i := 0; i < 25; i++ {
		rows = append(rows, map[string]interface{}{
			"id":   i,
			"tags": []interface{}{"售后", "加急"},
			"备注":   strings.Repeat("详情。", 150),
		})
	}
	b, _ := json.Marshal(rows)
	if _, ok := store.TryOffload(ctx, "mcp_svc_tool", string(b)); !ok {
		t.Fatal("rows with array fields should offload")
	}
	var tags string
	if err := db.QueryRowContext(ctx, `SELECT "tags" FROM mcp_svc_tool_t1 WHERE "id" = 0`).Scan(&tags); err != nil {
		t.Fatalf("array column should be JSON string: %v", err)
	}
	if !strings.Contains(tags, "加急") {
		t.Fatalf("array JSON unexpected: %s", tags)
	}
}

func TestMCPOffload_DeeplyWrappedArray(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	// 真实 MCP 常见形态：{"code":0,"message":"ok","result":{"data":{"page":{"records":[...]}}}}
	inner := json.RawMessage(bigRowsJSON(30))
	wrapped := map[string]interface{}{
		"code":    0,
		"message": "ok",
		"result":  map[string]interface{}{"data": map[string]interface{}{"page": map[string]interface{}{"records": inner}}},
	}
	b, _ := json.Marshal(wrapped)
	summary, ok := store.TryOffload(ctx, "mcp_zhijietong_queryaftersale", string(b))
	if !ok {
		t.Fatal("deeply wrapped records array should offload")
	}
	if !strings.Contains(summary, "30 rows") {
		t.Fatalf("summary should report 30 rows:\n%s", summary)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1").Scan(&n); err != nil {
		t.Fatalf("query deeply wrapped table: %v", err)
	}
	if n != 30 {
		t.Fatalf("rows = %d, want 30", n)
	}
}

func TestMCPOffload_NilStoreIsNoop(t *testing.T) {
	var store *MCPOffloadStore
	if _, ok := store.TryOffload(context.Background(), "t", bigRowsJSON(100)); ok {
		t.Fatal("nil store must be a no-op")
	}
	if store.Has("x") {
		t.Fatal("nil store Has = false")
	}
	store.Cleanup(context.Background()) // 不得 panic
	_ = fmt.Sprint(store)
}

// TestMCPOffload_RealZhijietongShape 用真实智捷通返回形态验证：
// {"data":{"rows":[{...,"ee":[{...,"se":[{...,"qtn":"问题类型"}]}]}]}}
// 主表 + ee 子表 + se 孙表，qtn 可直接 GROUP BY。
func TestMCPOffload_RealZhijietongShape(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	mkRow := func(i int, qtns ...string) map[string]interface{} {
		stages := make([]interface{}, 0)
		for s, qtn := range qtns {
			issues := []interface{}{
				map[string]interface{}{"seq": 1, "ss": "2", "stt": "已解决", "qtn": qtn, "memo": strings.Repeat("问题处理备注。", 10)},
			}
			stages = append(stages, map[string]interface{}{
				"seq": s + 1, "tot": fmt.Sprintf("T%d", s), "tost": "已解决",
				"todm": "项目部", "tobn": "倍智004", "mq": 20.0, "se": issues,
			})
		}
		if len(qtns) == 0 {
			stages = append(stages, map[string]interface{}{
				"seq": 1, "tot": "T0", "tost": "待提交", "todm": "开发部",
				"tobn": "谢仁峰", "mq": 0.0, "se": nil,
			})
		}
		return map[string]interface{}{
			"id": fmt.Sprintf("20849241479803269%04d", i),
			"bn": fmt.Sprintf("SM-241116-%04d", i),
			"ct": "2024-11-16 15:31:21", "tofh": "0", "toft": "已开始",
			"ee": stages, "pnm": "镜筒", "pnb": fmt.Sprintf("HK24002%d", i%10),
			"备注": strings.Repeat("详情。", 100),
		}
	}
	rows := make([]interface{}, 30)
	for i := 0; i < 30; i++ {
		switch i % 3 {
		case 0:
			rows[i] = mkRow(i, "孔位不正")
		case 1:
			rows[i] = mkRow(i, "规格不符", "表面粗糙")
		default:
			rows[i] = mkRow(i) // 无问题记录
		}
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"data":      map[string]interface{}{"filter": "[billstatus = 'C']", "pageNo": 1, "pageSize": 30, "rows": rows, "totalCount": 30},
		"errorCode": "0",
		"message":   nil,
		"status":    true,
	})

	summary, ok := store.TryOffload(ctx, "mcp_zhijietong_queryaftersale", string(payload))
	if !ok {
		t.Fatal("zhijietong-shaped payload should offload")
	}
	for _, want := range []string{
		"mcp_zhijietong_queryaftersale_t1",
		"mcp_zhijietong_queryaftersale_t1__ee",
		"mcp_zhijietong_queryaftersale_t1__ee__se",
		"_prow", "_row", "qtn",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	// 主表行数。
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1").Scan(&n); err != nil || n != 30 {
		t.Fatalf("parent rows = %d err=%v, want 30", n, err)
	}
	// 孙表直接按问题类型聚合：10×1 + 10×2 = 30 条问题，其中"规格不符"与"表面粗糙"各 10，"孔位不正" 10。
	var qtn string
	var cnt int
	err := db.QueryRowContext(ctx,
		`SELECT "qtn", COUNT(*) FROM mcp_zhijietong_queryaftersale_t1__ee__se GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 1`).Scan(&qtn, &cnt)
	if err != nil {
		t.Fatalf("group by qtn on grandchild table: %v", err)
	}
	if cnt != 10 {
		t.Fatalf("top qtn count = %d, want 10 (got %q)", cnt, qtn)
	}
	// JOIN 链：按部门统计问题数。
	var joined int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_zhijietong_queryaftersale_t1__ee__se g
		JOIN mcp_zhijietong_queryaftersale_t1__ee e ON g."_prow" = e."_row"
		WHERE e."todm" = '项目部'`).Scan(&joined)
	if err != nil {
		t.Fatalf("join child chain: %v", err)
	}
	if joined != 30 {
		t.Fatalf("project-dept issues = %d, want 30", joined)
	}
	// data_analysis 也能直接查子表。
	da := NewDataAnalysisTool(nil, nil, nil, nil, db, "sess-zjt").WithMCPOffload(store)
	args, _ := json.Marshal(DataAnalysisInput{
		KnowledgeID: "mcp_zhijietong_queryaftersale_t1__ee__se",
		Sql:         `SELECT "qtn", COUNT(*) AS n FROM mcp_zhijietong_queryaftersale_t1__ee__se GROUP BY 1 ORDER BY n DESC`,
	})
	res, err := da.Execute(ctx, args)
	if err != nil || !res.Success {
		t.Fatalf("data_analysis on grandchild: err=%v res=%+v", err, res)
	}
	// 清理后全部子表也一起 DROP。
	store.Cleanup(ctx)
	for _, tbl := range []string{"mcp_zhijietong_queryaftersale_t1", "mcp_zhijietong_queryaftersale_t1__ee", "mcp_zhijietong_queryaftersale_t1__ee__se"} {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&n); err == nil {
			t.Fatalf("table %s should be dropped", tbl)
		}
	}
}

// TestMCPOffload_SmallChildNotSplit 自适应停止：子树数据量太小（<2KB）
// 就不单独建表，保留 JSON 字符串列。
func TestMCPOffload_SmallChildNotSplit(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	var rows []map[string]interface{}
	for i := 0; i < 30; i++ {
		rows = append(rows, map[string]interface{}{
			"id":  i,
			"tag": []interface{}{map[string]interface{}{"k": "v"}}, // 子树总量 <2KB
			"备注":  strings.Repeat("详情。", 150),
		})
	}
	b, _ := json.Marshal(rows)
	summary, ok := store.TryOffload(ctx, "mcp_svc_tool", string(b))
	if !ok {
		t.Fatal("should offload main rows")
	}
	if strings.Contains(summary, "__tag") {
		t.Fatalf("tiny child subtree must not be split:\n%s", summary)
	}
	if _, err := db.QueryContext(ctx, "SELECT 1 FROM mcp_svc_tool_t1__tag LIMIT 1"); err == nil {
		t.Fatal("child table should not exist for tiny subtree")
	}
	// 父表里 tag 仍是 JSON 字符串列。
	var tag string
	if err := db.QueryRowContext(ctx, `SELECT "tag" FROM mcp_svc_tool_t1 WHERE "id" = 0`).Scan(&tag); err != nil {
		t.Fatalf("tag column should exist as JSON string: %v", err)
	}
}

// TestMCPOffload_BigChildStillSplits 数据量足够时照常拆（对照）。
func TestMCPOffload_BigChildStillSplits(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	var rows []map[string]interface{}
	for i := 0; i < 30; i++ {
		rows = append(rows, map[string]interface{}{
			"id": i,
			"ee": []interface{}{
				map[string]interface{}{"seq": 1, "tobn": "张三", "tost": "已解决", "说明": strings.Repeat("处理记录。", 40)},
				map[string]interface{}{"seq": 2, "tobn": "李四", "tost": "已提交", "说明": strings.Repeat("处理记录。", 40)},
			},
			"备注": strings.Repeat("详情。", 150),
		})
	}
	b, _ := json.Marshal(rows)
	summary, ok := store.TryOffload(ctx, "mcp_svc_tool", string(b))
	if !ok {
		t.Fatal("should offload")
	}
	if !strings.Contains(summary, "mcp_svc_tool_t1__ee") {
		t.Fatalf("substantial child should be split:\n%s", summary)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_svc_tool_t1__ee WHERE "tobn" = '张三'`).Scan(&n); err != nil || n != 30 {
		t.Fatalf("child rows for 张三 = %d err=%v, want 30", n, err)
	}
}

// TestMCPOffload_MixedArrayObjectNullField 同一字段时为数组、时为单对象、
// 时为 null（真实 MCP 形态）：归一拆子表，父表该列不被点路径摊平污染。
func TestMCPOffload_MixedArrayObjectNullField(t *testing.T) {
	db := newOffloadDuckDB(t)
	store := NewMCPOffloadStore(db)
	ctx := context.Background()

	var rows []map[string]interface{}
	for i := 0; i < 30; i++ {
		var se interface{}
		switch i % 3 {
		case 0: // 数组（≥2 元素）
			se = []interface{}{
				map[string]interface{}{"seq": 1, "qtn": "孔位不正", "ss": "2", "备注2": strings.Repeat("x", 80)},
				map[string]interface{}{"seq": 2, "qtn": "表面粗糙", "ss": "1", "备注2": strings.Repeat("x", 80)},
			}
		case 1: // 单对象
			se = map[string]interface{}{"seq": 1, "qtn": "规格不符", "ss": "2", "备注2": strings.Repeat("x", 80)}
		default: // null
			se = nil
		}
		rows = append(rows, map[string]interface{}{
			"id": i,
			"se": se,
			"备注": strings.Repeat("详情。", 150),
		})
	}
	b, _ := json.Marshal(rows)
	summary, ok := store.TryOffload(ctx, "mcp_svc_tool", string(b))
	if !ok {
		t.Fatal("should offload")
	}
	if !strings.Contains(summary, "mcp_svc_tool_t1__se") {
		t.Fatalf("mixed-shape field should still split into child table:\n%s", summary)
	}
	// 子表行数 = 10×2 + 10×1 + 0 = 30；三种形态都归一进来。
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_svc_tool_t1__se").Scan(&n); err != nil || n != 30 {
		t.Fatalf("child rows = %d err=%v, want 30", n, err)
	}
	var cnt int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_svc_tool_t1__se WHERE "qtn" = '规格不符'`).Scan(&cnt); err != nil || cnt != 10 {
		t.Fatalf("规格不符 = %d err=%v, want 10", cnt, err)
	}
	// 父表 se 列保持单一 JSON 字符串列，不出现 se.qtn 之类的点路径列。
	for _, c := range store.Schema("mcp_svc_tool_t1").Columns {
		if strings.HasPrefix(c.Name, "se.") {
			t.Fatalf("parent schema polluted by dotted column %q", c.Name)
		}
	}
}
