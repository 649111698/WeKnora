-- Migration 000094 (down): drop tenant branding config.

ALTER TABLE tenants DROP COLUMN IF EXISTS branding_config;
