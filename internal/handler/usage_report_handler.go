package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// 每日使用报告（每天 09:00 统计前一天并推送到企微应用窗口）。
// 配置存 tenants.usage_report_config；路由挂在 /tenants/usage-report*
// （Admin+，认证上下文取空间，路径不带 :id）。

// GetUsageReportConfig godoc
// @Summary      获取每日使用报告配置
// @Tags         空间管理
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/usage-report-config [get]
func (h *TenantHandler) GetUsageReportConfig(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	cfg, err := h.usageReportSvc.GetUsageReportConfig(ctx, tenant.ID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"tenant_id": tenant.ID})
		c.Error(errors.NewInternalServerError("Failed to load usage report config"))
		return
	}
	if cfg == nil {
		cfg = &types.UsageReportConfig{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": cfg})
}

// UpdateUsageReportConfig godoc
// @Summary      更新每日使用报告配置
// @Tags         空间管理
// @Accept       json
// @Produce      json
// @Param        request  body  types.UsageReportConfig  true  "配置"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/usage-report-config [put]
func (h *TenantHandler) UpdateUsageReportConfig(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	var cfg types.UsageReportConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if len(cfg.NotifyUserIDs) > 50 {
		c.Error(errors.NewBadRequestError("notify_user_ids must contain at most 50 users"))
		return
	}
	// 保留调度器维护的 last_run_date：配置面只改用户可见字段。
	existing, err := h.usageReportSvc.GetUsageReportConfig(ctx, tenant.ID)
	if err == nil && existing != nil {
		cfg.LastRunDate = existing.LastRunDate
	}
	if err := h.usageReportSvc.SetUsageReportConfig(ctx, tenant.ID, &cfg); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"tenant_id": tenant.ID})
		c.Error(errors.NewInternalServerError("Failed to save usage report config"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": cfg})
}

// SendTestUsageReport godoc
// @Summary      立即生成并推送昨天的使用报告（测试）
// @Tags         空间管理
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/usage-report-config/test [post]
func (h *TenantHandler) SendTestUsageReport(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	if cfg := tenant.UsageReportConfig; cfg == nil || !cfg.Enabled {
		c.Error(errors.NewBadRequestError("usage report is disabled"))
		return
	}
	// 重新读完整租户（上下文里的对象可能不含 SSO/报告字段）。
	full, err := h.service.GetTenantByID(ctx, tenant.ID)
	if err != nil || full == nil {
		c.Error(errors.NewInternalServerError("Failed to load workspace"))
		return
	}
	report, err := h.usageReportSvc.SendTestUsageReport(ctx, full)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"tenant_id": tenant.ID})
		c.Error(errors.NewInternalServerError("Failed to send usage report: "+err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"date":           report.Date,
			"total_users":    report.TotalUsers,
			"qualified":      report.Qualified,
			"unqualified":    report.Unqualified,
			"total_messages": report.TotalMessages,
			"rows":           report.Rows,
			"markdown":       h.usageReportSvc.RenderUsageReportMarkdown(report, time.Now()),
		},
	})
}
