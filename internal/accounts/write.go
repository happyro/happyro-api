package accounts

import (
	"context"
	"database/sql"
	"errors"
	"github.com/go-sql-driver/mysql"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
	"strings"
)

type SQLWriter struct{ DB *sqlx.DB }

const accountProjection = "account_id,userid AS username,email,sex,group_id,state,CAST(lastlogin AS CHAR) AS lastlogin,last_ip"

func (r *SQLWriter) account(ctx context.Context, id int64) (database.Row, error) {
	rows, err := database.Rows(ctx, r.DB, "SELECT "+accountProjection+" FROM login WHERE account_id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrAccountNotFound
	}
	if rows[0]["sex"] == "S" {
		return nil, ErrProtected
	}
	return rows[0], nil
}
func (r *SQLWriter) Create(ctx context.Context, input NewAccount) (database.Row, error) {
	if err := database.CheckAccounts(ctx, r.DB); err != nil {
		return nil, err
	}
	result, err := r.DB.ExecContext(ctx, "INSERT INTO login (userid,user_pass,email,sex) VALUES (?,?,?,?)", input.Username, input.Password, input.Email, input.Sex)
	if err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return nil, ErrAccountExists
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.account(ctx, id)
}
func (r *SQLWriter) Update(ctx context.Context, id int64, input AccountUpdate) (database.Row, error) {
	sets := []string{}
	args := []any{}
	if input.Email != nil {
		sets = append(sets, "email=?")
		args = append(args, *input.Email)
	}
	if input.State != nil {
		sets = append(sets, "state=?")
		args = append(args, *input.State)
		if *input.State != 0 {
			sets = append(sets, "web_auth_token=NULL", "web_auth_token_enabled=0")
		}
	}
	if len(sets) == 0 {
		return nil, errors.New("empty account update")
	}
	args = append(args, id)
	return r.mutateAccount(ctx, id, "UPDATE login SET "+strings.Join(sets, ",")+" WHERE account_id=? AND sex<>'S'", args...)
}
func (r *SQLWriter) Password(ctx context.Context, id int64, password string) (database.Row, error) {
	return r.mutateAccount(ctx, id, "UPDATE login SET user_pass=?,web_auth_token=NULL,web_auth_token_enabled=0 WHERE account_id=? AND sex<>'S'", password, id)
}

// Validate the protected account while holding the shared account operation lock.
func checkPlayerAccount(ctx context.Context, conn *sqlx.Conn, id int64) error {
	var sex string
	err := conn.GetContext(ctx, &sex, "SELECT sex FROM login WHERE account_id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	if sex == "S" {
		return ErrProtected
	}
	return nil
}
func (r *SQLWriter) mutateAccount(ctx context.Context, id int64, query string, args ...any) (database.Row, error) {
	conn, release, err := database.LockAccountOperation(ctx, r.DB, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = checkPlayerAccount(ctx, conn, id); err != nil {
		return nil, err
	}
	var deleting int
	if err = conn.GetContext(ctx, &deleting, "SELECT COUNT(*) FROM happyro_admin_account_deletions WHERE account_id=?", id); err != nil {
		return nil, err
	}
	if deleting != 0 {
		return nil, ErrAccountDeleting
	}
	if _, err = conn.ExecContext(ctx, query, args...); err != nil {
		return nil, err
	}
	rows, err := database.Rows(ctx, conn, "SELECT "+accountProjection+" FROM login WHERE account_id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrAccountNotFound
	}
	return rows[0], nil
}
