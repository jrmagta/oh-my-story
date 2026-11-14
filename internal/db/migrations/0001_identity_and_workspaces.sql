-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id            uuid PRIMARY KEY,
    email         citext NOT NULL UNIQUE,
    display_name  text NOT NULL,
    avatar_url    text,
    password_hash text,
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE identities (
    id      uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issuer  text NOT NULL,
    subject text NOT NULL,
    UNIQUE (issuer, subject)
);
CREATE INDEX identities_user_id ON identities (user_id);

CREATE TABLE sessions (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    user_agent text,
    ip         inet,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id ON sessions (user_id);

CREATE TABLE workspaces (
    id             uuid PRIMARY KEY,
    name           text NOT NULL,
    slug           citext NOT NULL UNIQUE,
    key_prefix     text NOT NULL DEFAULT 'STORY' CHECK (key_prefix ~ '^[A-Z][A-Z0-9]{0,9}$'),
    next_story_seq integer NOT NULL DEFAULT 1 CHECK (next_story_seq > 0),
    estimate_unit  text NOT NULL DEFAULT 'points',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'commenter', 'viewer')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX workspace_members_user_id ON workspace_members (user_id);

CREATE TABLE invites (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        citext NOT NULL,
    role         text NOT NULL CHECK (role IN ('admin', 'editor', 'commenter', 'viewer')),
    token_hash   bytea NOT NULL UNIQUE,
    invited_by   uuid NOT NULL REFERENCES users (id),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
-- At most one open invite per email per workspace.
CREATE UNIQUE INDEX invites_open_email ON invites (workspace_id, email) WHERE accepted_at IS NULL;

-- +goose Down
DROP TABLE invites;
DROP TABLE workspace_members;
DROP TABLE workspaces;
DROP TABLE sessions;
DROP TABLE identities;
DROP TABLE users;
