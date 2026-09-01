-- Migration 000093: per-member agent access control.
--
-- tenant_members.allowed_agent_ids restricts which of the tenant's own
-- custom agents the member sees in the chat picker and may run. NULL keeps
-- the previous behaviour (all agents); an empty JSON array means no custom
-- agent. Built-in agents and agents shared into the tenant are unaffected.
-- See internal/types/tenant_member.go (AgentIDList).

ALTER TABLE tenant_members ADD COLUMN IF NOT EXISTS allowed_agent_ids JSONB DEFAULT NULL;
COMMENT ON COLUMN tenant_members.allowed_agent_ids IS 'Allowed tenant custom agent IDs; NULL = unrestricted';
