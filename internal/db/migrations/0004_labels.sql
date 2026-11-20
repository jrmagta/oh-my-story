-- +goose Up

-- Labels are shared by every map in a workspace. Names are unique ignoring case.
CREATE TABLE labels (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         citext NOT NULL,
    colour       text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE step_labels (
    step_id  uuid NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (step_id, label_id)
);
CREATE INDEX step_labels_label_id ON step_labels (label_id);

-- +goose Down
DROP TABLE step_labels;
DROP TABLE labels;
