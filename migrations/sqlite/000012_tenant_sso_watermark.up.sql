-- Mirrors versioned migration 000085_tenant_sso_watermark:
-- tenant-level SSO credentials + watermark replace global system_settings.

ALTER TABLE tenants ADD COLUMN sso_config TEXT;
ALTER TABLE tenants ADD COLUMN watermark_config TEXT;

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
