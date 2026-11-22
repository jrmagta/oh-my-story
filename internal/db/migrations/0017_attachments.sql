-- +goose Up

-- Files on a step, a node (objective or activity) or a comment: exactly one.
-- The bytes live in attachment_data, so listing and counting never read them.
-- Deleting is soft, for undo; a file goes and comes back with its owner,
-- since it is only reached through it.
CREATE TABLE attachments (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    step_id      uuid REFERENCES steps (id) ON DELETE CASCADE,
    node_id      uuid REFERENCES nodes (id) ON DELETE CASCADE,
    comment_id   uuid REFERENCES comments (id) ON DELETE CASCADE,
    name         text NOT NULL,
    content_type text NOT NULL, -- sniffed from the bytes, never the uploader's claim
    size_bytes   bigint NOT NULL,
    sha256       bytea NOT NULL,
    uploaded_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    CHECK (num_nonnulls(step_id, node_id, comment_id) = 1)
);
CREATE INDEX attachments_step_id ON attachments (step_id) WHERE step_id IS NOT NULL;
CREATE INDEX attachments_node_id ON attachments (node_id) WHERE node_id IS NOT NULL;
CREATE INDEX attachments_comment_id ON attachments (comment_id) WHERE comment_id IS NOT NULL;

CREATE TABLE attachment_data (
    attachment_id uuid PRIMARY KEY REFERENCES attachments (id) ON DELETE CASCADE,
    data          bytea NOT NULL
);
-- Out of line and uncompressed: most files (images, PDFs, zips) are compressed already.
ALTER TABLE attachment_data ALTER COLUMN data SET STORAGE EXTERNAL;

-- +goose Down
DROP TABLE attachment_data;
DROP TABLE attachments;
