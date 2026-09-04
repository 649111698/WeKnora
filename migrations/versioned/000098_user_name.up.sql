-- Migration 000098: user real/display name (姓名).
--
-- users.name is the human-facing label shown in member management and
-- usage reports; username stays the login handle. Empty string for
-- legacy rows (backfilled by SSO login or admin edit).

ALTER TABLE users ADD COLUMN IF NOT EXISTS name VARCHAR(100) DEFAULT '';
COMMENT ON COLUMN users.name IS 'Real/display name; empty until backfilled';
