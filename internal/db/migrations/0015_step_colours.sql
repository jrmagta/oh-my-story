-- +goose Up

-- A step's own colour (#RRGGBB). Null means the step takes its activity's colour.
ALTER TABLE steps ADD COLUMN colour text;

-- +goose Down
ALTER TABLE steps DROP COLUMN colour;
