-- Mirrors versioned migration 000093_member_agent_access:
-- per-member agent access control (NULL = unrestricted).

ALTER TABLE tenant_members ADD COLUMN allowed_agent_ids TEXT;
