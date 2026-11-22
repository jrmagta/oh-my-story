-- +goose Up

-- A file added while writing a comment is a draft of its step until the
-- comment is posted and claims it; only its uploader can see it. Unclaimed
-- drafts are swept after a day.
ALTER TABLE attachments ADD COLUMN draft_step_id uuid REFERENCES steps (id) ON DELETE CASCADE;
ALTER TABLE attachments DROP CONSTRAINT attachments_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_one_owner
    CHECK (num_nonnulls(step_id, node_id, comment_id, draft_step_id) = 1);

-- For the sweeper.
CREATE INDEX attachments_drafts ON attachments (created_at) WHERE draft_step_id IS NOT NULL;
CREATE INDEX attachments_deleted ON attachments (deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX attachments_deleted;
DROP INDEX attachments_drafts;
DELETE FROM attachments WHERE draft_step_id IS NOT NULL;
ALTER TABLE attachments DROP CONSTRAINT attachments_one_owner;
ALTER TABLE attachments DROP COLUMN draft_step_id;
ALTER TABLE attachments ADD CONSTRAINT attachments_check CHECK (num_nonnulls(step_id, node_id, comment_id) = 1);
