package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// 每日使用报告：统计前一天空间成员的登录/对话情况（达标 = 登录 ≥1 且
// 对话 ≥2），渲染成企微应用消息推送给所选通知人。配置存
// tenants.usage_report_config（见 types.UsageReportConfig）。
//
// 统计口径：
//   - 登录次数 = auth_tokens 中 refresh_token 行数（仅登录时创建，token
//     刷新不新增行），按 created_at 落在统计日计。
//   - 对话次数 = 该空间内用户当日发出的消息数（messages.role='user'）。
//   - 上次活跃 = 该空间内用户最近一条消息时间，无消息时取最近登录。
//   - 较前日 = 与统计日前一天（D-2）的对应指标对比。

const (
	usageReportLoginMin = 1
	usageReportChatMin  = 2
	// 企微 markdown 应用消息上限 4096 字节；明细行超限时截断并提示。
	usageReportMaxBytes  = 4000
	usageReportMaxRows   = 50
	wecomSyntheticPrefix = "wecom_"
	wecomSyntheticDomain = "@wecom.sso.weknora.local"
)

// UsageReportRow 用户使用明细一行。
type UsageReportRow struct {
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	IsWeCom    bool      `json:"is_wecom"`
	Logins     int       `json:"logins"`
	Chats      int       `json:"chats"`
	Qualified  bool      `json:"qualified"`
	LastActive time.Time `json:"last_active"`
}

// UsageReport 一次统计的完整结果。
type UsageReport struct {
	TenantID      uint64           `json:"tenant_id"`
	TenantName    string           `json:"tenant_name"`
	Date          string           `json:"date"` // 统计日 yyyy-mm-dd
	TotalUsers    int              `json:"total_users"`
	Qualified     int              `json:"qualified"`
	Unqualified   int              `json:"unqualified"`
	TotalMessages int              `json:"total_messages"`
	PrevQualified int              `json:"prev_qualified"`
	PrevMessages  int              `json:"prev_messages"`
	Rows          []UsageReportRow `json:"rows"`
}

// UsageReportService 每日使用报告服务。
type UsageReportService interface {
	BuildUsageReport(ctx context.Context, tenantID uint64, day time.Time) (*UsageReport, error)
	RenderUsageReportMarkdown(r *UsageReport, generatedAt time.Time) string
	SendUsageReport(ctx context.Context, tenant *types.Tenant, day time.Time, markRun bool) error
	SendTestUsageReport(ctx context.Context, tenant *types.Tenant) (*UsageReport, error)
	GetUsageReportConfig(ctx context.Context, tenantID uint64) (*types.UsageReportConfig, error)
	SetUsageReportConfig(ctx context.Context, tenantID uint64, cfg *types.UsageReportConfig) error
}

type usageReportService struct {
	db *gorm.DB
}

// NewUsageReportService 构造函数（容器注入）。
func NewUsageReportService(db *gorm.DB) UsageReportService {
	return &usageReportService{db: db}
}

func (s *usageReportService) GetUsageReportConfig(ctx context.Context, tenantID uint64) (*types.UsageReportConfig, error) {
	var tenant types.Tenant
	if err := s.db.WithContext(ctx).Select("id", "usage_report_config").First(&tenant, "id = ?", tenantID).Error; err != nil {
		return nil, err
	}
	return tenant.UsageReportConfig, nil
}

func (s *usageReportService) SetUsageReportConfig(ctx context.Context, tenantID uint64, cfg *types.UsageReportConfig) error {
	return s.db.WithContext(ctx).
		Model(&types.Tenant{}).
		Where("id = ?", tenantID).
		Updates(map[string]any{
			"usage_report_config": cfg,
			"updated_at":          time.Now(),
		}).Error
}

// dayWindow 返回统计日的本地 [起点, 终点)。
func dayWindow(day time.Time) (time.Time, time.Time) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return start, start.AddDate(0, 0, 1)
}

func (s *usageReportService) loginCounts(ctx context.Context, userIDs []string, start, end time.Time) (map[string]int, error) {
	counts := map[string]int{}
	if len(userIDs) == 0 {
		return counts, nil
	}
	type row struct {
		UserID string
		N      int
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Table("auth_tokens").
		Select("user_id, COUNT(*) AS n").
		Where("user_id IN ? AND token_type = ? AND created_at >= ? AND created_at < ?", userIDs, "refresh_token", start, end).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.UserID] = r.N
	}
	return counts, nil
}

func (s *usageReportService) chatCounts(ctx context.Context, tenantID uint64, start, end time.Time) (map[string]int, error) {
	type row struct {
		UserID string
		N      int
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Model(&types.Message{}).
		Select("user_id, COUNT(*) AS n").
		Where("tenant_id = ? AND role = ? AND created_at >= ? AND created_at < ?", tenantID, "user", start, end).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.UserID] = r.N
	}
	return counts, nil
}

