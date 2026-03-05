-- +goose Up
-- Menu locations per tenant (main, footer, footer_legal).
CREATE TABLE menus (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    location   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, location)
);

-- Ordered links within a menu; one level of nesting via parent_id.
CREATE TABLE menu_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    menu_id    UUID NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES menu_items(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,
    url        TEXT NOT NULL DEFAULT '',
    content_id UUID REFERENCES content(id) ON DELETE SET NULL,
    target     TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_menu_items_menu_id ON menu_items(menu_id);
CREATE INDEX idx_menu_items_parent_id ON menu_items(parent_id);

-- +goose Down
DROP TABLE IF EXISTS menu_items;
DROP TABLE IF EXISTS menus;
