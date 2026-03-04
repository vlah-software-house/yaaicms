-- +goose Up
-- Many-to-many: users can belong to multiple tenants, each with a per-tenant role.
CREATE TABLE user_tenants (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role       VARCHAR(50) NOT NULL DEFAULT 'author',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, tenant_id),
    CONSTRAINT user_tenants_role_check CHECK (role IN ('admin', 'editor', 'author'))
);

CREATE INDEX idx_user_tenants_tenant_id ON user_tenants(tenant_id);

-- +goose Down
DROP TABLE IF EXISTS user_tenants;
