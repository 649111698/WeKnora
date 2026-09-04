-- Migration 000097: tenant-level daily usage report config.
--
-- tenants.usage_report_config stores the 9am WeCom usage-report push
-- settings: {enabled, push_to_wecom, notify_user_ids, last_run_date}.
-- NULL = report disabled for the workspace.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS usage_report_config JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.usage_report_config IS 'Tenant daily usage report: {enabled, push_to_wecom, notify_user_ids, last_run_date}';
