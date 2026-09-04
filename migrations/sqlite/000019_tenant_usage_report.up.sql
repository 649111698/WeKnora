-- Mirrors versioned migration 000097_tenant_usage_report:
-- tenant daily usage report config (NULL = disabled).

ALTER TABLE tenants ADD COLUMN usage_report_config TEXT;
