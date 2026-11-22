-- +goose Up

-- Labels are plain names; their colour was never used for anything.
ALTER TABLE labels DROP COLUMN colour;

-- +goose Down
ALTER TABLE labels ADD COLUMN colour text NOT NULL DEFAULT '#8A8A93';
ALTER TABLE labels ALTER COLUMN colour DROP DEFAULT;
