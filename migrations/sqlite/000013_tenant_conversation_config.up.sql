-- Mirrors versioned migration 000086_tenant_conversation_config:
-- tenant-level chat UX config (hide model dropdown + pin chat model).

ALTER TABLE tenants ADD COLUMN conversation_config TEXT;
