-- Mirrors versioned migration 000094_tenant_branding (down).

ALTER TABLE tenants DROP COLUMN branding_config;
