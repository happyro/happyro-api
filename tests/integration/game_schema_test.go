package integration

import (
	"context"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
	"os"
	"path/filepath"
	"strings"
)

func applyServerSchema(ctx context.Context, db *sqlx.DB, name string) error {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "happyro-server", "sql-files", name))
	if err != nil {
		return err
	}
	for _, statement := range strings.Split(string(source), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
func prepareAccounts(ctx context.Context, db *sqlx.DB) error {
	if err := database.CheckAccounts(ctx, db); err == nil {
		return nil
	}
	return applyServerSchema(ctx, db, "happyro-accounts.sql")
}
func prepareMail(ctx context.Context, db *sqlx.DB) error {
	if err := database.CheckMail(ctx, db, false); err != nil {
		return err
	}
	return applyServerSchema(ctx, db, "happyro-mail.sql")
}
func prepareDeletions(ctx context.Context, db *sqlx.DB) error {
	return applyServerSchema(ctx, db, "happyro-admin.sql")
}
