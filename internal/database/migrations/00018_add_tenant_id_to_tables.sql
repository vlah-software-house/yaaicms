-- +goose Up

-- ============================================================================
-- Step 1: Create a "default" tenant for all existing data.
-- ============================================================================
INSERT INTO tenants (id, name, subdomain)
VALUES ('00000000-0000-0000-0000-000000000001', 'Default', 'default');

-- ============================================================================
-- Step 2: Migrate existing user roles to user_tenants join table.
-- Every existing user gets assigned to the default tenant with their current role.
-- ============================================================================
INSERT INTO user_tenants (user_id, tenant_id, role)
SELECT id, '00000000-0000-0000-0000-000000000001', role
FROM users;

-- Mark the first admin as super_admin.
UPDATE users SET is_super_admin = TRUE
WHERE role = 'admin'
  AND id = (SELECT id FROM users WHERE role = 'admin' ORDER BY created_at ASC LIMIT 1);

-- ============================================================================
-- Step 3: Add tenant_id (nullable first) to all tenant-scoped tables.
-- ============================================================================

-- content
ALTER TABLE content ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE content SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE content ALTER COLUMN tenant_id SET NOT NULL;

-- Drop old unique slug constraint, add tenant-scoped one.
ALTER TABLE content DROP CONSTRAINT IF EXISTS content_slug_key;
DROP INDEX IF EXISTS idx_content_slug;
CREATE UNIQUE INDEX idx_content_tenant_slug ON content(tenant_id, slug);
CREATE INDEX idx_content_tenant_id ON content(tenant_id);

-- templates
ALTER TABLE templates ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE templates SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE templates ALTER COLUMN tenant_id SET NOT NULL;

-- Drop old active partial index, add tenant-scoped one (one active per type per tenant).
DROP INDEX IF EXISTS idx_templates_is_active;
CREATE UNIQUE INDEX idx_templates_tenant_type_active ON templates(tenant_id, type) WHERE is_active = TRUE;
CREATE INDEX idx_templates_tenant_id ON templates(tenant_id);

-- categories
ALTER TABLE categories ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE categories SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE categories ALTER COLUMN tenant_id SET NOT NULL;

-- Drop old unique slug constraint, add tenant-scoped one.
ALTER TABLE categories DROP CONSTRAINT IF EXISTS categories_slug_key;
CREATE UNIQUE INDEX idx_categories_tenant_slug ON categories(tenant_id, slug);
CREATE INDEX idx_categories_tenant_id ON categories(tenant_id);

-- media
ALTER TABLE media ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE media SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE media ALTER COLUMN tenant_id SET NOT NULL;
CREATE INDEX idx_media_tenant_id ON media(tenant_id);

-- design_themes
ALTER TABLE design_themes ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE design_themes SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE design_themes ALTER COLUMN tenant_id SET NOT NULL;

-- Drop old active partial index, add tenant-scoped one (one active theme per tenant).
DROP INDEX IF EXISTS idx_design_themes_active;
CREATE UNIQUE INDEX idx_design_themes_tenant_active ON design_themes(tenant_id) WHERE is_active = TRUE;
CREATE INDEX idx_design_themes_tenant_id ON design_themes(tenant_id);

-- site_settings: change PK from (key) to (tenant_id, key).
ALTER TABLE site_settings ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE site_settings SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE site_settings ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE site_settings DROP CONSTRAINT site_settings_pkey;
ALTER TABLE site_settings ADD PRIMARY KEY (tenant_id, key);

-- cache_invalidation_log
ALTER TABLE cache_invalidation_log ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE cache_invalidation_log SET tenant_id = '00000000-0000-0000-0000-000000000001';
ALTER TABLE cache_invalidation_log ALTER COLUMN tenant_id SET NOT NULL;
CREATE INDEX idx_cache_log_tenant_id ON cache_invalidation_log(tenant_id);

-- ============================================================================
-- Step 4: Drop the role column from users (role now lives in user_tenants).
-- We also drop the role constraint and index.
-- ============================================================================
DROP INDEX IF EXISTS idx_users_role;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users DROP COLUMN role;

-- +goose Down

-- Restore role column on users.
ALTER TABLE users ADD COLUMN role VARCHAR(50) NOT NULL DEFAULT 'author';
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'editor', 'author'));
CREATE INDEX idx_users_role ON users(role);

-- Restore roles from user_tenants.
UPDATE users u SET role = ut.role
FROM user_tenants ut
WHERE ut.user_id = u.id
  AND ut.tenant_id = '00000000-0000-0000-0000-000000000001';

-- Remove tenant_id columns (reverse order).
ALTER TABLE cache_invalidation_log DROP COLUMN tenant_id;
DROP INDEX IF EXISTS idx_cache_log_tenant_id;

ALTER TABLE site_settings DROP CONSTRAINT site_settings_pkey;
ALTER TABLE site_settings DROP COLUMN tenant_id;
ALTER TABLE site_settings ADD PRIMARY KEY (key);

DROP INDEX IF EXISTS idx_design_themes_tenant_active;
DROP INDEX IF EXISTS idx_design_themes_tenant_id;
ALTER TABLE design_themes DROP COLUMN tenant_id;
CREATE UNIQUE INDEX idx_design_themes_active ON design_themes(is_active) WHERE is_active = TRUE;

DROP INDEX IF EXISTS idx_media_tenant_id;
ALTER TABLE media DROP COLUMN tenant_id;

DROP INDEX IF EXISTS idx_categories_tenant_slug;
DROP INDEX IF EXISTS idx_categories_tenant_id;
ALTER TABLE categories DROP COLUMN tenant_id;
ALTER TABLE categories ADD CONSTRAINT categories_slug_key UNIQUE (slug);

DROP INDEX IF EXISTS idx_templates_tenant_type_active;
DROP INDEX IF EXISTS idx_templates_tenant_id;
ALTER TABLE templates DROP COLUMN tenant_id;
CREATE UNIQUE INDEX idx_templates_is_active ON templates(is_active) WHERE is_active = TRUE;

DROP INDEX IF EXISTS idx_content_tenant_slug;
DROP INDEX IF EXISTS idx_content_tenant_id;
ALTER TABLE content DROP COLUMN tenant_id;
ALTER TABLE content ADD CONSTRAINT content_slug_key UNIQUE (slug);
CREATE INDEX idx_content_slug ON content(slug);

-- Remove default tenant data and super_admin flag.
DELETE FROM user_tenants WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM tenants WHERE id = '00000000-0000-0000-0000-000000000001';
