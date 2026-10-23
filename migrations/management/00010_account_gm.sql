-- +goose Up
INSERT INTO role_permissions (role_name, permission) VALUES ('administrator', 'players.gm');

-- +goose Down
DELETE FROM role_permissions WHERE role_name='administrator' AND permission='players.gm';
