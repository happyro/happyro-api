package session

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

type PlayerLimiter interface {
	Allow(context.Context, int64, string, int) (int, error)
}
type SQLPlayerLimiter struct{ DB *sqlx.DB }

// Allow returns the retry delay in seconds, or zero when this request is allowed.
// The database clock and locked counter keep limits shared across API processes.
func (s *SQLPlayerLimiter) Allow(ctx context.Context, account int64, category string, limit int) (int, error) {
	if account <= 0 || limit <= 0 || (category != "read" && category != "action" && category != "assets") {
		return 0, errors.New("invalid adventure limit")
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO adventure_limits(account_id,category,attempts,expires_at)
 VALUES(?,?,1,UTC_TIMESTAMP(6)+INTERVAL 60 SECOND)
 ON DUPLICATE KEY UPDATE attempts=IF(expires_at<=UTC_TIMESTAMP(6),1,LEAST(attempts+1,?)),
 expires_at=IF(expires_at<=UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 60 SECOND,expires_at)`, account, category, limit+1)
	if err != nil {
		return 0, err
	}
	var row struct {
		Attempts  int
		Remaining int
	}
	if err = tx.GetContext(ctx, &row, `SELECT attempts,GREATEST(1,CEIL(TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),expires_at)/1000000)) AS remaining FROM adventure_limits WHERE account_id=? AND category=?`, account, category); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	if row.Attempts > limit {
		return row.Remaining, nil
	}
	return 0, nil
}
