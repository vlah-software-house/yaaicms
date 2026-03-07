-- +goose Up
ALTER TABLE templates DROP CONSTRAINT templates_type_check;
ALTER TABLE templates ADD CONSTRAINT templates_type_check
    CHECK (type IN ('header', 'footer', 'page', 'article_loop', 'author_page'));

-- +goose Down
DELETE FROM templates WHERE type = 'author_page';
ALTER TABLE templates DROP CONSTRAINT templates_type_check;
ALTER TABLE templates ADD CONSTRAINT templates_type_check
    CHECK (type IN ('header', 'footer', 'page', 'article_loop'));
