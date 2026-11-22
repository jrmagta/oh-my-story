-- +goose Up

-- Comments on objectives and activities as well as steps: exactly one owner.
ALTER TABLE comments ALTER COLUMN step_id DROP NOT NULL;
ALTER TABLE comments ADD COLUMN node_id uuid REFERENCES nodes (id) ON DELETE CASCADE;
ALTER TABLE comments ADD CONSTRAINT comments_one_owner CHECK (num_nonnulls(step_id, node_id) = 1);
CREATE INDEX comments_node_id ON comments (node_id, created_at) WHERE node_id IS NOT NULL;

-- Files added while writing a comment on a node are its drafts until posted.
ALTER TABLE attachments ADD COLUMN draft_node_id uuid REFERENCES nodes (id) ON DELETE CASCADE;
ALTER TABLE attachments DROP CONSTRAINT attachments_one_owner;
ALTER TABLE attachments ADD CONSTRAINT attachments_one_owner
    CHECK (num_nonnulls(step_id, node_id, comment_id, draft_step_id, draft_node_id) = 1);
DROP INDEX attachments_drafts;
CREATE INDEX attachments_drafts ON attachments (created_at)
    WHERE draft_step_id IS NOT NULL OR draft_node_id IS NOT NULL;

-- +goose Down
DROP INDEX attachments_drafts;
CREATE INDEX attachments_drafts ON attachments (created_at) WHERE draft_step_id IS NOT NULL;
DELETE FROM attachments WHERE draft_node_id IS NOT NULL;
ALTER TABLE attachments DROP CONSTRAINT attachments_one_owner;
ALTER TABLE attachments DROP COLUMN draft_node_id;
ALTER TABLE attachments ADD CONSTRAINT attachments_one_owner
    CHECK (num_nonnulls(step_id, node_id, comment_id, draft_step_id) = 1);
DELETE FROM comments WHERE node_id IS NOT NULL;
DROP INDEX comments_node_id;
ALTER TABLE comments DROP CONSTRAINT comments_one_owner;
ALTER TABLE comments DROP COLUMN node_id;
ALTER TABLE comments ALTER COLUMN step_id SET NOT NULL;
