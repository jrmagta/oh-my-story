-- +goose Up

-- "blocker blocks blocked", between any two steps of a workspace, on the same
-- map or not. The store refuses links that would make a cycle.
CREATE TABLE dependencies (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    blocker_id   uuid NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    blocked_id   uuid NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);
CREATE INDEX dependencies_blocked_id ON dependencies (blocked_id);

-- +goose Down
DROP TABLE dependencies;
