package service

import (
	"os"
	"testing"
	"time"
)

// TestRenderUsageReportImagePreview 生成样例报告 PNG 到
// WEKNORA_REPORT_PREVIEW 指定路径，便于本地人工查看版式。
// 需要 WEKNORA_REPORT_FONT 指向本机 CJK 字体，否则跳过。
func TestRenderUsageReportImagePreview(t *testing.T) {
	preview := os.Getenv("WEKNORA_REPORT_PREVIEW")
	if preview == "" || os.Getenv("WEKNORA_REPORT_FONT") == "" {
		t.Skip("set WEKNORA_REPORT_FONT and WEKNORA_REPORT_PREVIEW to render a preview")
	}
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.Local)
	r := &UsageReport{
		TenantName:    "倍智信息",
		Date:          "2026-09-03",
		TotalUsers:    14,
		Qualified:     2,
		Unqualified:   12,
		TotalMessages: 71,
		PrevQualified: 1,
		PrevMessages:  58,
		Rows: []UsageReportRow{
			{Username: "wecom_zhengwei.zhou-pexetech.com", Logins: 3, Chats: 28, Qualified: true, LastActive: now.Add(-2 * time.Hour)},
			{Username: "wecom_mountain", Logins: 2, Chats: 12, Qualified: true, LastActive: now.Add(-19 * time.Hour)},
			{Username: "wecom_wangfang", Logins: 1, Chats: 0, Qualified: false, LastActive: now.Add(-26 * time.Hour)},
			{Username: "19952610696", Logins: 0, Chats: 0, Qualified: false, LastActive: time.Time{}},
			{Username: "wecom_lihua.chen-pexetech.com", Logins: 1, Chats: 1, Qualified: false, LastActive: now.Add(-30 * time.Hour)},
			{Username: "wecom_zhangsan", Logins: 0, Chats: 3, Qualified: false, LastActive: now.Add(-49 * time.Hour)},
		},
	}
	pngBytes, err := renderUsageReportImage(r, now)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(preview, pngBytes, 0o644); err != nil {
		t.Fatalf("write preview: %v", err)
	}
	t.Logf("preview written to %s (%d bytes)", preview, len(pngBytes))
}
