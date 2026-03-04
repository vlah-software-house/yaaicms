-- +goose Up
-- Seed social meta tag settings for all existing tenants.
-- og_default_image: fallback Open Graph image URL when content has no featured image.
-- twitter_site: Twitter/X handle (e.g. @yoursite) for twitter:site meta tag.
INSERT INTO site_settings (tenant_id, key, value)
SELECT t.id, s.key, s.value
FROM tenants t
CROSS JOIN (VALUES
    ('og_default_image', ''),
    ('twitter_site',     '')
) AS s(key, value)
WHERE NOT EXISTS (
    SELECT 1 FROM site_settings ss
    WHERE ss.tenant_id = t.id AND ss.key = s.key
);

-- +goose Down
DELETE FROM site_settings WHERE key IN ('og_default_image', 'twitter_site');
