package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

/*
 * 企微/飞书内置浏览器 OAuth 免登 + JIT 自动建号。
 *
 * 流程与 LoginWithOIDC 一致：外部平台 code → 平台用户身份 → 本地用户
 * （不存在则按默认租户策略自动创建，即"访客账号"）→ 签发本地 JWT，
 * 返回结构与 OIDC 登录完全相同（types.OIDCCallbackResponse），前端复用
 * /#oidc_result= 回调链路。
 *
 * 身份映射规则：platform_userid 合成稳定邮箱（wecom_xxx@wecom.sso.local），
 * 同一平台用户重复登录复用同一本地账号。
 */

const (
	ssoHTTPTimeout = 10 * time.Second
	// 平台 access token 有效期一般 2h，提前刷新
	ssoTokenRefreshAhead = 10 * time.Minute
)

type ssoTokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

var (
	ssoTokenCacheMu sync.Mutex
	ssoTokenCache   = map[string]ssoTokenCacheEntry{}
	ssoHTTPClient   = &http.Client{
		Timeout: ssoHTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
)

func ssoCachedToken(key string) (string, bool) {
	ssoTokenCacheMu.Lock()
	defer ssoTokenCacheMu.Unlock()
	entry, ok := ssoTokenCache[key]
	return entry.token, ok && time.Now().Before(entry.expiresAt)
}

func ssoStoreToken(key, token string, ttl time.Duration) {
	ssoTokenCacheMu.Lock()
	defer ssoTokenCacheMu.Unlock()
	ssoTokenCache[key] = ssoTokenCacheEntry{
		token:     token,
		expiresAt: time.Now().Add(ttl - ssoTokenRefreshAhead),
	}
}

func ssoInvalidateToken(key string) {
	ssoTokenCacheMu.Lock()
	defer ssoTokenCacheMu.Unlock()
	delete(ssoTokenCache, key)
}

// GetSSOStatus 返回各 SSO 提供方启用状态与构建授权 URL 所需的公开参数。
// 凭证为租户级（tenants.sso_config），租户按请求 Host 与其专属登录域名
// （login_domain）精确匹配——域名是唯一的区分方式，不匹配即未启用。
func (s *userService) GetSSOStatus(ctx context.Context, host string) (*types.SSOStatusResponse, error) {
	resp := &types.SSOStatusResponse{}
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil || tenant == nil {
		return resp, nil
	}
	cfg := tenant.SSOConfig
	if cfg.WeComEnabled() {
		resp.WeCom = &types.SSOProviderStatus{
			Enabled: true,
			CorpID:  cfg.WeCom.CorpID,
			AgentID: cfg.WeCom.AgentID,
		}
	}
	if cfg.FeishuEnabled() {
		resp.Feishu = &types.SSOProviderStatus{Enabled: true, AppID: cfg.Feishu.AppID}
	}
	if cfg.KingdeeEnabled() {
		resp.Kingdee = &types.SSOProviderStatus{
			Enabled:     true,
			BaseURL:     cfg.Kingdee.BaseURL,
			AppClientID: cfg.Kingdee.AppClientID,
		}
	}
	return resp, nil
}

// GetSSOWatermark 解析未登录页面（登录页）的水印配置，租户解析同 GetSSOStatus。
func (s *userService) GetSSOWatermark(ctx context.Context, host string) types.WatermarkConfig {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil || tenant == nil {
		return (&types.WatermarkConfig{}).Resolved()
	}
	return tenant.WatermarkConfig.Resolved()
}

// GetSSOBranding 返回目标租户的白标品牌配置（登录页在鉴权前使用，
// 经公开的 /auth/config 搭车下发）。未命中域名或未配置时返回零值，
// 前端回退默认文案与默认 Logo。
func (s *userService) GetSSOBranding(ctx context.Context, host string) types.BrandingConfig {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil || tenant == nil {
		return types.BrandingConfig{}
	}
	return tenant.BrandingConfig.ResolvedBranding()
}

// GetSSODomainVerifyText 返回目标租户配置的企微可信域名验证文字。
func (s *userService) GetSSODomainVerifyText(ctx context.Context, host string) string {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil || tenant == nil || tenant.SSOConfig == nil || tenant.SSOConfig.WeCom == nil {
		return ""
	}
	return strings.TrimSpace(tenant.SSOConfig.WeCom.DomainVerifyText)
}

// resolveSSOTenant 定位 SSO 登录的目标租户：请求 Host 与各租户
// login_domain 精确匹配（忽略大小写，可含端口）。域名是唯一的租户
// 区分方式——未匹配到即未启用；不猜测、不回退。
func (s *userService) resolveSSOTenant(ctx context.Context, host string) (*types.Tenant, error) {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return nil, nil
	}
	tenants, err := s.tenantService.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tenants {
		if t.SSOConfig != nil && strings.EqualFold(strings.TrimSpace(t.SSOConfig.LoginDomain), h) {
			return t, nil
		}
	}
	return nil, nil
}

