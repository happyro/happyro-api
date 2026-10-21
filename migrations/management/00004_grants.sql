-- +goose Up
ALTER TABLE audit_logs ADD COLUMN operation_key VARBINARY(128) NULL UNIQUE;
CREATE TABLE mail_grants (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
 idempotency_key VARBINARY(64) NOT NULL UNIQUE,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 item_id INT UNSIGNED NOT NULL,
 char_id INT UNSIGNED NOT NULL,
 amount INT UNSIGNED NOT NULL,
 title VARCHAR(255) NOT NULL,
 requested_by BIGINT UNSIGNED NOT NULL,
 prepared JSON NOT NULL,
 status ENUM('pending','sent') NOT NULL DEFAULT 'pending',
 mail_id BIGINT UNSIGNED NULL,
 error VARCHAR(64) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 INDEX grants_requester (requested_by,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE mail_grants;
ALTER TABLE audit_logs DROP COLUMN operation_key;
