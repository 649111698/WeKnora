package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

/*
 * 未达标成员个人提醒（样式A 文本卡片）。
 *
 * 每天 9 点日报推送给管理员后，再给每个未达标的企微成员本人单独发
 * 一张 textcard：告知其昨日登录/对话次数与差距，按钮直达新会话页
 * （/platform/creatChat）降低“回去用一下”的门槛。
 *
 * 只有企微 SSO 开号的成员能收到（合成邮箱 → 企微 userid）；发送失败
 * 仅记录并继续，不影响日报本身。按钮 URL 优先取租户登录域名，回退
 * FRONTEND_BASE_URL，两者都缺时跳过提醒（textcard 必须绝对链接）。
 */

// wecomTextCard 企微文本卡片消息（msgtype=textcard）。
type wecomTextCard struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	BtnTxt      string `json:"btntxt"`
}

// reminderBaseURL 解析提醒卡片按钮的站点根地址。
func reminderBaseURL(tenant *types.Tenant, cfg *config.Config) string {
	candidates := []string{}
	if tenant != nil && tenant.SSOConfig != nil {
		candidates = append(candidates, strings.TrimSpace(tenant.SSOConfig.LoginDomain))
	}
	if cfg != nil {
		candidates = append(candidates, strings.TrimSpace(cfg.FrontendBaseURL))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if !strings.HasPrefix(c, "http://") && !strings.HasPrefix(c, "https://") {
			c = "https://" + c
		}
		return strings.TrimRight(c, "/")
	}
	return ""
}

// unqualifiedGapTip 按差距给一句行动提示。
func unqualifiedGapTip(logins, chats int) string {
	needLogin := logins < usageReportLoginMin
	needChat := chats < usageReportChatMin
	switch {
	case needLogin && needChat:
		return "今天登录并使用即可恢复达标"
	case needChat:
		return fmt.Sprintf("还差 %d 次对话即达标", usageReportChatMin-chats)
	default:
		return fmt.Sprintf("还差 %d 次登录即达标", usageReportLoginMin-logins)
	}
}

// unqualifiedReminderCard 组装个人提醒卡片。
func unqualifiedReminderCard(reportDate string, row UsageReportRow, baseURL string) wecomTextCard {
	// reportDate 形如 2026-09-03，标题里展示 9月3日。
	monthDay := reportDate
	if t, err := time.Parse("2006-01-02", reportDate); err == nil {
		monthDay = fmt.Sprintf("%d月%d日", t.Month(), t.Day())
	}
	name := row.Username
	if name == "" {
		name = "你"
	}
	desc := strings.Join([]string{
		fmt.Sprintf("<div class=\"normal\">%s，你昨日登录 %d 次、对话 %d 条，未达到使用标准</div>", name, row.Logins, row.Chats),
		fmt.Sprintf("<div class=\"highlight\">%s</div>", unqualifiedGapTip(row.Logins, row.Chats)),
		"<div class=\"gray\">达标标准：登录 ≥1 次 且 对话 ≥2 次 · 统计每日 9:00 更新</div>",
	}, "")
	return wecomTextCard{
		Title:       fmt.Sprintf("昨日使用未达标（%s）", monthDay),
		Description: desc,
		URL:         baseURL + "/platform/creatChat",
		BtnTxt:      "立即使用",
	}
}

// sendWeComTextCard 发送文本卡片到指定企微 userid 列表。
func (s *usageReportService) sendWeComTextCard(ctx context.Context, tenant *types.Tenant, recipients []string, card wecomTextCard) error {
	w := tenant.SSOConfig.WeCom
	agentID := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(w.AgentID), "%d", &agentID); err != nil {
		return fmt.Errorf("invalid WeCom agent_id %q: %w", w.AgentID, err)
	}
	token, err := wecomReportAccessToken(ctx, w.CorpID, w.CorpSecret)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"touser":  strings.Join(recipients, "|"),
		"msgtype": "textcard",
		"agentid": agentID,
		"textcard": map[string]string{
			"title":       card.Title,
			"description": card.Description,
			"url":         card.URL,
			"btntxt":      card.BtnTxt,
		},
	}
	return postWeComMessage(ctx, token, payload)
}

// sendUnqualifiedReminders 给每个未达标的企微成员本人发提醒卡片。
// 返回成功提醒的人数；单个失败只记日志，不中断整批。
func (s *usageReportService) sendUnqualifiedReminders(ctx context.Context, tenant *types.Tenant, report *UsageReport, baseURL string) int {
	if baseURL == "" {
		logger.Warnf(ctx, "[UsageReport] skip unqualified reminders: no absolute base URL (login_domain / FRONTEND_BASE_URL)")
		return 0
	}
	sent := 0
	for _, row := range report.Rows {
		if row.Qualified || !row.IsWeCom || row.Email == "" {
			continue
		}
		userID, ok := WeComUserIDFromEmail(row.Email)
		if !ok || userID == "" {
			continue
		}
		card := unqualifiedReminderCard(report.Date, row, baseURL)
		if err := s.sendWeComTextCard(ctx, tenant, []string{userID}, card); err != nil {
			logger.Warnf(ctx, "[UsageReport] reminder to %s failed: %v", userID, err)
			continue
		}
		sent++
	}
	return sent
}

// wecomSendResult message/send 响应。errcode=0 仅表示请求受理成功；
// 部分接收人不可达（不在应用可见范围 / userid 已不存在）时 errcode
// 仍为 0，但 invaliduser 会列出失败的 userid——必须显式检查，否则
// "推送成功"的日志会掩盖静默丢人。
type wecomSendResult struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	InvalidUser string `json:"invaliduser"`
}

func (r wecomSendResult) logPartialFailures(ctx context.Context, recipients []string) {
	if r.ErrCode != 0 || r.InvalidUser == "" {
		return
	}
	bad := strings.Split(r.InvalidUser, "|")
	// 企业微信把 userid 规范成小写回传；按小写匹配原始列表恢复原形。
	lower := map[string]string{}
	for _, u := range recipients {
		lower[strings.ToLower(u)] = u
	}
	named := make([]string, 0, len(bad))
	for _, u := range bad {
		if orig, ok := lower[strings.ToLower(u)]; ok {
			named = append(named, orig)
		} else {
			named = append(named, u)
		}
	}
	logger.Warnf(ctx, "[UsageReport] WeCom dropped unreachable recipient(s): %s (检查企微通讯录/应用可见范围)",
		strings.Join(named, ", "))
}

// postWeComMessage 统一的 message/send POST（text/textcard/image 共用）。
func postWeComMessage(ctx context.Context, accessToken string, payload map[string]any) error {
	body, _ := json.Marshal(payload)
	sendURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + url.QueryEscape(accessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result wecomSendResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom message/send(%v) failed: %s (errcode=%d)",
			payload["msgtype"], result.ErrMsg, result.ErrCode)
	}
	result.logPartialFailures(ctx, splitTouser(payload))
	return nil
}

func splitTouser(payload map[string]any) []string {
	raw, _ := payload["touser"].(string)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "|")
}
