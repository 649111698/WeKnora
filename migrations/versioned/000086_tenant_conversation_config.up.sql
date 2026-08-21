-- Migration 000086: tenant-level conversation UX config.
--
-- Hiding the chat input-box model dropdown and pinning the chat model is a
-- per-workspace choice (white-label deployments usually lock members to one
-- cost-controlled model), so it lives on tenants as a jsonb column next to
-- watermark_config / sso_config. See internal/types/tenant_sso.go.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS conversation_config JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.conversation_config IS 'Tenant chat UX config: {model_selector_hidden, default_model_id}';
