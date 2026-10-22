-- +goose Up
CREATE TABLE settings_revisions (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 idempotency_key VARBINARY(64) NOT NULL UNIQUE,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 requested_by BIGINT UNSIGNED NULL,
 requested_game_account_id BIGINT UNSIGNED NULL,
 changes JSON NOT NULL,
 previous_content MEDIUMTEXT NOT NULL,
 desired_content MEDIUMTEXT NOT NULL,
 status ENUM('prepared','published','applied','failed','uncertain') NOT NULL DEFAULT 'prepared',
 error_code VARCHAR(64) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 CHECK ((requested_by IS NOT NULL AND requested_game_account_id IS NULL) OR
        (requested_by IS NULL AND requested_game_account_id IS NOT NULL)),
 INDEX settings_revision_status(status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE settings_revisions;
