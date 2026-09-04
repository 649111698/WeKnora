package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestUnqualifiedReminderCard(t *testing.T) {
	row := UsageReportRow{
		Username: "Mountain",
		IsWeCom:  true,
		Email:    "wecom_Mountain@wecom.sso.weknora.local",
		Logins:   0,
		Chats:    0,
	}
	card := unqualifiedReminderCard("2026-09-03", row, "https://rag.moldcio.com")
	if want := "昨日使用未达标（9月3日）"; card.Title != want {
		t.Fatalf("title=%q want %q", card.Title, want)
	}
	if !strings.Contains(card.Description, "Mountain，你昨日登录 0 次、对话 0 条") {
		t.Fatalf("description missing usage line: %q", card.Description)
	}
	if !strings.Contains(card.Description, "今天登录并使用即可恢复达标") {
		t.Fatalf("description missing gap tip: %q", card.Description)
	}
	if !strings.Contains(card.Description, "达标标准：登录 ≥1 次 且 对话 ≥2 次") {
		t.Fatalf("description missing standard line: %q", card.Description)
	}
	if card.URL != "https://rag.moldcio.com/platform/creatChat" {
		t.Fatalf("url=%q want new-chat page", card.URL)
	}
	if card.BtnTxt != "立即使用" {
		t.Fatalf("btntxt=%q", card.BtnTxt)
	}
}

func TestUnqualifiedGapTip(t *testing.T) {
	cases := []struct {
		logins, chats int
		want          string
	}{
		{0, 0, "今天登录并使用即可恢复达标"},
		{1, 1, "还差 1 次对话即达标"},
		{2, 0, "还差 2 次对话即达标"},
		{0, 3, "还差 1 次登录即达标"},
	}
	for _, c := range cases {
		if got := unqualifiedGapTip(c.logins, c.chats); got != c.want {
			t.Errorf("gapTip(%d,%d)=%q want %q", c.logins, c.chats, got, c.want)
		}
	}
}

func TestReminderBaseURL(t *testing.T) {
	if got := reminderBaseURL(&types.Tenant{SSOConfig: &types.TenantSSOConfig{LoginDomain: "rag.moldcio.com"}}, nil); got != "https://rag.moldcio.com" {
		t.Fatalf("login_domain fallback=%q", got)
	}
	if got := reminderBaseURL(&types.Tenant{}, &config.Config{Server: &config.ServerConfig{}}); got != "" {
		// 空 SSOConfig + 空 FRONTEND_BASE_URL → 无绝对地址，跳过提醒
		t.Fatalf("empty config should yield empty base, got %q", got)
	}
	cfg := &config.Config{}
	cfg.FrontendBaseURL = "https://example.com/"
	if got := reminderBaseURL(&types.Tenant{}, cfg); got != "https://example.com" {
		t.Fatalf("frontend base=%q", got)
	}
}

func TestUsageReportWeekdayUsesReportDate(t *testing.T) {
	// 统计日 2026-09-03 是周四；生成时刻是 9月4日周五——周几必须按统计日。
	generated := time.Date(2026, 9, 4, 9, 0, 0, 0, time.Local)
	if got := usageReportWeekday("2026-09-03", generated); got != "周四" {
		t.Fatalf("weekday=%q want 周四", got)
	}
	if got := usageReportWeekday("2026-09-04", generated); got != "周五" {
		t.Fatalf("same-day weekday=%q want 周五", got)
	}
	if got := usageReportWeekday("bad-date", generated); got != "周五" {
		t.Fatalf("fallback weekday=%q want 周五", got)
	}
}

func TestShouldRemindUnqualifiedDefault(t *testing.T) {
	cfg0 := types.UsageReportConfig{}
	if !cfg0.ShouldRemindUnqualified() {
		t.Fatal("nil flag should default to enabled")
	}
	off := false
	if (&types.UsageReportConfig{RemindUnqualified: &off}).ShouldRemindUnqualified() {
		t.Fatal("explicit off must be respected")
	}
}
