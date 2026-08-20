-- Down for 000012: values are NOT recoverable; re-enter in system settings.

ALTER TABLE tenants DROP COLUMN sso_config;
ALTER TABLE tenants DROP COLUMN watermark_config;
