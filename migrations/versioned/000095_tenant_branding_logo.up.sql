-- Migration 000095: uploaded workspace logos for white-label branding.
--
-- tenants.branding_config.logo_url may point at an uploaded image served by
-- GET /api/v1/branding/logo/:tenant_id. Bytes live in a side table so tenant
-- reads never drag the blob along. See internal/types/tenant_branding_asset.go.

CREATE TABLE IF NOT EXISTS tenant_branding_assets (
    tenant_id    VARCHAR(36)  PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    content_type VARCHAR(64)  NOT NULL,
    data         BYTEA        NOT NULL,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
