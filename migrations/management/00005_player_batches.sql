-- +goose Up
CREATE TABLE player_batches (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
 idempotency_key VARBINARY(64) NOT NULL UNIQUE,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 actor_id BIGINT UNSIGNED NOT NULL,
 action VARCHAR(16) NOT NULL,
 account_ids JSON NOT NULL,
 result JSON NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 completed_at DATETIME(6) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE player_batches;
