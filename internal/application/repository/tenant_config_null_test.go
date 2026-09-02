package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestTenantConfigSettersPersistNull pins the reason SetBrandingConfig /
// SetDefaultMemberAgentIDs exist: gorm's Updates(struct) skips nil zero
// values, so a "clear" through UpdateTenant would silently keep the old
// column value. The explicit map-based setters must write SQL NULL.
func TestTenantConfigSettersPersistNull(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-config-null?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tenant := &types.Tenant{ID: 42, Name: "t"}
	if err := db.Create(tenant).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	r := &tenantRepository{db: db}
	ctx := context.Background()

	// Set a branding config and a default allowlist.
	branding := &types.BrandingConfig{LoginTitle: "自定义"}
	if err := r.SetBrandingConfig(ctx, 42, branding); err != nil {
		t.Fatalf("SetBrandingConfig: %v", err)
	}
	if err := r.SetDefaultMemberAgentIDs(ctx, 42, types.AgentIDList{"a", "b"}); err != nil {
		t.Fatalf("SetDefaultMemberAgentIDs: %v", err)
	}

	var got types.Tenant
	if err := db.First(&got, 42).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.BrandingConfig == nil || got.BrandingConfig.LoginTitle != "自定义" {
		t.Fatalf("branding not persisted: %+v", got.BrandingConfig)
	}
	if len(got.DefaultMemberAgentIDs) != 2 || got.DefaultMemberAgentIDs[0] != "a" {
		t.Fatalf("default agents not persisted: %v", got.DefaultMemberAgentIDs)
	}

	// Clear both — the exact path that Updates(struct) would drop.
	if err := r.SetBrandingConfig(ctx, 42, nil); err != nil {
		t.Fatalf("clear branding: %v", err)
	}
	if err := r.SetDefaultMemberAgentIDs(ctx, 42, nil); err != nil {
		t.Fatalf("clear defaults: %v", err)
	}

	var after types.Tenant
	if err := db.First(&after, 42).Error; err != nil {
		t.Fatalf("reload after clear: %v", err)
	}
	if after.BrandingConfig != nil {
		t.Fatalf("branding clear must persist NULL, still %+v", after.BrandingConfig)
	}
	if after.DefaultMemberAgentIDs != nil {
		t.Fatalf("defaults clear must persist NULL, still %v", after.DefaultMemberAgentIDs)
	}
}
