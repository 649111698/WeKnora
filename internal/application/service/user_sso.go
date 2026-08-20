package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
// 凭证为租户级（tenants.sso_config），租户解析顺序：?t=<tenant id> >
// 请求 Host 匹配租户 login_domain > 全实例仅一租户配置了该平台时回退到它。
func (s *userService) GetSSOStatus(ctx context.Context, tenantParam, host string) (*types.SSOStatusResponse, error) {
	resp := &types.SSOStatusResponse{}
	tenant, err := s.resolveSSOTenant(ctx, tenantParam, host, "")
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
	return resp, nil
}

// GetSSOWatermark 解析未登录页面（登录页）的水印配置，租户解析同 GetSSOStatus。
func (s *userService) GetSSOWatermark(ctx context.Context, tenantParam, host string) types.WatermarkConfig {
	tenant, err := s.resolveSSOTenant(ctx, tenantParam, host, "")
	if err != nil || tenant == nil {
		return (&types.WatermarkConfig{}).Resolved()
	}
	return tenant.WatermarkConfig.Resolved()
}

// GetSSODomainVerifyText 返回目标租户配置的企微可信域名验证文字。
func (s *userService) GetSSODomainVerifyText(ctx context.Context, tenantParam, host string) string {
	tenant, err := s.resolveSSOTenant(ctx, tenantParam, host, "wecom")
	if err != nil || tenant == nil || tenant.SSOConfig == nil || tenant.SSOConfig.WeCom == nil {
		return ""
	}
	return strings.TrimSpace(tenant.SSOConfig.WeCom.DomainVerifyText)
}

// resolveSSOTenant 定位 SSO 登录的目标租户。
//   - tenantParam: 登录链接携带的 ?t=<tenant id>，优先级最高；
//   - host: 请求 Host（可能带端口），与各租户 login_domain 精确匹配（忽略大小写）；
//   - 两者都未命中时：若全实例恰好只有一个租户配置了 platform（platform 为
//     空则任一平台）的 SSO，回退到它——单企业部署不需要改登录链接。
//
// 找不到返回 (nil, nil)（调用方按"未启用"处理），配置歧义返回错误。
func (s *userService) resolveSSOTenant(ctx context.Context, tenantParam, host, platform string) (*types.Tenant, error) {
	if tid := strings.TrimSpace(tenantParam); tid != "" {
		id, err := strconv.ParseUint(tid, 10, 64)
		if err != nil {
			return nil, errors.NewForbiddenError("invalid tenant parameter")
		}
		tenant, err := s.tenantService.GetTenantByID(ctx, id)
		if err != nil || tenant == nil {
			return nil, nil
		}
		return tenant, nil
	}
	tenants, err := s.tenantService.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	if h := strings.ToLower(strings.TrimSpace(host)); h != "" {
		for _, t := range tenants {
			if t.SSOConfig != nil && strings.EqualFold(strings.TrimSpace(t.SSOConfig.LoginDomain), h) {
				return t, nil
			}
		}
	}
	var matches []*types.Tenant
	for _, t := range tenants {
		cfg := t.SSOConfig
		if cfg == nil {
			continue
		}
		ok := false
		switch platform {
		case "wecom":
			ok = cfg.WeComEnabled()
		case "feishu":
			ok = cfg.FeishuEnabled()
		default:
			ok = cfg.WeComEnabled() || cfg.FeishuEnabled()
		}
		if ok {
			matches = append(matches, t)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, nil
}

// ssoTenantWeComConfig 取目标租户的企微凭证（要求已配置齐全）。
func (s *userService) ssoTenantWeComConfig(ctx context.Context, tenantParam, host string) (*config.WeComSSOConfig, *types.Tenant, error) {
	tenant, err := s.resolveSSOTenant(ctx, tenantParam, host, "wecom")
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
func (s *userService) ssoTenantFeishuConfig(ctx context.Context, tenantParam, host string) (*config.FeishuSSOConfig, *types.Tenant, error) {
	tenant, err := s.resolveSSOTenant(ctx, tenantParam, host, "feishu")
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

// --- 登录与 JIT 建号 ---

// LoginWithWeComCode 用企微网页授权 code 完成登录（首次自动建号）。
// 凭证与目标租户按 tenantParam/host 解析（见 resolveSSOTenant）；
// 登录后直接进入该租户，非成员自动以 contributor 加入。
func (s *userService) LoginWithWeComCode(
	ctx context.Context,
	code, tenantParam, host string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, tenant, err := s.ssoTenantWeComConfig(ctx, tenantParam, host)
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
	code, tenantParam, host string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	cfg, tenant, err := s.ssoTenantFeishuConfig(ctx, tenantParam, host)
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

// ensureTenantMembership 保证用户是租户的活跃成员，缺失时以 contributor 加入
// （租户 SSO 入口登录即视为受邀使用；已是成员则不动）。
func (s *userService) ensureTenantMembership(ctx context.Context, userID string, tenantID uint64) error {
	if s.memberService == nil {
		return fmt.Errorf("member service unavailable")
	}
	_, err := s.memberService.AddMember(ctx, userID, tenantID, types.TenantRoleContributor, nil)
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
