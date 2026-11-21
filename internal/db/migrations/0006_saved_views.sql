-- +goose Up

-- A named map filter. Private to its creator unless shared with the workspace.
-- Not map content, so changes are not in the event log.
CREATE TABLE saved_views (
    id         uuid PRIMARY KEY,
    map_id     uuid NOT NULL REFERENCES maps (id) ON DELETE CASCADE,
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL,
    filter     jsonb NOT NULL,
    is_shared  boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saved_views_map_id ON saved_views (map_id);

-- +goose Down
DROP TABLE saved_views;
