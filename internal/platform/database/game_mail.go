package database

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

func CheckMail(ctx context.Context, db *sqlx.DB, receipt bool) error {
	names := []string{"mail", "mail_attachments"}
	if receipt {
		names = append(names, "happyro_admin_mail_receipts")
	}
	query, args, err := sqlx.In("SELECT TABLE_NAME,ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN (?)", names)
	if err != nil {
		return err
	}
	var rows []struct {
		Name   string `db:"TABLE_NAME"`
		Engine string `db:"ENGINE"`
	}
	if err = db.SelectContext(ctx, &rows, query, args...); err != nil {
		return err
	}
	if len(rows) != len(names) {
		return errors.New("mail schema is not prepared; apply the HappyRO server SQL initialization with schema privileges")
	}
	for _, row := range rows {
		if row.Engine != "InnoDB" {
			return errors.New("mail delivery requires InnoDB tables; review the game database storage engines before enabling delivery")
		}
	}
	return nil
}
