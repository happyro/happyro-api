-- +goose Up
CREATE TABLE users (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 username VARCHAR(64) NOT NULL UNIQUE,
 name VARCHAR(255) NOT NULL,
 password_hash VARCHAR(255) NOT NULL,
 is_active BOOLEAN NOT NULL DEFAULT TRUE,
 last_login_at DATETIME(6) NULL,
 last_login_ip VARCHAR(45) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE roles (
 name VARCHAR(64) NOT NULL PRIMARY KEY,
 label VARCHAR(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE user_roles (
 user_id BIGINT UNSIGNED NOT NULL,
 role_name VARCHAR(64) NOT NULL,
 PRIMARY KEY (user_id, role_name),
 FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
 FOREIGN KEY (role_name) REFERENCES roles(name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE role_permissions (
 role_name VARCHAR(64) NOT NULL,
 permission VARCHAR(128) NOT NULL,
 PRIMARY KEY (role_name, permission),
 FOREIGN KEY (role_name) REFERENCES roles(name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO roles (name, label) VALUES
 ('super_admin','超级管理员'), ('administrator','管理员'), ('operator','运营人员'), ('auditor','审计员');
INSERT INTO role_permissions (role_name, permission) VALUES
 ('super_admin','*'),
 ('administrator','players.view'), ('administrator','players.edit'),
 ('administrator','game-data.view'), ('administrator','operations.item-grant'),
 ('administrator','operations.game-control'), ('administrator','settings.manage'),
 ('operator','players.view'), ('operator','game-data.view'), ('operator','operations.item-grant'),
 ('auditor','players.view'), ('auditor','game-data.view');
CREATE TABLE sessions (
 token CHAR(43) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 data BLOB NOT NULL,
 expiry TIMESTAMP(6) NOT NULL,
 INDEX sessions_expiry (expiry)
) ENGINE=InnoDB;
CREATE TABLE audit_logs (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 actor_id BIGINT UNSIGNED NULL,
 username VARCHAR(64) NOT NULL DEFAULT '',
 action VARCHAR(128) NOT NULL,
 ip_address VARCHAR(45) NOT NULL DEFAULT '',
 user_agent VARCHAR(1024) NOT NULL DEFAULT '',
 metadata JSON NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX audit_actor_time (actor_id, created_at),
 INDEX audit_action_time (action, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE login_attempts (
 throttle_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 attempts INT UNSIGNED NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 INDEX login_attempts_expiry (expires_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE login_attempts;
DROP TABLE audit_logs;
DROP TABLE sessions;
DROP TABLE role_permissions;
DROP TABLE user_roles;
DROP TABLE roles;
DROP TABLE users;
