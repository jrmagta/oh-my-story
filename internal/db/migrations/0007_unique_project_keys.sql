-- +goose Up

-- A workspace's project key (key_prefix, as in CHC-12) is unique across the
-- instance, like a Jira project key, so a step key names one step anywhere.
CREATE UNIQUE INDEX workspaces_key_prefix_key ON workspaces (key_prefix);

-- +goose Down
DROP INDEX workspaces_key_prefix_key;
