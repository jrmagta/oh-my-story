-- +goose Up

-- Workspace API tokens. Each acts as its own bot user (never a member, no
-- password), so history and created-by name the token. Only the hash is kept;
-- the prefix lets people tell tokens apart.
ALTER TABLE users ADD COLUMN is_bot boolean NOT NULL DEFAULT false;

CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    bot_user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    scope        text NOT NULL CHECK (scope IN ('read', 'write')),
    prefix       text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at   timestamptz,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_workspace_id ON api_tokens (workspace_id);

-- +goose Down
DROP TABLE api_tokens;
ALTER TABLE users DROP COLUMN is_bot;
