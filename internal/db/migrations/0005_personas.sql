-- +goose Up

-- Personas are shared by every map in a workspace and link to objectives.
CREATE TABLE personas (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         citext NOT NULL,
    description  text NOT NULL DEFAULT '',
    colour       text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE persona_nodes (
    persona_id uuid NOT NULL REFERENCES personas (id) ON DELETE CASCADE,
    node_id    uuid NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    PRIMARY KEY (persona_id, node_id)
);
CREATE INDEX persona_nodes_node_id ON persona_nodes (node_id);

-- +goose Down
DROP TABLE persona_nodes;
DROP TABLE personas;
