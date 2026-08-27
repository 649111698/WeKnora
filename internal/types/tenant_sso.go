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

// KingdeeTenantSSO 金蝶苍穹统一门户 SSO 凭证（苍穹 V6.0.015+，
// OAuth2.0 授权码模式，苍穹作为 IdP）。
type KingdeeTenantSSO struct {
	// BaseURL 苍穹站点地址（如 https://kde.erp.com），不带末尾斜杠。
	BaseURL string `json:"base_url,omitempty"`
	// AppClientID 苍穹「系统服务云 → OpenAPI → 第三方应用」的系统编码。
	AppClientID string `json:"app_client_id,omitempty"`
	// AppSecret 苍穹「认证凭证」里自定义的 appSecret。
	AppSecret string `json:"app_secret,omitempty"`
	// ProxyUsername 应用代理的用户名（「获取 Token 示例」里的 username）。
	// 独立部署的第三方必须走 token 模式，此项与 AppSecret/AccountID 配套。
	ProxyUsername string `json:"username,omitempty"`
	// AccountID 数据中心 id（「获取 Token 示例」里的 accountId）。
	AccountID string `json:"account_id,omitempty"`
}

// TenantSSOConfig 租户 SSO 总配置。
type TenantSSOConfig struct {
	// LoginDomain 租户专属登录域名（可选）。配置后按 Host 精确匹配
	// （大小写不敏感，可含端口），匹配到的租户登录页自动使用其 SSO
	// 与水印配置。
	LoginDomain string            `json:"login_domain,omitempty"`
	WeCom       *WeComTenantSSO   `json:"wecom,omitempty"`
	Feishu      *FeishuTenantSSO  `json:"feishu,omitempty"`
	Kingdee     *KingdeeTenantSSO `json:"kingdee,omitempty"`
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

// KingdeeEnabled 苍穹凭证是否齐备（BaseURL + AppClientID）。
func (c *TenantSSOConfig) KingdeeEnabled() bool {
	return c != nil && c.Kingdee != nil &&
		strings.TrimSpace(c.Kingdee.BaseURL) != "" &&
		strings.TrimSpace(c.Kingdee.AppClientID) != ""
}

// TokenMode 独立部署的第三方需走 OpenAPI token 模式：应用凭证
// （client_id/client_secret/username/accountId）换 access_token 后调用
// 业务接口；跑在苍穹框架内的系统才可 code 直连。
func (k *KingdeeTenantSSO) TokenMode() bool {
	return k != nil &&
		strings.TrimSpace(k.AppSecret) != "" &&
		strings.TrimSpace(k.ProxyUsername) != "" &&
		strings.TrimSpace(k.AccountID) != ""
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
	if c.Kingdee != nil {
		k := *c.Kingdee
		if k.AppSecret != "" {
			k.AppSecret = ssoSecretMask
		}
		out.Kingdee = &k
	}
	return out
}

// MergeTenantSSOConfigForUpdate 将提交值合并到已存配置：
// secret 提交 "***"（掩码）或空串均表示保留原值——前端表单加载后密钥
// 输入框为空、占位符提示"保持原密钥"，直接保存不应误清已配置的密钥。
func MergeTenantSSOConfigForUpdate(submitted, existing *TenantSSOConfig) *TenantSSOConfig {
	merged := &TenantSSOConfig{}
	if existing != nil {
		merged = &TenantSSOConfig{
			LoginDomain: existing.LoginDomain,
			WeCom:       existing.WeCom,
			Feishu:      existing.Feishu,
			Kingdee:     existing.Kingdee,
		}
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
		if w.CorpSecret == ssoSecretMask || w.CorpSecret == "" {
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
		if f.AppSecret == ssoSecretMask || f.AppSecret == "" {
			f.AppSecret = prev.AppSecret
		}
		merged.Feishu = &f
	}
	if submitted.Kingdee != nil {
		prev := &KingdeeTenantSSO{}
		if existing != nil && existing.Kingdee != nil {
			prev = existing.Kingdee
		}
		k := *submitted.Kingdee
		if k.AppSecret == ssoSecretMask || k.AppSecret == "" {
			k.AppSecret = prev.AppSecret
		}
		merged.Kingdee = &k
	}
	return merged
}

// WatermarkConfig 租户全站水印配置。未配置（nil）视为关闭。
type WatermarkConfig struct {
	Enabled bool `json:"enabled"`
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

// ConversationConfig 租户级对话体验配置：隐藏输入框模型下拉并锁定对话模型。
// 未配置（nil）视为不隐藏，成员可自由选择模型。
type ConversationConfig struct {
	// ModelSelectorHidden 隐藏问答输入框的模型选择器。
	ModelSelectorHidden bool `json:"model_selector_hidden"`
	// DefaultModelID 隐藏选择器时锁定的对话模型（KnowledgeQA 类型）。
	DefaultModelID string `json:"default_model_id,omitempty"`
}

// ResolvedConversation 返回生效的对话配置；nil 回退为不隐藏。
func (c *ConversationConfig) ResolvedConversation() ConversationConfig {
	if c == nil {
		return ConversationConfig{}
	}
	return ConversationConfig{
		ModelSelectorHidden: c.ModelSelectorHidden,
		DefaultModelID:      strings.TrimSpace(c.DefaultModelID),
	}
}

// Value implements driver.Valuer for tenants.conversation_config.
func (c ConversationConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner for tenants.conversation_config.
func (c *ConversationConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}
