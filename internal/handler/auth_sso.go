package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"

	"github.com/gin-gonic/gin"
)

/*
 * 企微/飞书内置浏览器 SSO 免登接口（凭证为租户级）。
 *
 * 登录入口通过租户专属域名（请求 Host 与 login_domain 匹配）定位租户：
 * /auth/sso/config 返回该租户的启用状态与公开参数；
 * /auth/sso/{wecom|feishu}/callback 接收平台回跳的 code，完成登录后
 * 302 回前端 /#oidc_result=<payload>，复用 OIDC 回调的前端处理链路；
 * /auth/sso/wecom/domain-verify 供 nginx 代理 /WW_verify_*.txt，
 * 返回该租户配置的域名验证文字。
 */

// ssoHost 返回请求 Host：租户按其专属登录域名（login_domain）与 Host
// 精确匹配区分，是唯一的租户定位方式。
func ssoHost(c *gin.Context) string {
	return c.Request.Host
}

// GetSSOConfig godoc
// @Summary      获取企微/飞书 SSO 免登配置
// @Description  按请求 Host 匹配租户专属登录域名，返回该租户各平台启用状态与构建 OAuth 授权地址所需的公开参数（不含密钥）
// @Tags         认证
// @Produce      json
// @Success      200  {object}  types.SSOStatusResponse
// @Router       /auth/sso/config [get]
func (h *AuthHandler) GetSSOConfig(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.userService.GetSSOStatus(ctx, ssoHost(c))
	if err != nil {
		logger.Errorf(ctx, "[SSO] failed to load status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load SSO status"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SSOWeComDomainVerify godoc
// @Summary      企微可信域名归属验证
// @Description  按请求 Host 匹配租户专属登录域名，返回该租户的企微域名验证文字（WW_verify_*.txt 内容）。nginx 将根路径的 /WW_verify_*.txt 代理到本接口。
// @Tags         认证
// @Produce      text/plain
// @Success      200  {string}  验证文字
// @Failure      404  该租户未配置验证文字
// @Router       /auth/sso/wecom/domain-verify [get]
func (h *AuthHandler) SSOWeComDomainVerify(c *gin.Context) {
	if h.userService == nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	text := h.userService.GetSSODomainVerifyText(c.Request.Context(), ssoHost(c))
	if text == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}
	c.String(http.StatusOK, text)
}

func (h *AuthHandler) ssoRedirectCallback(c *gin.Context, platform string, login func() (*types.OIDCCallbackResponse, error)) {
	ctx := c.Request.Context()
	frontendRedirectURI := "/"

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("missing_code"))
		return
	}

	resp, err := login()
	if err != nil {
		logger.Errorf(ctx, "[SSO] %s login failed: %v", platform, err)
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("login_failed")+"&oidc_error_description="+urlQueryEscape(err.Error()))
		return
	}
	if !resp.Success {
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("login_failed")+"&oidc_error_description="+urlQueryEscape(resp.Message))
		return
	}

	payload, err := encodeOIDCCallbackPayload(resp)
	if err != nil {
		logger.Errorf(ctx, "[SSO] failed to encode callback payload: %v", err)
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("payload_encode_failed"))
		return
	}
	c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_result="+urlQueryEscape(payload))
}

// SSOWeComCallback godoc
// @Summary      企微 SSO 免登回调
// @Description  用企微网页授权 code 完成登录（首次自动创建访客账号），成功后 302 回前端
// @Tags         认证
// @Param        code  query  string  true  "企微授权码"
// @Success      302
// @Router       /auth/sso/wecom/callback [get]
func (h *AuthHandler) SSOWeComCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	h.ssoRedirectCallback(c, "wecom", func() (*types.OIDCCallbackResponse, error) {
		return h.userService.LoginWithWeComCode(c.Request.Context(), code, ssoHost(c), h.resolveDefaultTenantMode(c.Request.Context()))
	})
}

// SSOFeishuCallback godoc
// @Summary      飞书 SSO 免登回调
// @Description  用飞书网页授权 code 完成登录（首次自动创建访客账号），成功后 302 回前端
// @Tags         认证
// @Param        code  query  string  true  "飞书授权码"
// @Success      302
// @Router       /auth/sso/feishu/callback [get]
func (h *AuthHandler) SSOFeishuCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	h.ssoRedirectCallback(c, "feishu", func() (*types.OIDCCallbackResponse, error) {
		return h.userService.LoginWithFeishuCode(c.Request.Context(), code, ssoHost(c), h.resolveDefaultTenantMode(c.Request.Context()))
	})
}

// SSOKingdeeCallback godoc
// @Summary      金蝶苍穹 SSO 免登回调
// @Description  用苍穹统一门户授权码完成登录（首次自动创建访客账号），成功后 302 回前端。苍穹侧「第三方应用」的 SSO 可信白名单需登记本回调地址（含 app_client_id 与 response_code 查询参数）
// @Tags         认证
// @Param        code  query  string  true  "苍穹授权码"
// @Success      302
// @Router       /auth/sso/kingdee/callback [get]
func (h *AuthHandler) SSOKingdeeCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	h.ssoRedirectCallback(c, "kingdee", func() (*types.OIDCCallbackResponse, error) {
		return h.userService.LoginWithKingdeeCode(c.Request.Context(), code, ssoHost(c), h.resolveDefaultTenantMode(c.Request.Context()))
	})
}
