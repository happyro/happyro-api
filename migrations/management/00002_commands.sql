-- +goose Up
CREATE TABLE game_server_commands (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 idempotency_key VARBINARY(64) NOT NULL UNIQUE,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 type VARCHAR(64) NOT NULL,
 status ENUM('pending','running','succeeded','failed','indeterminate') NOT NULL,
 target_type VARCHAR(32) NULL,
 target_id VARCHAR(64) NULL,
 payload JSON NOT NULL,
 result JSON NULL,
 error_code VARCHAR(64) NULL,
 error_message VARCHAR(255) NULL,
 requested_by BIGINT UNSIGNED NULL,
 requested_game_account_id BIGINT UNSIGNED NULL,
 started_at DATETIME(6) NULL,
 completed_at DATETIME(6) NULL,
 deadline DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 CHECK ((requested_by IS NOT NULL AND requested_game_account_id IS NULL) OR
        (requested_by IS NULL AND requested_game_account_id IS NOT NULL)),
 INDEX command_actor_time (requested_by,created_at),
 INDEX command_deadline (status,deadline)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE game_server_commands;
