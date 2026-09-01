-- Mirrors versioned migration 000094_tenant_branding:
-- tenant white-label branding (NULL = stock texts/logo).

ALTER TABLE tenants ADD COLUMN branding_config TEXT;
