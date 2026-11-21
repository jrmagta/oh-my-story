-- +goose Up

-- Read-only links to a map for people without an account. The token is
-- shown once and only its hash is kept.
CREATE TABLE share_links (
    id           uuid PRIMARY KEY,
    map_id       uuid NOT NULL REFERENCES maps (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at   timestamptz,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX share_links_map_id ON share_links (map_id);

-- +goose Down
DROP TABLE share_links;
