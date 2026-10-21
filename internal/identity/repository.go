package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type SQLRepository struct{ DB *sqlx.DB }

func (r *SQLRepository) ByUsername(ctx context.Context, username string) (User, error) {
	return r.find(ctx, "username", username)
}
func (r *SQLRepository) ByID(ctx context.Context, id int64) (User, error) {
	return r.find(ctx, "id", id)
}
func (r *SQLRepository) find(ctx context.Context, column string, value any) (User, error) {
	var user User
	err := r.DB.GetContext(ctx, &user, "SELECT id,username,name,password_hash,is_active FROM users WHERE "+column+" = ?", value)
	if errors.Is(err, sql.ErrNoRows) {
		return user, ErrNotFound
	}
	if err != nil {
		return user, err
	}
	user.Roles = []string{}
	user.Permissions = []string{}
	if err := r.DB.SelectContext(ctx, &user.Roles, "SELECT role_name FROM user_roles WHERE user_id=? ORDER BY role_name", user.ID); err != nil {
		return user, err
	}
	err = r.DB.SelectContext(ctx, &user.Permissions, `SELECT DISTINCT rp.permission FROM role_permissions rp
 JOIN user_roles ur ON ur.role_name=rp.role_name WHERE ur.user_id=? ORDER BY rp.permission`, user.ID)
	return user, err
}

func (r *SQLRepository) Attempt(ctx context.Context, key string) (int, error) {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Increment under a row lock before password verification, so concurrent
	// attempts cannot bypass the limit. Use the database clock on all replicas.
	_, err = tx.ExecContext(ctx, `INSERT INTO login_attempts (throttle_key,attempts,expires_at)
 VALUES (?,1,DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 60 SECOND))
 ON DUPLICATE KEY UPDATE attempts=IF(expires_at<=UTC_TIMESTAMP(6),1,attempts+1),
 expires_at=IF(expires_at<=UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 60 SECOND),expires_at)`, key)
	if err != nil {
		return 0, err
	}
	var row struct {
		Attempts  int
		Remaining int
	}
	err = tx.GetContext(ctx, &row, `SELECT attempts,GREATEST(1,CEIL(TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),expires_at)/1000000)) AS remaining FROM login_attempts WHERE throttle_key=?`, key)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	if row.Attempts > 5 {
		return row.Remaining, nil
	}
	return 0, nil
}

func writeAudit(ctx context.Context, db sqlx.ExtContext, action string, user *User, username string, client Client) error {
	var id any
	if user != nil {
		id = user.ID
	}
	agent := []rune(client.UserAgent)
	if len(agent) > 1024 {
		agent = agent[:1024]
	}
	_, err := db.ExecContext(ctx, `INSERT INTO audit_logs (actor_id,username,action,ip_address,user_agent,metadata) VALUES (?,?,?,?,?,'{}')`, id, username, action, client.IP, string(agent))
	return err
}

func (r *SQLRepository) Audit(ctx context.Context, action string, user *User, username string, client Client) error {
	return writeAudit(ctx, r.DB, action, user, username, client)
}
func (r *SQLRepository) LoginSucceeded(ctx context.Context, user User, client Client, key string) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE users SET last_login_at=UTC_TIMESTAMP(6),last_login_ip=? WHERE id=?", client.IP, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM login_attempts WHERE throttle_key=?", key); err != nil {
		return err
	}
	if err = writeAudit(ctx, tx, "auth.login_succeeded", &user, user.Username, client); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) Provision(ctx context.Context, input NewUser, hash string, initial bool) (bool, error) {
	// Serialize initial provisioning across processes on a dedicated connection.
	conn, err := r.DB.Connx(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var locked int
	if err = conn.GetContext(ctx, &locked, "SELECT GET_LOCK(CONCAT(DATABASE(), ':initial-user'),10)"); err != nil {
		return false, err
	}
	if locked != 1 {
		return false, errors.New("initial user initialization is busy")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn.ExecContext(release, "SELECT RELEASE_LOCK(CONCAT(DATABASE(), ':initial-user'))")
	}()
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if initial {
		var count int
		if err = tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM users"); err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO users (username,name,password_hash) VALUES (?,?,?)", input.Username, input.Name, hash)
	if err != nil {
		return false, fmt.Errorf("create admin user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO user_roles (user_id,role_name) VALUES (?,?)", id, input.Role); err != nil {
		return false, err
	}
	if err = writeAudit(ctx, tx, "user.created", &User{ID: id}, input.Username, Client{UserAgent: "console"}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *SQLRepository) ChangePassword(ctx context.Context, username, hash string) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var user User
	if err = tx.GetContext(ctx, &user, "SELECT id,username FROM users WHERE username=? FOR UPDATE", username); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE id=?", hash, user.ID); err != nil {
		return err
	}
	// Session authentication checks this version against the current password hash.
	if err = writeAudit(ctx, tx, "user.password_changed", &user, user.Username, Client{UserAgent: "console"}); err != nil {
		return err
	}
	return tx.Commit()
}
