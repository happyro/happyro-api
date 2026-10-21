-- +goose Up
ALTER TABLE settings_revisions
 ADD COLUMN map_instance VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
 ADD COLUMN map_revision BIGINT UNSIGNED NULL,
 ADD CONSTRAINT settings_admission_pair CHECK ((map_instance IS NULL) = (map_revision IS NULL));

-- +goose Down
ALTER TABLE settings_revisions DROP CONSTRAINT settings_admission_pair,
 DROP COLUMN map_revision, DROP COLUMN map_instance;
