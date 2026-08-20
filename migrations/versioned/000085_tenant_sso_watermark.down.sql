-- Migration 000085 (down): restore global SSO/watermark system_settings keys
-- and drop the tenant-level columns. Values are NOT recoverable — tenants
-- must re-enter credentials in system settings after downgrade.

ALTER TABLE tenants DROP COLUMN IF EXISTS sso_config;
ALTER TABLE tenants DROP COLUMN IF EXISTS watermark_config;
