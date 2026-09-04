-- Mirrors versioned migration 000098_user_name: user real/display name.

ALTER TABLE users ADD COLUMN name VARCHAR(100) DEFAULT '';
