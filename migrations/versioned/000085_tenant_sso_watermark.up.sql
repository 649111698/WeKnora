-- Migration 000085: tenant-level SSO and watermark configuration.
--
-- WeCom/Feishu SSO credentials and the site watermark move from global
-- system_settings to per-tenant jsonb columns on tenants, so each tenant
-- (typically one company) configures its own self-built app credentials,
-- dedicated login domain, and watermark. The legacy global keys are
-- deleted here; deployments upgrading with those keys configured must
-- re-enter them in the tenant settings UI.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS sso_config JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.sso_config IS 'Tenant-level SSO config: WeCom/Feishu credentials + dedicated login domain (see internal/types/tenant_sso.go)';

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS watermark_config JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.watermark_config IS 'Tenant-wide page watermark config: {enabled, text}';

DELETE FROM system_settings WHERE key IN (
    'sso.wecom.corp_id',
    'sso.wecom.corp_secret',
    'sso.wecom.agent_id',
    'sso.wecom.domain_verify_text',
    'sso.feishu.app_id',
    'sso.feishu.app_secret',
    'auth.watermark_enabled',
    'auth.watermark_text'
);