func (s *usageReportService) lastActive(ctx context.Context, tenantID uint64) (map[string]time.Time, error) {
	type row struct {
		UserID string
		Last   time.Time
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Model(&types.Message{}).
		Select("user_id, MAX(created_at) AS last").
		Where("tenant_id = ? AND role = ?", tenantID, "user").
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	m := map[string]time.Time{}
	for _, r := range rows {
		m[r.UserID] = r.Last
	}
	return m, nil
}

func (s *usageReportService) BuildUsageReport(ctx context.Context, tenantID uint64, day time.Time) (*UsageReport, error) {
	var tenant types.Tenant
	if err := s.db.WithContext(ctx).
		Select("id", "name", "usage_report_config").
		First(&tenant, "id = ?", tenantID).Error; err != nil {
		return nil, err
	}

	// 空间内在职成员（软删/退出不计）。
	var members []types.TenantMember
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, string(types.TenantMemberStatusActive)).
		Find(&members).Error; err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	users := map[string]types.User{}
	if len(userIDs) > 0 {
		var ul []types.User
		if err := s.db.WithContext(ctx).
			Select("id", "username", "email").
			Where("id IN ?", userIDs).
			Find(&ul).Error; err != nil {
			return nil, err
		}
		for _, u := range ul {
			users[u.ID] = u
		}
	}

	start, end := dayWindow(day)
	logins, err := s.loginCounts(ctx, userIDs, start, end)
	if err != nil {
		return nil, fmt.Errorf("count logins: %w", err)
	}
	chats, err := s.chatCounts(ctx, tenantID, start, end)
	if err != nil {
		return nil, fmt.Errorf("count chats: %w", err)
	}
	active, err := s.lastActive(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("last active: %w", err)
	}

	// 前一天（D-2）对比指标。
	prevStart := start.AddDate(0, 0, -1)
	prevChats, err := s.chatCounts(ctx, tenantID, prevStart, start)
	if err != nil {
		return nil, err
	}
	prevLogins, err := s.loginCounts(ctx, userIDs, prevStart, start)
	if err != nil {
		return nil, err
	}
	prevQualified := 0
	prevMessages := 0
	for _, m := range members {
		if prevLogins[m.UserID] >= usageReportLoginMin && prevChats[m.UserID] >= usageReportChatMin {
			prevQualified++
		}
		prevMessages += prevChats[m.UserID]
	}

	report := &UsageReport{
		TenantID:      tenantID,
		TenantName:    tenant.Name,
		Date:          start.Format("2006-01-02"),
		TotalUsers:    len(members),
		PrevQualified: prevQualified,
		PrevMessages:  prevMessages,
	}
	for _, m := range members {
		u := users[m.UserID]
		row := UsageReportRow{
			UserID:   m.UserID,
			Username: displayUsername(u),
			IsWeCom:  IsWeComSyntheticEmail(u.Email),
			Logins:   logins[m.UserID],
			Chats:    chats[m.UserID],
			LastActive: active[m.UserID],
		}
		row.Qualified = row.Logins >= usageReportLoginMin && row.Chats >= usageReportChatMin
		if row.Qualified {
			report.Qualified++
		}
		report.TotalMessages += row.Chats
		report.Rows = append(report.Rows, row)
	}
	sortUsageRows(report.Rows)
	report.Unqualified = report.TotalUsers - report.Qualified
	return report, nil
}

// lastActive 兜底：无消息的用户显示"从未活跃"。

func displayUsername(u types.User) string {
	if strings.TrimSpace(u.Username) != "" {
		return u.Username
	}
	if strings.TrimSpace(u.Email) != "" {
		return u.Email
	}
	return "未知用户"
}

// IsWeComSyntheticEmail 判定企微 SSO 开号的合成邮箱。
func IsWeComSyntheticEmail(email string) bool {
	return strings.HasPrefix(email, wecomSyntheticPrefix) && strings.HasSuffix(email, wecomSyntheticDomain)
}

