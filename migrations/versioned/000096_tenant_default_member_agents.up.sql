-- Migration 000096: default agent allowlist applied to new tenant members.
--
-- tenants.default_member_agent_ids seeds tenant_members.allowed_agent_ids
-- when a non-Owner member joins (direct add / create / invitation accept /
-- SSO JIT). NULL keeps the historical behaviour: new members are
-- unrestricted until an Owner edits them. Existing members are untouched.
-- INTEGER-style tenant PK follows 000065/000067/000095.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS default_member_agent_ids JSONB DEFAULT NULL;
COMMENT ON COLUMN tenants.default_member_agent_ids IS 'Agent ID list copied into new members allowed_agent_ids on join; NULL = no default (unrestricted)';
