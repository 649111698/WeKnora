-- Migration 000093 (down): drop per-member agent access control.

ALTER TABLE tenant_members DROP COLUMN IF EXISTS allowed_agent_ids;
