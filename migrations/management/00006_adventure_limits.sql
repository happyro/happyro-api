-- +goose Up
CREATE TABLE adventure_limits (
 account_id INT UNSIGNED NOT NULL,
 category VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 attempts INT UNSIGNED NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 PRIMARY KEY(account_id,category),
 INDEX adventure_limits_expiry(expires_at)
) ENGINE=InnoDB;
-- +goose Down
DROP TABLE adventure_limits;
