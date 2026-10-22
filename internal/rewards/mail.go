package rewards

import (
	"context"
	"database/sql"
	"errors"

	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type SQLMail struct{ DB *sqlx.DB }

func (s *SQLMail) Deliver(ctx context.Context, key, hash string, prepared PreparedMail) (int64, error) {
	if err := database.CheckMail(ctx, s.DB, true); err != nil {
		return 0, err
	}
	conn, err := s.DB.Connx(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	tx, err := conn.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// An upsert takes an exclusive row lock. INSERT followed by a duplicate-key
	// read can deadlock when simultaneous retries upgrade shared locks.
	insertedReceipt, err := tx.ExecContext(ctx, "INSERT INTO happyro_admin_mail_receipts (idempotency_key,request_hash) VALUES (?,?) ON DUPLICATE KEY UPDATE idempotency_key=VALUES(idempotency_key)", key, hash)
	if err != nil {
		return 0, err
	}
	affected, err := insertedReceipt.RowsAffected()
	if err != nil {
		return 0, err
	}
	created := affected == 1
	var receipt struct {
		Hash   string `db:"request_hash"`
		MailID *int64 `db:"mail_id"`
	}
	if err = tx.GetContext(ctx, &receipt, "SELECT request_hash,mail_id FROM happyro_admin_mail_receipts WHERE idempotency_key=? FOR UPDATE", key); err != nil {
		return 0, err
	}
	if receipt.Hash != hash {
		return 0, ErrConflict
	}
	if receipt.MailID != nil {
		return *receipt.MailID, tx.Commit()
	}
	if !created {
		return 0, ErrPending
	}
	var target struct {
		AccountID int64  `db:"account_id"`
		Name      string `db:"name"`
	}
	err = tx.GetContext(ctx, &target, "SELECT account_id,name FROM `char` WHERE char_id=?", prepared.Request.CharacterID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCharacter
	}
	if err != nil {
		return 0, err
	}
	release, err := database.LockAccountConnection(ctx, conn, target.AccountID)
	if err != nil {
		return 0, err
	}
	defer release()
	var deleting int
	if err = tx.GetContext(ctx, &deleting, "SELECT COUNT(*) FROM happyro_admin_account_deletions WHERE account_id=?", target.AccountID); err != nil {
		return 0, err
	}
	if deleting != 0 {
		return 0, ErrAccountDeleting
	}
	// Revalidate after acquiring the account lock; deletion could have completed
	// while this delivery was waiting. Read committed avoids a stale snapshot.
	var name string
	err = tx.GetContext(ctx, &name, "SELECT name FROM `char` WHERE char_id=? AND account_id=?", prepared.Request.CharacterID, target.AccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCharacter
	}
	if err != nil {
		return 0, err
	}
	input := prepared.Request
	inserted, err := tx.ExecContext(ctx, `INSERT INTO mail (send_name,send_id,dest_name,dest_id,title,message,time,status,zeny,type) VALUES (?,0,?,?,?,?,UNIX_TIMESTAMP(),0,0,0)`, prepared.Sender, name, input.CharacterID, input.Title, input.Message)
	if err != nil {
		return 0, err
	}
	mailID, err := inserted.LastInsertId()
	if err != nil {
		return 0, err
	}
	bound := 0
	if input.Bound {
		bound = 1
	}
	for slot, amount := range prepared.AttachmentAmounts {
		if _, err = tx.ExecContext(ctx, "INSERT INTO mail_attachments (id,`index`,nameid,amount,refine,attribute,identify,card0,card1,card2,card3,unique_id,bound,enchantgrade) VALUES (?,?,?,?,0,0,1,0,0,0,0,0,?,0)", mailID, slot, input.ItemID, amount, bound); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE happyro_admin_mail_receipts SET mail_id=? WHERE idempotency_key=?", mailID, key); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return mailID, nil
}
