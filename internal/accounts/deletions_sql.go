package accounts

import (
	"context"
	"database/sql"
	"errors"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type SQLDeletions struct{ DB *sqlx.DB }

const deletionProjection = "account_id,account_name,status,error_code,total_characters,deleted_characters,CAST(created_at AS CHAR) AS created_at,CAST(updated_at AS CHAR) AS updated_at"

func (r *SQLDeletions) Find(ctx context.Context, id int64) (*Deletion, error) {
	var row Deletion
	err := r.DB.GetContext(ctx, &row, "SELECT "+deletionProjection+" FROM happyro_admin_account_deletions WHERE account_id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
func (r *SQLDeletions) Ready(ctx context.Context) error {
	var count int
	if err := r.DB.GetContext(ctx, &count, "SELECT COUNT(*) FROM happyro_admin_workers WHERE worker_name='account-deletions' AND heartbeat_at>=UTC_TIMESTAMP(6)-INTERVAL 10 SECOND"); err != nil {
		return err
	}
	if count != 1 {
		return ErrDeletionWorker
	}
	return nil
}
func (r *SQLDeletions) Enqueue(ctx context.Context, id int64, user identityapi.User) (Deletion, error) {
	conn, release, err := database.LockAccountOperation(ctx, r.DB, id)
	if err != nil {
		return Deletion{}, err
	}
	defer release()
	if err = checkPlayerAccount(ctx, conn, id); err != nil {
		return Deletion{}, err
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO happyro_admin_account_deletions (account_id,requested_by,requested_username,account_name) SELECT account_id,?,?,userid FROM login WHERE account_id=? AND sex<>'S' ON DUPLICATE KEY UPDATE account_id=VALUES(account_id)`, user.ID, user.Username, id)
	if err != nil {
		return Deletion{}, err
	}
	row, err := r.Find(ctx, id)
	if err != nil {
		return Deletion{}, err
	}
	if row == nil {
		return Deletion{}, ErrAccountNotFound
	}
	return *row, nil
}
func (r *SQLDeletions) Retry(ctx context.Context, id int64) (Deletion, error) {
	result, err := r.DB.ExecContext(ctx, "UPDATE happyro_admin_account_deletions SET status='queued',error_code=NULL WHERE account_id=? AND status='failed'", id)
	if err != nil {
		return Deletion{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Deletion{}, err
	}
	if changed != 1 {
		return Deletion{}, ErrDeletionState
	}
	row, err := r.Find(ctx, id)
	if err != nil {
		return Deletion{}, err
	}
	if row == nil {
		return Deletion{}, ErrAccountNotFound
	}
	return *row, nil
}

func (r *SQLDeletions) List(ctx context.Context, page database.Pagination) (DeletionPage, error) {
	result := DeletionPage{Data: []Deletion{}}
	if err := r.DB.GetContext(ctx, &result.Total, "SELECT COUNT(*) FROM happyro_admin_account_deletions"); err != nil {
		return result, err
	}
	err := r.DB.SelectContext(ctx, &result.Data, "SELECT "+deletionProjection+" FROM happyro_admin_account_deletions ORDER BY (status='completed'),created_at DESC,account_id DESC LIMIT ? OFFSET ?", page.Size, page.Offset())
	return result, err
}
