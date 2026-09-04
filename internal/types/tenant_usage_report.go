package types

import (
	"database/sql/driver"
	"encoding/json"
)

// UsageReportConfig 空间级每日使用报告配置（tenants.usage_report_config）。
//
// 每天 09:00（服务器本地时间）统计前一天空间内企业微信用户的使用情况
// 并推送到所选成员的企微自建应用窗口。nil = 未开启。
type UsageReportConfig struct {
	// Enabled 总开关：关闭后定时任务跳过本空间。
	Enabled bool `json:"enabled"`
	// PushToWeCom 推送到企业微信应用消息（要求空间已配置企微自建应用 SSO）。
	// 关闭时仅后台生成报告（日志可见），不发送。
	PushToWeCom bool `json:"push_to_wecom"`
	// NotifyUserIDs 通知人（WeKnora 用户 ID 列表）。发送时解析为企微
	// userid：仅企微 SSO 开号的成员可收到应用消息。
	NotifyUserIDs []string `json:"notify_user_ids,omitempty"`
	// RemindUnqualified 是否给每个未达标的企微成员本人单独发一张
	// 提醒卡片（样式A 文本卡片）。nil 视为开启——旧配置无此字段时
	// 默认启用，无需迁移。
	RemindUnqualified *bool `json:"remind_unqualified,omitempty"`
	// LastRunDate 上次成功运行的本地日期（yyyy-mm-dd）。调度器用它
	// 防止同一天重复推送，也让重启后能补跑当天错过的 9 点任务。
	LastRunDate string `json:"last_run_date,omitempty"`
}

// ShouldRemindUnqualified 未达标个人提醒是否开启（默认开）。
func (c *UsageReportConfig) ShouldRemindUnqualified() bool {
	return c == nil || c.RemindUnqualified == nil || *c.RemindUnqualified
}

// Value implements driver.Valuer for tenants.usage_report_config.
func (c UsageReportConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner for tenants.usage_report_config.
func (c *UsageReportConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}
