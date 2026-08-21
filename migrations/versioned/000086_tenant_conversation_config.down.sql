-- Migration 000086 (down): drop tenant conversation UX config.

ALTER TABLE tenants DROP COLUMN IF EXISTS conversation_config;
