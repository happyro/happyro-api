-- +goose Up
ALTER TABLE settings_revisions ADD COLUMN command_attempt INT UNSIGNED NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE settings_revisions DROP COLUMN command_attempt;
