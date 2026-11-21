-- +goose Up

-- First and last name, from the identity provider (SSO users) and shown
-- together as display_name. Null for accounts made with a password.
ALTER TABLE users ADD COLUMN first_name text, ADD COLUMN last_name text;

-- +goose Down
ALTER TABLE users DROP COLUMN first_name, DROP COLUMN last_name;
