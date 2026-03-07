-- +goose Up
CREATE TABLE user_profiles (
    user_id      UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    slug         TEXT NOT NULL DEFAULT '',
    bio          TEXT NOT NULL DEFAULT '',
    avatar_url   TEXT NOT NULL DEFAULT '',
    website      TEXT NOT NULL DEFAULT '',
    location     TEXT NOT NULL DEFAULT '',
    job_title    TEXT NOT NULL DEFAULT '',
    pronouns     TEXT NOT NULL DEFAULT '',
    twitter      TEXT NOT NULL DEFAULT '',
    github       TEXT NOT NULL DEFAULT '',
    linkedin     TEXT NOT NULL DEFAULT '',
    instagram    TEXT NOT NULL DEFAULT '',
    is_published BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_profiles_slug ON user_profiles(slug);

-- +goose Down
DROP TABLE IF EXISTS user_profiles;