// ssoTenantWeComConfig 取目标租户的企微凭证（要求已配置齐全）。
func (s *userService) ssoTenantWeComConfig(ctx context.Context, host string) (*config.WeComSSOConfig, *types.Tenant, error) {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil {
		return nil, nil, err
	}
	if tenant == nil || !tenant.SSOConfig.WeComEnabled() {
		return nil, nil, errors.NewForbiddenError("WeCom SSO is not configured for this workspace")
	}
	w := tenant.SSOConfig.WeCom
	return &config.WeComSSOConfig{
		CorpID:  w.CorpID,
		Secret:  w.CorpSecret,
		AgentID: w.AgentID,
	}, tenant, nil
}

// ssoTenantFeishuConfig 取目标租户的飞书凭证（要求已配置齐全）。
func (s *userService) ssoTenantFeishuConfig(ctx context.Context, host string) (*config.FeishuSSOConfig, *types.Tenant, error) {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil {
		return nil, nil, err
	}
	if tenant == nil || !tenant.SSOConfig.FeishuEnabled() {
		return nil, nil, errors.NewForbiddenError("Feishu SSO is not configured for this workspace")
	}
	f := tenant.SSOConfig.Feishu
	return &config.FeishuSSOConfig{AppID: f.AppID, AppSecret: f.AppSecret}, tenant, nil
}

// --- 企微 API ---

type wecomTokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
}

type wecomUserInfoResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	UserID  string `json:"userid"`
	OpenID  string `json:"openid"`
}

type wecomUserDetailResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Name    string `json:"name"`
}

func (s *userService) getWeComAccessToken(ctx context.Context, cfg *config.WeComSSOConfig) (string, error) {
	const cacheKey = "wecom:sso:access_token"
	if token, ok := ssoCachedToken(cacheKey); ok {
		return token, nil
	}
	endpoint := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(cfg.CorpID), url.QueryEscape(cfg.Secret),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build WeCom token request: %w", err)
	}
	var data wecomTokenResponse
	if err := ssoDoJSON(ctx, req, &data); err != nil {
		return "", err
	}
	if data.ErrCode != 0 || data.AccessToken == "" {
		return "", fmt.Errorf("WeCom gettoken failed: %s (errcode=%d)", data.ErrMsg, data.ErrCode)
	}
	ssoStoreToken(cacheKey, data.AccessToken, 2*time.Hour)
	return data.AccessToken, nil
}

func (s *userService) getWeComUserIdentity(ctx context.Context, cfg *config.WeComSSOConfig, code string) (userID, displayName string, err error) {
	accessToken, err := s.getWeComAccessToken(ctx, cfg)
	if err != nil {
		return "", "", err
	}
	endpoint := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo?access_token=%s&code=%s",
		url.QueryEscape(accessToken), url.QueryEscape(code),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to build WeCom userinfo request: %w", err)
	}
	var data wecomUserInfoResponse
	if err := ssoDoJSON(ctx, req, &data); err != nil {
		return "", "", err
	}
	if data.ErrCode != 0 {
		return "", "", fmt.Errorf("WeCom getuserinfo failed: %s (errcode=%d)", data.ErrMsg, data.ErrCode)
	}
	// 非企业成员只返回 openid（无 userid），拒绝登录
	if data.UserID == "" {
		return "", "", errors.NewUnauthorizedError("Not a WeCom corp member")
	}

	// 拉取成员姓名做显示名（无通讯录权限时用 userid 兜底，不阻断登录）
	detailEndpoint := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/user/get?access_token=%s&userid=%s",
		url.QueryEscape(accessToken), url.QueryEscape(data.UserID),
	)
	detailReq, derr := http.NewRequestWithContext(ctx, http.MethodGet, detailEndpoint, nil)
	if derr == nil {
		var detail wecomUserDetailResponse
		if derr := ssoDoJSON(ctx, detailReq, &detail); derr == nil && detail.ErrCode == 0 && detail.Name != "" {
			displayName = detail.Name
		}
	}
	return data.UserID, displayName, nil
}

