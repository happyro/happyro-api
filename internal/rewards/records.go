package rewards

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type SQLRecords struct{ DB *sqlx.DB }

func (r *SQLRecords) Find(ctx context.Context, key string) (*MailRecord, error) {
	var row struct {
		ID       int64  `db:"id"`
		Hash     string `db:"request_hash"`
		Status   string `db:"status"`
		MailID   *int64 `db:"mail_id"`
		Prepared []byte `db:"prepared"`
	}
	err := r.DB.GetContext(ctx, &row, "SELECT id,request_hash,status,mail_id,prepared FROM mail_grants WHERE idempotency_key=?", key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record := &MailRecord{ID: row.ID, Hash: row.Hash, Status: row.Status, MailID: row.MailID}
	if err = json.Unmarshal(row.Prepared, &record.Prepared); err != nil {
		return nil, err
	}
	return record, nil
}
func (r *SQLRecords) Create(ctx context.Context, key, hash string, prepared PreparedMail, actor int64) (MailRecord, error) {
	raw, err := json.Marshal(prepared)
	if err != nil {
		return MailRecord{}, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO mail_grants (idempotency_key,request_hash,item_id,char_id,amount,title,requested_by,prepared) VALUES (?,?,?,?,?,?,?,?)`, key, hash, prepared.Request.ItemID, prepared.Request.CharacterID, prepared.Request.Amount, prepared.Request.Title, actor, raw)
	if err != nil {
		var duplicate *mysql.MySQLError
		if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
			return MailRecord{}, err
		}
	}
	record, err := r.Find(ctx, key)
	if err != nil {
		return MailRecord{}, err
	}
	if record == nil {
		return MailRecord{}, errors.New("mail record disappeared")
	}
	return *record, nil
}
func (r *SQLRecords) Sent(ctx context.Context, record MailRecord, mailID int64, user identityapi.User, client identityapi.Client) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing struct {
		Status string `db:"status"`
		MailID *int64 `db:"mail_id"`
	}
	if err = tx.GetContext(ctx, &existing, "SELECT status,mail_id FROM mail_grants WHERE id=? FOR UPDATE", record.ID); err != nil {
		return err
	}
	if existing.Status == "sent" {
		if existing.MailID == nil || *existing.MailID != mailID {
			return ErrConflict
		}
		return nil
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mail_grants SET status='sent',mail_id=?,error=NULL WHERE id=?", mailID, record.ID); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{"delivery": "mail", "mail_id": mailID, "item_id": record.Prepared.Request.ItemID, "char_id": record.Prepared.Request.CharacterID, "amount": record.Prepared.Request.Amount})
	if err != nil {
		return err
	}
	agent := []rune(client.UserAgent)
	if len(agent) > 1024 {
		agent = agent[:1024]
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs (actor_id,username,action,ip_address,user_agent,metadata,operation_key) VALUES (?,?,'operations.item_mailed',?,?,?,CONCAT('mail:',?))`, user.ID, user.Username, client.IP, string(agent), metadata, record.ID); err != nil {
		return err
	}
	return tx.Commit()
}

type RecordHistory interface {
	History(context.Context, database.Pagination) (audit.History, error)
}

func (r *SQLRecords) History(ctx context.Context, p database.Pagination) (audit.History, error) {
	page, err := database.Paginate(ctx, r.DB, "g.id,g.idempotency_key,g.item_id,g.char_id,g.amount,g.title,g.status,g.mail_id,g.error,g.requested_by,DATE_FORMAT(g.created_at,'%Y-%m-%dT%H:%i:%s.%fZ') AS created_at,DATE_FORMAT(g.updated_at,'%Y-%m-%dT%H:%i:%s.%fZ') AS updated_at,u.name AS actor_name,u.username AS actor_username", "mail_grants g LEFT JOIN users u ON u.id=g.requested_by", "", "g.id DESC", nil, p)
	if err != nil {
		return audit.History{}, err
	}
	for _, row := range page.Data {
		row["requester"] = nil
		if row["actor_username"] != nil {
			row["requester"] = map[string]any{"id": row["requested_by"], "name": row["actor_name"], "username": row["actor_username"]}
		}
		delete(row, "actor_name")
		delete(row, "actor_username")
	}
	return audit.History{Data: page.Data, Meta: audit.Meta{Current: p.Page, Last: max(1, (page.Total+int64(p.Size)-1)/int64(p.Size)), Total: page.Total}}, nil
}
