-- +goose Up

-- A step on several maps logs each change once per map, so every map's feed
-- and history has it. The copies are echoes: a step's own history and
-- outgoing notifications skip them.
ALTER TABLE events ADD COLUMN echo boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE events DROP COLUMN echo;
