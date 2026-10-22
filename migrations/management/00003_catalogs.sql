-- +goose Up
CREATE TABLE catalog_generations (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
 content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
 client_version VARCHAR(128) NOT NULL,
 server_version VARCHAR(128) NOT NULL,
 npc_version VARCHAR(128) NOT NULL,
 metadata JSON NOT NULL,
 item_count INT UNSIGNED NOT NULL,
 monster_count INT UNSIGNED NOT NULL,
 npc_count INT UNSIGNED NOT NULL,
 imported_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE catalog_state (
 id TINYINT PRIMARY KEY CHECK (id=1),
 generation_id BIGINT UNSIGNED NULL,
 FOREIGN KEY (generation_id) REFERENCES catalog_generations(id)
) ENGINE=InnoDB;
INSERT INTO catalog_state (id,generation_id) VALUES (1,NULL);
CREATE TABLE catalog_items (
 generation_id BIGINT UNSIGNED NOT NULL,
 item_id INT UNSIGNED NOT NULL,
 name_zh_cn VARCHAR(255) NOT NULL,
 name_en_us VARCHAR(255) NOT NULL,
 aegis_name VARCHAR(255) NULL,
 item_type VARCHAR(64) NULL,
 item_subtype VARCHAR(64) NULL,
 client_exists BOOLEAN NOT NULL,
 server_exists BOOLEAN NOT NULL,
 payload JSON NOT NULL,
 PRIMARY KEY (generation_id,item_id),
 INDEX item_type (generation_id,item_type,item_subtype),
 INDEX item_aegis (generation_id,aegis_name),
 FOREIGN KEY (generation_id) REFERENCES catalog_generations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE catalog_monsters (
 generation_id BIGINT UNSIGNED NOT NULL,
 monster_id INT UNSIGNED NOT NULL,
 name_zh_cn VARCHAR(255) NOT NULL,
 name_en_us VARCHAR(255) NOT NULL,
 aegis_name VARCHAR(255) NOT NULL,
 kind ENUM('normal','mini','mvp') NOT NULL,
 race VARCHAR(64) NOT NULL,
 element VARCHAR(64) NOT NULL,
 size VARCHAR(64) NOT NULL,
 level INT UNSIGNED NOT NULL,
 payload JSON NOT NULL,
 PRIMARY KEY (generation_id,monster_id),
 INDEX monster_kind (generation_id,kind),
 FOREIGN KEY (generation_id) REFERENCES catalog_generations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE catalog_drops (
 generation_id BIGINT UNSIGNED NOT NULL,
 monster_id INT UNSIGNED NOT NULL,
 item_aegis_name VARCHAR(255) NOT NULL,
 method ENUM('normal','mvp_reward') NOT NULL,
 slot INT UNSIGNED NOT NULL,
 rate INT UNSIGNED NOT NULL,
 PRIMARY KEY (generation_id,monster_id,method,slot),
 INDEX item_drop (generation_id,item_aegis_name),
 FOREIGN KEY (generation_id,monster_id) REFERENCES catalog_monsters(generation_id,monster_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE catalog_npcs (
 generation_id BIGINT UNSIGNED NOT NULL,
 npc_key VARCHAR(255) NOT NULL,
 map VARCHAR(64) NOT NULL,
 map_name VARCHAR(255) NOT NULL,
 name VARCHAR(255) NOT NULL,
 source_name VARCHAR(255) NOT NULL,
 display_name VARCHAR(255) NOT NULL,
 navigation_name VARCHAR(255) NOT NULL,
 navigation_class VARCHAR(255) NOT NULL,
 game_visible BOOLEAN NOT NULL,
 catalog_order INT NOT NULL,
 x INT NOT NULL,
 y INT NOT NULL,
 payload JSON NOT NULL,
 PRIMARY KEY (generation_id,npc_key),
 INDEX npc_map (generation_id,map,game_visible),
 INDEX npc_order (generation_id,catalog_order),
 FOREIGN KEY (generation_id) REFERENCES catalog_generations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE catalog_npcs;
DROP TABLE catalog_drops;
DROP TABLE catalog_monsters;
DROP TABLE catalog_items;
DROP TABLE catalog_state;
DROP TABLE catalog_generations;
