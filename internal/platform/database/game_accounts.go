package database

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

// Registration also happens in the game server, so an application mutex cannot
// enforce uniqueness. The index uses the existing userid collation.
func CheckAccounts(ctx context.Context, db *sqlx.DB) error {
	var count int
	err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM (SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='login' AND NON_UNIQUE=0 GROUP BY INDEX_NAME HAVING COUNT(*)=1 AND MAX(COLUMN_NAME)='userid' AND MAX(SUB_PART) IS NULL) AS unique_names`)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("account schema is not prepared; apply the HappyRO server SQL initialization with schema privileges")
	}
	return nil
}