// WeComUserIDFromEmail 从合成邮箱还原企微 userid（收件人用）。
func WeComUserIDFromEmail(email string) (string, bool) {
	if !IsWeComSyntheticEmail(email) {
		return "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(email, wecomSyntheticPrefix), wecomSyntheticDomain)
	inner = strings.TrimSuffix(inner, "@wecom")
	return inner, inner != ""
}

func sortUsageRows(rows []UsageReportRow) {
	// 达标在前，其次对话多、登录多、名字稳定。
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && usageRowLess(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func usageRowLess(a, b UsageReportRow) bool {
	if a.Qualified != b.Qualified {
		return a.Qualified
	}
	if a.Chats != b.Chats {
		return a.Chats > b.Chats
	}
	if a.Logins != b.Logins {
		return a.Logins > b.Logins
	}
	return a.Username < b.Username
}

func pct(part, total int) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// RenderUsageReportMarkdown 渲染为企微应用 markdown 消息。
func (s *usageReportService) RenderUsageReportMarkdown(r *UsageReport, generatedAt time.Time) string {
	weekday := map[string]string{"Sunday": "周日", "Monday": "周一", "Tuesday": "周二", "Wednesday": "周三", "Thursday": "周四", "Friday": "周五", "Saturday": "周六"}[generatedAt.Format("Monday")]

	var b strings.Builder
	fmt.Fprintf(&b, "## 📊 %s 使用日报\n**%s（%s）**\n", r.TenantName, r.Date, weekday)
	fmt.Fprintf(&b, "> 👥 总用户数：**%d**\n", r.TotalUsers)
	fmt.Fprintf(&b, "> <font color=\"info\">✅ 达标用户：%d（%s）</font>\n", r.Qualified, pct(r.Qualified, r.TotalUsers))
	fmt.Fprintf(&b, "> <font color=\"warning\">⭕ 未达标用户：%d（%s）</font>\n", r.Unqualified, pct(r.Unqualified, r.TotalUsers))
	fmt.Fprintf(&b, "> 💬 昨日消息总数：**%d**（较前日 %s）\n", r.TotalMessages, deltaPercent(r.TotalMessages, r.PrevMessages))
	fmt.Fprintf(&b, "\n**达标标准**：登录 ≥1 次 且 对话 ≥2 次\n")
	fmt.Fprintf(&b, "\n**用户明细**\n")

	rows := r.Rows
	if len(rows) > usageReportMaxRows {
		rows = rows[:usageReportMaxRows]
	}
	shown := 0
	for _, row := range rows {
		line := fmt.Sprintf("%s：登录 %d｜对话 %d｜%s｜活跃 %s\n",
			row.Username, row.Logins, row.Chats,
			qualifiedTag(row.Qualified), lastActiveText(row.LastActive, generatedAt))
		if b.Len()+len(line) > usageReportMaxBytes {
			break
		}
		b.WriteString(line)
		shown++
	}
	if hidden := len(r.Rows) - shown; hidden > 0 {
		fmt.Fprintf(&b, "…其余 %d 人略\n", hidden)
	}

	fmt.Fprintf(&b, "\n**昨日小结**\n")
	cur := ratePermille(r.Qualified, r.TotalUsers)
	prev := ratePermille(r.PrevQualified, r.TotalUsers)
	diffPP := float64(cur-prev) / 10.0
	switch {
	case cur > prev:
		fmt.Fprintf(&b, "整体达标率 %s，较前日 +%.1f 个百分点，趋势向好 📈\n", pct(r.Qualified, r.TotalUsers), diffPP)
	case cur < prev:
		fmt.Fprintf(&b, "整体达标率 %s，较前日 %.1f 个百分点，有所回落 📉\n", pct(r.Qualified, r.TotalUsers), diffPP)
	default:
		fmt.Fprintf(&b, "整体达标率 %s，与前日持平 ➖\n", pct(r.Qualified, r.TotalUsers))
	}
	fmt.Fprintf(&b, "\n<font color=\"comment\">报告生成：%s</font>\n", generatedAt.Format("2006-01-02 15:04"))
	return b.String()
}

func qualifiedTag(ok bool) string {
	if ok {
		return "<font color=\"info\">达标</font>"
	}
	return "<font color=\"warning\">未达标</font>"
}

func lastActiveText(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "从未"
	}
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return "今天 " + t.Format("15:04")
	}
	return t.Format("01-02 15:04")
}

func deltaPercent(cur, prev int) string {
	if prev <= 0 {
		if cur <= 0 {
			return "持平"
		}
		return fmt.Sprintf("+%d 条", cur)
	}
	d := float64(cur-prev) / float64(prev) * 100
	sign := "+"
	if d < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, d)
}

func ratePermille(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 1000 / total
}

// --- 企微应用消息发送（与 IM webhook 适配器同款 API，独立最小实现） ---

var (
	wecomReportTokenMu   sync.Mutex
	wecomReportTokenVal  string
	wecomReportTokenExp  time.Time
	wecomReportTokenCorp string
	wecomReportTokenSec  string
)

