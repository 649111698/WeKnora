package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"

	"github.com/gin-gonic/gin"
)

/*
 * 企微/飞书内置浏览器 SSO 免登接口。
 *
 * /auth/sso/config 公开返回启用状态与构建授权 URL 的公开参数；
 * /auth/sso/{wecom|feishu}/callback 接收平台回跳的 code，完成登录后
 * 302 回前端 /#oidc_result=<payload>，复用 OIDC 回调的前端处理链路。
 */

// GetSSOConfig godoc
// @Summary      获取企微/飞书 SSO 免登配置
// @Description  返回各平台启用状态与构建 OAuth 授权地址所需的公开参数（不含密钥）
// @Tags         认证
// @Produce      json
// @Success      200  {object}  types.SSOStatusResponse
// @Router       /auth/sso/config [get]
func (h *AuthHandler) GetSSOConfig(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := h.userService.GetSSOStatus(ctx)
	if err != nil {
		logger.Errorf(ctx, "[SSO] failed to load status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load SSO status"})
		return
	}
	c.JSON(http.StatusOK, resp)
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
		return h.userService.LoginWithWeComCode(c.Request.Context(), code, h.resolveDefaultTenantMode(c.Request.Context()))
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
		return h.userService.LoginWithFeishuCode(c.Request.Context(), code, h.resolveDefaultTenantMode(c.Request.Context()))
	})
}
