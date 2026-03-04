-- +goose Up
-- Platform-level super admin flag. Super admins can manage all tenants.
ALTER TABLE users ADD COLUMN is_super_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS is_super_admin;