func wecomReportAccessToken(ctx context.Context, corpID, secret string) (string, error) {
	wecomReportTokenMu.Lock()
	defer wecomReportTokenMu.Unlock()
	if wecomReportTokenVal != "" && wecomReportTokenCorp == corpID && wecomReportTokenSec == secret && time.Now().Before(wecomReportTokenExp) {
		return wecomReportTokenVal, nil
	}
	endpoint := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(corpID), url.QueryEscape(secret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	var data struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.ErrCode != 0 || data.AccessToken == "" {
		return "", fmt.Errorf("wecom gettoken failed: %s (errcode=%d)", data.ErrMsg, data.ErrCode)
	}
	wecomReportTokenVal = data.AccessToken
	wecomReportTokenCorp = corpID
	wecomReportTokenSec = secret
	wecomReportTokenExp = time.Now().Add(110 * time.Minute)
	return wecomReportTokenVal, nil
}

// wecomReportRecipients 把配置的 WeKnora 用户解析为企微 userid。
// 非企微开号的成员收不到应用消息，返回时跳过并记录。
func (s *usageReportService) wecomReportRecipients(ctx context.Context, tenantID uint64, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	var users []types.User
	if err := s.db.WithContext(ctx).
		Select("id", "email").
		Where("id IN ?", userIDs).
		Find(&users).Error; err != nil {
		return nil, err
	}
	var out []string
	for _, u := range users {
		if wid, ok := WeComUserIDFromEmail(u.Email); ok {
			out = append(out, wid)
		} else {
			logger.Warnf(ctx, "[UsageReport] member %s is not a WeCom SSO user, skipped as recipient", u.ID)
		}
	}
	return out, nil
}

func (s *usageReportService) sendWeComMarkdown(ctx context.Context, tenant *types.Tenant, recipients []string, content string) error {
	w := tenant.SSOConfig.WeCom
	if w == nil {
		return fmt.Errorf("workspace has no WeCom app credentials")
	}
	agentID := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(w.AgentID), "%d", &agentID); err != nil {
		// 兼容未配置 AgentID 的空间：企微 message/send 必须携带 agentid。
		return fmt.Errorf("invalid WeCom agent_id %q: %w", w.AgentID, err)
	}
	token, err := wecomReportAccessToken(ctx, w.CorpID, w.CorpSecret)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"touser":  strings.Join(recipients, "|"),
		"msgtype": "markdown",
		"agentid": agentID,
		"markdown": map[string]string{
			"content": content,
		},
	}
	body, _ := json.Marshal(payload)
	sendURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom message/send failed: %s (errcode=%d)", result.ErrMsg, result.ErrCode)
	}
	return nil
}

// SendUsageReport 生成统计日的报告并按配置推送；markRun 为 true 时落
// last_run_date（定时任务路径），测试发送不落。
func (s *usageReportService) SendUsageReport(ctx context.Context, tenant *types.Tenant, day time.Time, markRun bool) error {
	cfg := tenant.UsageReportConfig
	if cfg == nil {
		return fmt.Errorf("usage report not configured")
	}
	report, err := s.BuildUsageReport(ctx, tenant.ID, day)
	if err != nil {
		return err
	}
	now := time.Now()
	markdown := s.RenderUsageReportMarkdown(report, now)
	logger.Infof(ctx, "[UsageReport] tenant=%s date=%s users=%d qualified=%d messages=%d",
		tenant.Name, report.Date, report.TotalUsers, report.Qualified, report.TotalMessages)

	if cfg.PushToWeCom && len(cfg.NotifyUserIDs) > 0 && tenant.SSOConfig != nil && tenant.SSOConfig.WeComEnabled() {
		recipients, err := s.wecomReportRecipients(ctx, tenant.ID, cfg.NotifyUserIDs)
		if err != nil {
			return fmt.Errorf("resolve recipients: %w", err)
		}
		if len(recipients) == 0 {
			return fmt.Errorf("no WeCom recipients resolvable from notify_user_ids")
		}
		if err := s.sendWeComMarkdown(ctx, tenant, recipients, markdown); err != nil {
			return fmt.Errorf("send wecom: %w", err)
		}
		logger.Infof(ctx, "[UsageReport] pushed to %d WeCom recipient(s)", len(recipients))
	}

	if markRun {
		cfg.LastRunDate = now.Format("2006-01-02")
		return s.SetUsageReportConfig(ctx, tenant.ID, cfg)
	}
	return nil
}

// SendTestUsageReport 立即生成昨天的报告并推送（设置页"立即测试"）。
func (s *usageReportService) SendTestUsageReport(ctx context.Context, tenant *types.Tenant) (*UsageReport, error) {
	yesterday := time.Now().In(time.Local).AddDate(0, 0, -1)
	if err := s.SendUsageReport(ctx, tenant, yesterday, false); err != nil {
		return nil, err
	}
	return s.BuildUsageReport(ctx, tenant.ID, yesterday)
}
