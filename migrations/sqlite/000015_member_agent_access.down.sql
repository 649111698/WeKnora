-- Mirrors versioned migration 000093_member_agent_access (down).

ALTER TABLE tenant_members DROP COLUMN allowed_agent_ids;
