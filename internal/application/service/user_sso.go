package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

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
// 凭证来源：DB system_settings > ENV > 无（齐备才视为启用）。
func (s *userService) GetSSOStatus(ctx context.Context) (*types.SSOStatusResponse, error) {
	resp := &types.SSOStatusResponse{}
	corpID := s.ssoSetting(ctx, "sso.wecom.corp_id", "WECOM_SSO_CORP_ID")
	secret := s.ssoSetting(ctx, "sso.wecom.corp_secret", "WECOM_SSO_CORP_SECRET")
	if corpID != "" && secret != "" {
		resp.WeCom = &types.SSOProviderStatus{
			Enabled: true,
			CorpID:  corpID,
			AgentID: s.ssoSetting(ctx, "sso.wecom.agent_id", "WECOM_SSO_AGENT_ID"),
		}
	}
	appID := s.ssoSetting(ctx, "sso.feishu.app_id", "FEISHU_SSO_APP_ID")
	appSecret := s.ssoSetting(ctx, "sso.feishu.app_secret", "FEISHU_SSO_APP_SECRET")
	if appID != "" && appSecret != "" {
		resp.Feishu = &types.SSOProviderStatus{Enabled: true, AppID: appID}
	}
	return resp, nil
}

// ssoSetting 三级解析一个 SSO 字符串配置：DB system_settings > ENV > 空。
func (s *userService) ssoSetting(ctx context.Context, key, envName string) string {
	if s.systemSettingSvc != nil {
		return strings.TrimSpace(s.systemSettingSvc.GetString(ctx, key, envName, ""))
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func (s *userService) ssoWeComConfig(ctx context.Context) (*config.WeComSSOConfig, error) {
	corpID := s.ssoSetting(ctx, "sso.wecom.corp_id", "WECOM_SSO_CORP_ID")
	secret := s.ssoSetting(ctx, "sso.wecom.corp_secret", "WECOM_SSO_CORP_SECRET")
	if corpID == "" || secret == "" {
		return nil, errors.NewForbiddenError("WeCom SSO is not configured")
	}
	return &config.WeComSSOConfig{
		CorpID:  corpID,
		Secret:  secret,
		AgentID: s.ssoSetting(ctx, "sso.wecom.agent_id", "WECOM_SSO_AGENT_ID"),
	}, nil
}

func (s *userService) ssoFeishuConfig(ctx context.Context) (*config.FeishuSSOConfig, error) {
	appID := s.ssoSetting(ctx, "sso.feishu.app_id", "FEISHU_SSO_APP_ID")
	appSecret := s.ssoSetting(ctx, "sso.feishu.app_secret", "FEISHU_SSO_APP_SECRET")
	if appID == "" || appSecret == "" {
		return nil, errors.NewForbiddenError("Feishu SSO is not configured")
	}
	return &config.FeishuSSOConfig{AppID: appID, AppSecret: appSecret}, nil
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

// --- 登录与 JIT 建号 ---

// LoginWithWeComCode 用企微网页授权 code 完成登录（首次自动建号）。
func (s *userService) LoginWithWeComCode(
	ctx context.Context,
	code string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, err := s.ssoWeComConfig(ctx)
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
	return s.completeSSOLogin(ctx, "wecom", userID, displayName, provisioning)
}

// LoginWithFeishuCode 用飞书网页授权 code 完成登录（首次自动建号）。
func (s *userService) LoginWithFeishuCode(
	ctx context.Context,
	code string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, err := s.ssoFeishuConfig(ctx)
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
	return s.completeSSOLogin(ctx, "feishu", openID, displayName, provisioning)
}

// completeSSOLogin 按平台身份查找本地用户，缺失则自动创建（JIT），
// 然后签发与本地登录一致的 token/租户/成员关系。
func (s *userService) completeSSOLogin(
	ctx context.Context,
	platform, externalID, displayName string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	email := ssoSyntheticEmail(platform, externalID)
	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil && !isUserLookupNotFound(err) {
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}
	isNewUser := false
	if isUserLookupNotFound(err) || user == nil {
		user, err = s.provisionSSOUser(ctx, platform, externalID, displayName, email, provisioning)
		if err != nil {
			return nil, err
		}
		isNewUser = true
		logger.Infof(ctx, "[SSO] auto-provisioned %s user %s (%s)", platform, user.ID, email)
	}

	if !user.IsActive {
		return &types.OIDCCallbackResponse{Success: false, Message: "Account is disabled"}, nil
	}

	resolvedTenantID := s.resolveLoginTenantID(ctx, user)
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
	resp, err := ssoHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSO HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSO HTTP request returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode SSO response: %w", err)
	}
	return nil
}
