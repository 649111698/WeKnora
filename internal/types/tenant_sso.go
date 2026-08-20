package types

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
)

/*
 * 租户级 SSO 与水印配置（tenants 表 jsonb 列）。
 *
 * 每个租户可独立配置企微/飞书自建应用凭证与专属登录域名
 * （sso_config），以及全站水印（watermark_config）。登录入口通过
 * ?t=<tenant_id> 参数或租户专属域名（Host 头）定位租户；两者都缺省
 * 时，若全实例恰好只有一个租户配置了对应平台的 SSO，则回退到它
 * （单企业部署无需改登录链接）。
 *
 * Secret 类字段（corp_secret / app_secret）在 GET 响应中打码为
 * "***"，PUT 提交字面量 "***" 表示保留原值——与系统设置的密钥
 * 处理约定一致。
 */

const ssoSecretMask = "***"

// WeComTenantSSO 企微自建应用凭证。
type WeComTenantSSO struct {
	CorpID           string `json:"corp_id,omitempty"`
	CorpSecret       string `json:"corp_secret,omitempty"`
	AgentID          string `json:"agent_id,omitempty"`
	DomainVerifyText string `json:"domain_verify_text,omitempty"`
}

// FeishuTenantSSO 飞书自建应用凭证。
type FeishuTenantSSO struct {
	AppID     string `json:"app_id,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
}

// TenantSSOConfig 租户 SSO 总配置。
type TenantSSOConfig struct {
	// LoginDomain 租户专属登录域名（可选）。配置后按 Host 精确匹配
	// （大小写不敏感，可含端口），匹配到的租户登录页自动使用其 SSO
	// 与水印配置。
	LoginDomain string           `json:"login_domain,omitempty"`
	WeCom       *WeComTenantSSO  `json:"wecom,omitempty"`
	Feishu      *FeishuTenantSSO `json:"feishu,omitempty"`
}

// WeComEnabled 企微凭证是否齐备（CorpID + Secret）。
func (c *TenantSSOConfig) WeComEnabled() bool {
	return c != nil && c.WeCom != nil &&
		strings.TrimSpace(c.WeCom.CorpID) != "" &&
		strings.TrimSpace(c.WeCom.CorpSecret) != ""
}

// FeishuEnabled 飞书凭证是否齐备（AppID + Secret）。
func (c *TenantSSOConfig) FeishuEnabled() bool {
	return c != nil && c.Feishu != nil &&
		strings.TrimSpace(c.Feishu.AppID) != "" &&
		strings.TrimSpace(c.Feishu.AppSecret) != ""
}

// TenantSSOConfigForResponse 返回打码后的配置（secret → "***"）。
func TenantSSOConfigForResponse(c *TenantSSOConfig) *TenantSSOConfig {
	if c == nil {
		return nil
	}
	out := &TenantSSOConfig{LoginDomain: c.LoginDomain}
	if c.WeCom != nil {
		w := *c.WeCom
		if w.CorpSecret != "" {
			w.CorpSecret = ssoSecretMask
		}
		out.WeCom = &w
	}
	if c.Feishu != nil {
		f := *c.Feishu
		if f.AppSecret != "" {
			f.AppSecret = ssoSecretMask
		}
		out.Feishu = &f
	}
	return out
}

// MergeTenantSSOConfigForUpdate 将提交值合并到已存配置：
// 提交 "***" 的 secret 表示保留原值；提交空串表示清除。
func MergeTenantSSOConfigForUpdate(submitted, existing *TenantSSOConfig) *TenantSSOConfig {
	merged := &TenantSSOConfig{}
	if existing != nil {
		merged = &TenantSSOConfig{LoginDomain: existing.LoginDomain, WeCom: existing.WeCom, Feishu: existing.Feishu}
	}
	if submitted == nil {
		return merged
	}
	merged.LoginDomain = strings.TrimSpace(submitted.LoginDomain)
	if submitted.WeCom != nil {
		prev := &WeComTenantSSO{}
		if existing != nil && existing.WeCom != nil {
			prev = existing.WeCom
		}
		w := *submitted.WeCom
		if w.CorpSecret == ssoSecretMask {
			w.CorpSecret = prev.CorpSecret
		}
		merged.WeCom = &w
	}
	if submitted.Feishu != nil {
		prev := &FeishuTenantSSO{}
		if existing != nil && existing.Feishu != nil {
			prev = existing.Feishu
		}
		f := *submitted.Feishu
		if f.AppSecret == ssoSecretMask {
			f.AppSecret = prev.AppSecret
		}
		merged.Feishu = &f
	}
	return merged
}

// WatermarkConfig 租户全站水印配置。未配置（nil）视为关闭。
type WatermarkConfig struct {
	Enabled bool   `json:"enabled"`
	// Text 水印文案，支持 {username} 占位符（登录后替换为用户名）。
	Text string `json:"text,omitempty"`
}

// ResolvedWatermark 返回生效的水印配置；nil 配置回退为关闭 + 默认文案。
func (c *WatermarkConfig) Resolved() WatermarkConfig {
	if c == nil {
		return WatermarkConfig{Enabled: false, Text: "{username}"}
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		text = "{username}"
	}
	return WatermarkConfig{Enabled: c.Enabled, Text: text}
}

// Value implements driver.Valuer so gorm persists the whole config as one
// JSON column (tenants.sso_config).
func (c TenantSSOConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner to hydrate the config from the JSON column.
func (c *TenantSSOConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// Value implements driver.Valuer for tenants.watermark_config.
func (c WatermarkConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner for tenants.watermark_config.
func (c *WatermarkConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}
