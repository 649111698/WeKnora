-- Migration 000094: tenant-level white-label branding config.
--
-- tenants.branding_config stores the workspace's custom welcome title,
-- login page copy, sidebar title and logo URL. NULL keeps the stock
-- WeKnora texts and logo. See internal/types/tenant_sso.go (BrandingConfig).

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS branding_config JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.branding_config IS 'Tenant white-label branding: {welcome_title, login_title, login_subtitle, logo_url, sidebar_title}';
