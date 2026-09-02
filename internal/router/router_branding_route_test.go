package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRegisterTenantRoutesNoRouteConflict smoke-registers the tenant route
// table: gin panics at registration time on path conflicts, so a passing
// test proves the new public /branding/logo/:tenant_id and per-tenant
// /branding/logo upload paths coexist with the existing trees.
func TestRegisterTenantRoutesNoRouteConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("route registration panicked: %v", rec)
		}
	}()
	RegisterTenantRoutes(v1, nil, nil, nil, nil, &rbacGuards{})
}
