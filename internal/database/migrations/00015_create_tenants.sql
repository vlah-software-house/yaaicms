-- +goose Up
-- Multi-tenancy: tenants table holds one row per isolated blog/site.
-- Each tenant is identified by a unique subdomain (e.g., "blog1" for blog1.smartpress.io).
CREATE TABLE tenants (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(200) NOT NULL,
    subdomain  VARCHAR(100) NOT NULL UNIQUE,
    is_active  BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_tenants_subdomain ON tenants(subdomain);

-- +goose Down
DROP TABLE IF EXISTS tenants;
