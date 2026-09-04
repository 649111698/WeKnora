package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIsWeComSyntheticEmail(t *testing.T) {
	assert.True(t, IsWeComSyntheticEmail("wecom_zhangsan@wecom.sso.weknora.local"))
	assert.False(t, IsWeComSyntheticEmail("zhangsan@corp.com"))
	assert.False(t, IsWeComSyntheticEmail("feishu_u1@feishu.sso.weknora.local"))
}

func TestWeComUserIDFromEmail(t *testing.T) {
	id, ok := WeComUserIDFromEmail("wecom_zhangsan@wecom.sso.weknora.local")
	assert.True(t, ok)
	assert.Equal(t, "zhangsan", id)

	_, ok = WeComUserIDFromEmail("mountain@pexetech.com")
	assert.False(t, ok, "non-WeCom member must not resolve")
}

func TestRenderUsageReportText(t *testing.T) {
	svc := &usageReportService{}
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.Local)
	r := &UsageReport{
		TenantName:    "倍智信息",
		Date:          "2026-09-03",
		TotalUsers:    3,
		Qualified:     2,
		Unqualified:   1,
		TotalMessages: 12,
		PrevQualified: 1,
		PrevMessages:  8,
		Rows: []UsageReportRow{
			{Username: "张三", Logins: 2, Chats: 5, Qualified: true, LastActive: now.Add(-2 * time.Hour)},
			{Username: "李四", Logins: 1, Chats: 7, Qualified: true, LastActive: now.Add(-26 * time.Hour)},
			{Username: "王五", Logins: 3, Chats: 0, Qualified: false},
		},
	}
	md := svc.RenderUsageReportText(r, now)

	assert.Contains(t, md, "倍智信息 使用日报")
	assert.Contains(t, md, "总用户数：3")
	assert.Contains(t, md, "达标用户：2（66.7%）")
	assert.Contains(t, md, "未达标用户：1（33.3%）")
	assert.Contains(t, md, "昨日消息总数：12")
	assert.Contains(t, md, "达标标准：登录 ≥1 次 且 对话 ≥2 次")
	assert.Contains(t, md, "张三：登录 2｜对话 5")
	assert.Contains(t, md, "王五：登录 3｜对话 0")
	assert.Contains(t, md, "整体达标率 66.7%，较前日 +33.3 个百分点，趋势向好")
	assert.Contains(t, md, "报告生成：2026-09-04 09:00")
	assert.LessOrEqual(t, len(md), usageReportMaxBytes+512, "report must stay within WeCom message limits")
	assert.NotContains(t, md, "**", "plain text report must not carry markdown syntax")
	assert.NotContains(t, md, "<font", "plain text report must not carry HTML tags")
	assert.NotContains(t, md, "##", "plain text report must not carry markdown headings")
}

func TestRenderUsageReportText_TrendDownAndFlat(t *testing.T) {
	svc := &usageReportService{}
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.Local)

	down := &UsageReport{TenantName: "T", Date: "2026-09-03", TotalUsers: 10, Qualified: 3, PrevQualified: 5}
	assert.Contains(t, svc.RenderUsageReportText(down, now), "有所回落")

	flat := &UsageReport{TenantName: "T", Date: "2026-09-03", TotalUsers: 10, Qualified: 4, PrevQualified: 4}
	assert.Contains(t, svc.RenderUsageReportText(flat, now), "持平")
}

func TestDeltaPercent(t *testing.T) {
	assert.Equal(t, "+50.0%", deltaPercent(12, 8))
	assert.Equal(t, "-20.0%", deltaPercent(8, 10))
	assert.Equal(t, "+5 条", deltaPercent(5, 0))
	assert.Equal(t, "持平", deltaPercent(0, 0))
}

func TestUsageRowSorting(t *testing.T) {
	rows := []UsageReportRow{
		{Username: "B", Logins: 1, Chats: 0},
		{Username: "A", Logins: 9, Chats: 9},
		{Username: "C", Logins: 2, Chats: 3},
	}
	sortUsageRows(rows)
	assert.Equal(t, "A", rows[0].Username, "qualified row first")
	assert.Equal(t, "C", rows[1].Username, "more chats ranks higher")
}

func TestUsageReportConfigRoundTrip(t *testing.T) {
	// 配置结构序列化保持字段名稳定（存储与 API 共用）。
	cfg := &types.UsageReportConfig{
		Enabled:       true,
		PushToWeCom:   true,
		NotifyUserIDs: []string{"u1", "u2"},
		LastRunDate:   "2026-09-04",
	}
	v, err := cfg.Value()
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(v.([]byte)), "notify_user_ids"))
}

func TestBuildUsageReportCountsWeComUsersOnly(t *testing.T) {
	// 日报只统计企微 SSO 用户：本地账号、飞书等其他渠道账号不进人数与达标考核。
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	assert.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT, usage_report_config TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE tenant_members (tenant_id INTEGER, user_id TEXT, status TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT, name TEXT, email TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE auth_tokens (user_id TEXT, token_type TEXT, created_at TIMESTAMP)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT)`,
		`CREATE TABLE messages (session_id TEXT, role TEXT, created_at TIMESTAMP)`,
	} {
		assert.NoError(t, db.Exec(ddl).Error)
	}
	assert.NoError(t, db.Exec(`INSERT INTO tenants (id, name) VALUES (10000, '倍智信息')`).Error)
	for _, u := range []struct{ id, email string }{
		{"u-wecom", "wecom_Mountain@wecom.sso.weknora.local"},
		{"u-local", "mountain@pexetech.com"},
		{"u-feishu", "feishu_open123@feishu.sso.weknora.local"},
	} {
		assert.NoError(t, db.Exec(`INSERT INTO users (id, username, email) VALUES (?, ?, ?)`, u.id, u.id, u.email).Error)
		assert.NoError(t, db.Exec(`INSERT INTO tenant_members (tenant_id, user_id, status) VALUES (10000, ?, 'active')`, u.id).Error)
	}

	svc := &usageReportService{db: db}
	r, err := svc.BuildUsageReport(context.Background(), 10000, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 1, r.TotalUsers, "only WeCom SSO users are counted")
	assert.Len(t, r.Rows, 1)
	assert.Equal(t, "u-wecom", r.Rows[0].UserID)
	assert.True(t, r.Rows[0].IsWeCom)
	assert.Equal(t, 1, r.Qualified+r.Unqualified)
}
