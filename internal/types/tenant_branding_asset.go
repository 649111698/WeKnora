package types

import "time"

// TenantBrandingAsset stores an uploaded workspace logo as raw bytes in a
// dedicated table (not on tenants itself): every tenant read would otherwise
// drag the image blob along. Served publicly by
// GET /api/v1/branding/logo/:tenant_id — the login page renders the logo
// before authentication, so the route is intentionally unauthenticated.
type TenantBrandingAsset struct {
	TenantID    uint64    `gorm:"column:tenant_id;primaryKey" json:"tenant_id"`
	ContentType string    `gorm:"column:content_type;size:64" json:"content_type"`
	Data        []byte    `gorm:"column:data;type:bytea" json:"-"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName overrides the gorm table name.
func (TenantBrandingAsset) TableName() string { return "tenant_branding_assets" }
