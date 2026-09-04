package service

import (
	"os"
	"testing"
	"time"
)

// TestRenderUsageReportImagePreview 生成样例报告 PNG 到
// WEKNORA_REPORT_PREVIEW 指定路径，便于本地人工查看版式。
// 需要 WEKNORA_REPORT_FONT 指向本机 CJK 字体，否则跳过。
// TestReportCanvasRoundRectInPlace 圆角矩形必须画在 (x,y) 原位：
// 中心命中、右缘之外与直角处未命中。历史上绝对坐标 bug 让所有
// 圆角矩形整体偏移被裁切，页面错乱。
func TestReportCanvasRoundRectInPlace(t *testing.T) {
	if os.Getenv("WEKNORA_REPORT_FONT") == "" {
		t.Skip("needs WEKNORA_REPORT_FONT")
	}
	cv, err := newReportCanvas(300)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	defer cv.close()
	red := reportColor{0xFF, 0x00, 0x00}
	cv.fillRoundRect(50, 50, 100, 80, 20, red)
	at := func(x, y int) [3]uint8 {
		r, g, b, _ := cv.img.At(x, y).RGBA()
		return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
	}
	if p := at(100, 90); p != [3]uint8{0xFF, 0, 0} {
		t.Fatalf("center should be red, got %v", p)
	}
	if p := at(200, 90); p[0] > 0xF0 && p[1] < 0x10 {
		t.Fatalf("pixel right of the rect must not be red, got %v", p)
	}
	if p := at(50, 50); p[0] > 0xF0 && p[1] < 0x10 {
		t.Fatalf("square corner must be clipped by rounding, got %v", p)
	}
}

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
			{Username: "周正伟", Logins: 3, Chats: 28, Qualified: true, LastActive: now.Add(-2 * time.Hour)},
			{Username: "Mountain", Logins: 2, Chats: 12, Qualified: true, LastActive: now.Add(-19 * time.Hour)},
			{Username: "王芳", Logins: 1, Chats: 0, Qualified: false, LastActive: now.Add(-26 * time.Hour)},
			{Username: "19952610696", Logins: 0, Chats: 0, Qualified: false, LastActive: time.Time{}},
			{Username: "陈丽华", Logins: 1, Chats: 1, Qualified: false, LastActive: now.Add(-30 * time.Hour)},
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
