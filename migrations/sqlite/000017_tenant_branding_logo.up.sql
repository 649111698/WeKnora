-- Mirrors versioned migration 000095_tenant_branding_logo:
-- uploaded workspace logos (served by /api/v1/branding/logo/:tenant_id).

CREATE TABLE IF NOT EXISTS tenant_branding_assets (
    tenant_id    TEXT      PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    content_type TEXT      NOT NULL,
    data         BLOB      NOT NULL,
    updated_at   DATETIME  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