// --- 飞书 API ---

type feishuBaseResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type feishuTenantTokenResponse struct {
	feishuBaseResponse
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // 秒
}

type feishuAuthenTokenResponse struct {
	feishuBaseResponse
	AccessToken string `json:"access_token"` // user_access_token
	OpenID      string `json:"open_id"`
	Name        string `json:"name"`
}

func (s *userService) getFeishuTenantToken(ctx context.Context, cfg *config.FeishuSSOConfig) (string, error) {
	const cacheKey = "feishu:sso:tenant_access_token"
	if token, ok := ssoCachedToken(cacheKey); ok {
		return token, nil
	}
	body := fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, cfg.AppID, cfg.AppSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build Feishu token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	var data feishuTenantTokenResponse
	if err := ssoDoJSON(ctx, req, &data); err != nil {
		return "", err
	}
	if data.Code != 0 || data.TenantAccessToken == "" {
		return "", fmt.Errorf("Feishu tenant_access_token failed: %s (code=%d)", data.Msg, data.Code)
	}
	ttl := time.Duration(data.Expire) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	ssoStoreToken(cacheKey, data.TenantAccessToken, ttl)
	return data.TenantAccessToken, nil
}

func (s *userService) getFeishuIdentity(ctx context.Context, cfg *config.FeishuSSOConfig, code string) (openID, displayName string, err error) {
	tenantToken, err := s.getFeishuTenantToken(ctx, cfg)
	if err != nil {
		return "", "", err
	}
	body := fmt.Sprintf(`{"grant_type":"authorization_code","code":%q}`, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/authen/v1/access_token", strings.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("failed to build Feishu authen request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	var data feishuAuthenTokenResponse
	if err := ssoDoJSON(ctx, req, &data); err != nil {
		return "", "", err
	}
	if data.Code != 0 {
		// token 失效时清缓存让下次重取
		ssoInvalidateToken("feishu:sso:tenant_access_token")
		return "", "", fmt.Errorf("Feishu authen access_token failed: %s (code=%d)", data.Msg, data.Code)
	}
	if data.OpenID == "" {
		return "", "", errors.NewUnauthorizedError("Feishu did not return open_id")
	}
	return data.OpenID, data.Name, nil
}

// --- 金蝶苍穹 API ---

// kingdeeUserInfoResponse 苍穹 authen/getUserInfo 返回结构。
type kingdeeUserInfoResponse struct {
	Data struct {
		Email    string `json:"email"`
		Mobile   string `json:"mobile"`
		Name     string `json:"name"`
		UserName string `json:"userName"`
	} `json:"data"`
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
	Status    bool   `json:"status"`
}

// kingdeeTokenResponse 苍穹 oauth2/token 响应。成功负载可能在 data 内嵌
// 或扁平两种形态，字段都带上兼容。
type kingdeeTokenResponse struct {
	Data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	} `json:"data"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	ErrorCode   string `json:"errorCode"`
	Status      bool   `json:"status"`
	Message     string `json:"message"`
	// 错误形态（如 login.loginBizException）用 description/description_cn
	Description   string `json:"description"`
	DescriptionCn string `json:"description_cn"`
}

// kingdeeTokenRequest 苍穹「获取 Token 示例」的官方契约：JSON body，
// 除应用凭证四件套外还需 nonce（随机串）与 timestamp（yyyy-MM-dd
// HH:mm:ss）参与校验。
type kingdeeTokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Username     string `json:"username"`
	AccountID    string `json:"accountId"`
	Nonce        string `json:"nonce"`
	Timestamp    string `json:"timestamp"`
	Language     string `json:"language"`
}

// getKingdeeAccessToken 调苍穹 OAuth2 令牌端点换 access_token，按凭证组合
// 缓存到过期。端点与报文以苍穹「获取 Token 示例」为准（/kapi/oauth2/
// getToken，JSON body），另保留 /ierp 前缀形态兼容标准网关。
func (s *userService) getKingdeeAccessToken(ctx context.Context, cfg *types.KingdeeTenantSSO) (string, error) {
	cacheKey := "kingdee:sso:" + cfg.BaseURL + "|" + cfg.AppClientID + "|" + cfg.ProxyUsername + "|" + cfg.AccountID + "|" + cfg.AppSecret
	if token, ok := ssoCachedToken(cacheKey); ok {
		return token, nil
	}

	body, err := json.Marshal(kingdeeTokenRequest{
		ClientID:     cfg.AppClientID,
		ClientSecret: cfg.AppSecret,
		Username:     cfg.ProxyUsername,
		AccountID:    cfg.AccountID,
		Nonce:        uuid.NewString(),
		// 苍穹按其服务端时钟校验 timestamp 偏差，按东八区生成
		Timestamp: time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"),
		Language:  "zh_CN",
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode Kingdee token request: %w", err)
	}

	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	paths := []string{"/kapi/oauth2/getToken", "/ierp/kapi/oauth2/getToken"}
	var data kingdeeTokenResponse
	for i, path := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("failed to build Kingdee token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		status, err := ssoDoJSONStatus(ctx, req, &data)
		if err == nil {
			break
		}
		if status != http.StatusNotFound || i == len(paths)-1 {
			return "", fmt.Errorf("Kingdee token request failed: %w", err)
		}
	}

	token := data.Data.AccessToken
	if token == "" {
		token = data.AccessToken
	}
	if token == "" {
		msg := strings.TrimSpace(data.Message)
		if msg == "" {
			msg = strings.TrimSpace(data.DescriptionCn)
		}
		if msg == "" {
			msg = strings.TrimSpace(data.Description)
		}
		if msg == "" {
			msg = data.ErrorCode
		}
		return "", fmt.Errorf("Kingdee token response missing access_token: %s", msg)
	}

	expiresIn := data.Data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = data.ExpiresIn
	}
	// 苍穹返回的 expires_in 可能是毫秒（如 7199992 ≈ 2h）
	if expiresIn > 100000 {
		expiresIn /= 1000
	}
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	ssoStoreToken(cacheKey, token, time.Duration(expiresIn)*time.Second)
	return token, nil
}

// getKingdeeIdentity 用授权码调苍穹 getUserInfo 换当前登录用户身份。
// externalID 取 userName（苍穹账号唯一键），displayName 取 name。
// 苍穹 kapi 有两种网关挂载形态：标准形态是 {base}/ierp/kapi/...（集成指南
// 的默认路径），部分私有化部署把 ierp 应用直接挂在上下文根，接口实际在
// {base}/kapi/...。指南路径 404 时自动退到无 /ierp 前缀的路径重试。
func (s *userService) getKingdeeIdentity(ctx context.Context, cfg *types.KingdeeTenantSSO, code string) (externalID, displayName string, err error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")

	var accessToken string
	if cfg.TokenMode() {
		if accessToken, err = s.getKingdeeAccessToken(ctx, cfg); err != nil {
			return "", "", err
		}
	}

	paths := []string{
		"/ierp/kapi/v2/secm/authen/getUserInfo",
		"/kapi/v2/secm/authen/getUserInfo",
	}
	var data kingdeeUserInfoResponse
	for i, path := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?code="+url.QueryEscape(code), nil)
		if err != nil {
			return "", "", fmt.Errorf("failed to build Kingdee userinfo request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if accessToken != "" {
			// 实测苍穹网关识别字面量名为 access_token 的请求头（Authorization
			// Bearer 与 query 参数均不被接受，统一 401 未经授权）。
			req.Header.Set("access_token", accessToken)
		}
		status, err := ssoDoJSONStatus(ctx, req, &data)
		if err == nil {
			break
		}
		if status != http.StatusNotFound || i == len(paths)-1 {
			return "", "", err
		}
	}
	if !data.Status || data.ErrorCode != "0" {
		msg := strings.TrimSpace(data.Message)
		if msg == "" {
			msg = data.ErrorCode
		}
		// 未配置 token 模式时的 401 多半是苍穹要求应用鉴权，给出可操作提示
		if !cfg.TokenMode() && (data.ErrorCode == "401" || strings.Contains(msg, "未经授权")) {
			msg += "（独立部署需在苍穹 SSO 设置中填写 app_secret/username/account_id）"
		}
		return "", "", fmt.Errorf("Kingdee getUserInfo failed: %s", msg)
	}
	if strings.TrimSpace(data.Data.UserName) == "" {
		return "", "", errors.NewUnauthorizedError("Kingdee did not return userName")
	}
	return data.Data.UserName, data.Data.Name, nil
}

// --- 登录与 JIT 建号 ---

// LoginWithWeComCode 用企微网页授权 code 完成登录（首次自动建号）。
// 凭证与目标租户按 tenantParam/host 解析（见 resolveSSOTenant）；
// 登录后直接进入该租户，非成员自动以 contributor 加入。
func (s *userService) LoginWithWeComCode(
	ctx context.Context,
	code, host string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, tenant, err := s.ssoTenantWeComConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.NewValidationError("code is required")
	}
	userID, displayName, err := s.getWeComUserIdentity(ctx, cfg, code)
	if err != nil {
		return nil, err
	}
	return s.completeSSOLogin(ctx, "wecom", userID, displayName, provisioning, tenant)
}

// LoginWithFeishuCode 用飞书网页授权 code 完成登录（首次自动建号）。
func (s *userService) LoginWithFeishuCode(
	ctx context.Context,
	code, host string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, tenant, err := s.ssoTenantFeishuConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.NewValidationError("code is required")
	}
	openID, displayName, err := s.getFeishuIdentity(ctx, cfg, code)
	if err != nil {
		return nil, err
	}
	return s.completeSSOLogin(ctx, "feishu", openID, displayName, provisioning, tenant)
}

// LoginWithKingdeeCode 用金蝶苍穹授权码完成登录（首次自动建号）。
// 用户从苍穹统一门户点击免登菜单进入，苍穹回跳本系统回调地址并带
// code；此处用 code 换苍穹用户身份（userName 为唯一键），后续与
// 企微/飞书共用 completeSSOLogin。
func (s *userService) LoginWithKingdeeCode(
	ctx context.Context,
	code, host string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	tenant, err := s.resolveSSOTenant(ctx, host)
	if err != nil {
		return nil, err
	}
	if tenant == nil || !tenant.SSOConfig.KingdeeEnabled() {
		return nil, errors.NewForbiddenError("Kingdee SSO is not configured for this workspace")
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.NewValidationError("code is required")
	}
	userName, displayName, err := s.getKingdeeIdentity(ctx, tenant.SSOConfig.Kingdee, code)
	if err != nil {
		return nil, err
	}
	return s.completeSSOLogin(ctx, "kingdee", userName, displayName, provisioning, tenant)
}

// completeSSOLogin 按平台身份查找本地用户，缺失则自动创建（JIT），
// 然后签发与本地登录一致的 token/租户/成员关系。
// targetTenant 非空时（租户专属 SSO 入口）：登录态直接落在该租户，
// 非成员自动以 contributor 加入；新用户不建个人空间。
func (s *userService) completeSSOLogin(
	ctx context.Context,
	platform, externalID, displayName string,
	provisioning types.TenantProvisioningMode,
	targetTenant *types.Tenant,
) (*types.OIDCCallbackResponse, error) {
	email := ssoSyntheticEmail(platform, externalID)
	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil && !isUserLookupNotFound(err) {
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}
	isNewUser := false
	if isUserLookupNotFound(err) || user == nil {
		effProvisioning := provisioning
		if targetTenant != nil {
			// 目标租户已知：不建个人空间，建号后直接加入目标租户。
			effProvisioning = types.TenantProvisioningTenantless
		}
		user, err = s.provisionSSOUser(ctx, platform, externalID, displayName, email, effProvisioning)
		if err != nil {
			return nil, err
		}
		isNewUser = true
		logger.Infof(ctx, "[SSO] auto-provisioned %s user %s (%s)", platform, user.ID, email)
	}

	if !user.IsActive {
		return &types.OIDCCallbackResponse{Success: false, Message: "Account is disabled"}, nil
	}

	resolvedTenantID := uint64(0)
	if targetTenant != nil {
		resolvedTenantID = targetTenant.ID
		if err := s.ensureTenantMembership(ctx, user.ID, targetTenant.ID); err != nil {
			return nil, err
		}
	} else {
		resolvedTenantID = s.resolveLoginTenantID(ctx, user)
	}
	accessToken, refreshToken, err := s.generateTokensForTenant(ctx, user, resolvedTenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate local tokens: %w", err)
	}

	var tenant *types.Tenant
	if resolvedTenantID > 0 {
		if t, terr := s.tenantService.GetTenantByID(ctx, resolvedTenantID); terr == nil {
			tenant = t
		} else {
			logger.Warnf(ctx, "[SSO] login: failed to load tenant %d for user %s: %v",
				resolvedTenantID, user.ID, terr)
		}
	}
	memberships := s.buildMembershipsForUser(ctx, user, tenant)

	return &types.OIDCCallbackResponse{
		Success:      true,
		Message:      "登录成功",
		User:         user,
		Tenant:       tenant,
		Memberships:  memberships,
		Token:        accessToken,
		RefreshToken: refreshToken,
		IsNewUser:    isNewUser,
	}, nil
}

// ensureTenantMembership 保证用户是租户的活跃成员，缺失时以 viewer 加入
// （租户 SSO 免登的同事只需聊天问答，读+会话均为 viewer 可用；已是成员则不动，
// 需要更高角色由租户管理员在成员管理里单独提权）。
func (s *userService) ensureTenantMembership(ctx context.Context, userID string, tenantID uint64) error {
	if s.memberService == nil {
		return fmt.Errorf("member service unavailable")
	}
	_, err := s.memberService.AddMember(ctx, userID, tenantID, types.TenantRoleViewer, nil)
	if err == nil || err == ErrMembershipAlreadyExists {
		return nil
	}
	return fmt.Errorf("failed to join workspace via SSO: %w", err)
}

// provisionSSOUser 为首次登录的平台用户创建本地访客账号：
// 随机密码（仅 SSO 入口可登录）+ 按默认租户策略建个人空间。
func (s *userService) provisionSSOUser(
	ctx context.Context,
	platform, externalID, displayName, email string,
	provisioning types.TenantProvisioningMode,
) (*types.User, error) {
	username := s.generateSSOUsername(ctx, platform, externalID, displayName)
	randomPassword, err := generateRandomString(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate password for SSO user: %w", err)
	}
	user, err := s.Register(ctx, &types.RegisterRequest{
		Username:           username,
		Email:              email,
		Password:           randomPassword,
		TenantProvisioning: provisioning,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to auto-provision %s SSO user: %w", platform, err)
	}
	return user, nil
}

func (s *userService) generateSSOUsername(ctx context.Context, platform, externalID, displayName string) string {
	base := sanitizeUsernameCandidate(displayName)
	if base == "" {
		base = sanitizeUsernameCandidate(platform + "_" + externalID)
	}
	if base == "" {
		base = platform + "-user"
	}
	candidate := base
	for i := 0; i < 20; i++ {
		existing, err := s.userRepo.GetUserByUsername(ctx, candidate)
		if isUserLookupNotFound(err) || (err == nil && existing == nil) {
			return candidate
		}
		if err != nil && !isUserLookupNotFound(err) {
			logger.Warnf(ctx, "[SSO] username lookup failed for %s: %v", candidate, err)
			break
		}
		candidate = fmt.Sprintf("%s_%d", base, i+2)
	}
	return fmt.Sprintf("%s_%d", base, time.Now().Unix())
}

// ssoSyntheticEmail 由平台身份合成稳定邮箱，作为本地账号锚点。
func ssoSyntheticEmail(platform, externalID string) string {
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, externalID)
	if sanitized == "" {
		sanitized = "unknown"
	}
	return fmt.Sprintf("%s_%s@%s.sso.weknora.local", platform, sanitized, platform)
}

func ssoDoJSON(ctx context.Context, req *http.Request, out interface{}) error {
	_, err := ssoDoJSONStatus(ctx, req, out)
	return err
}

// ssoDoJSONStatus 同 ssoDoJSON，但把 HTTP 状态码一并返回，供调用方按状态码
// 决定是否换候选路径重试（如苍穹 kapi 的两种挂载形态）。
func ssoDoJSONStatus(ctx context.Context, req *http.Request, out interface{}) (int, error) {
	resp, err := ssoHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("SSO HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("SSO HTTP request returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("failed to decode SSO response: %w", err)
	}
	return resp.StatusCode, nil
}
