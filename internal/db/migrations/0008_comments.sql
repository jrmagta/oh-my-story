-- +goose Up

-- Comments on a step, with one level of replies. Deleting keeps the row (so
-- its replies still have a thread) and blanks it in the API.
CREATE TABLE comments (
    id          uuid PRIMARY KEY,
    step_id     uuid NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    parent_id   uuid REFERENCES comments (id) ON DELETE CASCADE,
    author_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    body_md     text NOT NULL,
    resolved_at timestamptz,
    resolved_by uuid REFERENCES users (id) ON DELETE SET NULL,
    edited_at   timestamptz,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comments_step_id ON comments (step_id, created_at);
CREATE INDEX comments_parent_id ON comments (parent_id);

-- Who a comment mentions; each is emailed once, when first mentioned.
CREATE TABLE mentions (
    comment_id uuid NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (comment_id, user_id)
);

-- +goose Down
DROP TABLE mentions;
DROP TABLE comments;
