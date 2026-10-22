package database

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// CheckRuntime checks mandatory game schema before the API accepts requests.
// Runtime credentials do not need DDL privileges. Game schema initialization is
// owned by happyro-server; this process only checks required structures.
func CheckRuntime(ctx context.Context, db *sqlx.DB) error {
	if err := CheckAccounts(ctx, db); err != nil {
		return err
	}
	if err := CheckMail(ctx, db, true); err != nil {
		return err
	}
	for _, query := range []string{
		"SELECT worker_name,heartbeat_at FROM happyro_admin_workers LIMIT 0",
		"SELECT account_id,requested_by,requested_username,account_name,status,error_code,total_characters,deleted_characters,created_at,updated_at,completed_at FROM happyro_admin_account_deletions LIMIT 0",
		"SELECT idempotency_key,request_hash,mail_id,created_at FROM happyro_admin_mail_receipts LIMIT 0",
	} {
		rows, err := db.QueryxContext(ctx, query)
		if err != nil {
			return fmt.Errorf("game management schema is not prepared; apply the HappyRO server SQL initialization with schema privileges: %w", err)
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
