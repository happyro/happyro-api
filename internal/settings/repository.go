package settings

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/jmoiron/sqlx"
)

var ErrBusy = errors.New("另一项游戏设置修改尚未完成，请先确认其结果。")

type Revision struct {
	MapInstance *string         `json:"-" db:"map_instance"`
	MapRevision *uint64         `json:"-" db:"map_revision"`
	Attempt     uint32          `json:"-" db:"command_attempt"`
	ID          string          `json:"id" db:"id"`
	Key         string          `json:"-" db:"idempotency_key"`
	Hash        string          `json:"-" db:"request_hash"`
	AdminID     *int64          `json:"requested_by" db:"requested_by"`
	GameID      *int64          `json:"requested_game_account_id" db:"requested_game_account_id"`
	Changes     json.RawMessage `json:"changes" db:"changes"`
	Previous    string          `json:"-" db:"previous_content"`
	Desired     string          `json:"-" db:"desired_content"`
	Status      string          `json:"status" db:"status"`
	ErrorCode   *string         `json:"error_code" db:"error_code"`
	Created     time.Time       `json:"created_at" db:"created_at"`
	Updated     time.Time       `json:"updated_at" db:"updated_at"`
}

const revisionColumns = "map_instance,map_revision,command_attempt,id,idempotency_key,request_hash,requested_by,requested_game_account_id,changes,previous_content,desired_content,status,error_code,created_at,updated_at"

type SQLRevisions struct{ DB *sqlx.DB }
type LockedRevisions struct{ connection *sqlx.Conn }

// Lock holds a dedicated connection so intent can commit before external effects
// while all API processes serialize updates. Persistent unfinished rows survive crashes.
func (r *SQLRevisions) Lock(ctx context.Context) (*LockedRevisions, error) {
	conn, err := r.DB.Connx(ctx)
	if err != nil {
		return nil, err
	}
	var acquired int
	if err = conn.GetContext(ctx, &acquired, "SELECT GET_LOCK('happyro-settings-publication',0)"); err != nil || acquired != 1 {
		if err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrBusy
	}
	return &LockedRevisions{connection: conn}, nil
}
func (s *LockedRevisions) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var released int
	err := s.connection.GetContext(ctx, &released, "SELECT RELEASE_LOCK('happyro-settings-publication')")
	if err != nil || released != 1 {
		// Never return a connection with an unresolved advisory lock to the pool.
		s.connection.Raw(func(any) error { return driver.ErrBadConn })
	}
	closeErr := s.connection.Close()
	if err != nil {
		return err
	}
	if released != 1 {
		return errors.New("settings lock was lost")
	}
	return closeErr
}
func (s *LockedRevisions) Find(ctx context.Context, key string) (*Revision, error) {
	var row Revision
	err := s.connection.GetContext(ctx, &row, "SELECT "+revisionColumns+" FROM settings_revisions WHERE idempotency_key=?", key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
func (s *LockedRevisions) Create(ctx context.Context, key, hash string, changes map[string]int64, previous, desired string, actor gameapi.Actor) (Revision, error) {
	if !actor.Valid() {
		return Revision{}, errors.New("invalid settings actor")
	}
	var pending int
	if err := s.connection.GetContext(ctx, &pending, "SELECT COUNT(*) FROM settings_revisions WHERE status IN ('prepared','published','uncertain')"); err != nil {
		return Revision{}, err
	}
	if pending != 0 {
		return Revision{}, ErrBusy
	}
	id, err := gameapi.NewID()
	if err != nil {
		return Revision{}, err
	}
	raw, err := json.Marshal(changes)
	if err != nil {
		return Revision{}, err
	}
	_, err = s.connection.ExecContext(ctx, `INSERT INTO settings_revisions(id,idempotency_key,request_hash,requested_by,requested_game_account_id,changes,previous_content,desired_content) VALUES(?,?,?,?,?,?,?,?)`, id, key, hash, actor.AdminUserID, actor.GameAccountID, raw, previous, desired)
	if err != nil {
		return Revision{}, err
	}
	row, err := s.Find(ctx, key)
	if err != nil {
		return Revision{}, err
	}
	return *row, nil
}
func (s *LockedRevisions) Transition(ctx context.Context, id, from, to string, code *string) error {
	allowed := map[string][]string{"prepared": {"published", "failed", "uncertain"}, "published": {"applied", "failed", "uncertain"}, "uncertain": {"applied", "failed"}}
	valid := false
	for _, next := range allowed[from] {
		if next == to {
			valid = true
		}
	}
	if !valid {
		return errors.New("invalid settings revision transition")
	}
	result, err := s.connection.ExecContext(ctx, "UPDATE settings_revisions SET status=?,error_code=? WHERE id=? AND status=?", to, code, id, from)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("settings revision state changed")
	}
	return nil
}

type History struct {
	Definitions map[string]Definition `json:"definitions"`
	Data        []Revision            `json:"data"`
	Meta        map[string]int64      `json:"meta"`
}

func (r *SQLRevisions) History(ctx context.Context, page, size int) (History, error) {
	result := History{Data: []Revision{}, Definitions: Definitions(false)}
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return result, errors.New("invalid revision pagination")
	}
	var total int64
	if err := r.DB.GetContext(ctx, &total, "SELECT COUNT(*) FROM settings_revisions"); err != nil {
		return result, err
	}
	// Do not read configuration snapshots or request hashes for a public history view.
	err := r.DB.SelectContext(ctx, &result.Data, "SELECT id,requested_by,requested_game_account_id,changes,status,error_code,created_at,updated_at FROM settings_revisions ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	last := (total + int64(size) - 1) / int64(size)
	if last < 1 {
		last = 1
	}
	result.Meta = map[string]int64{"current_page": int64(page), "last_page": last, "total": total}
	return result, err
}

// AdvanceAttempt persists a new command identity only after the previous command
// has a durable, definitive rejection. Unknown/running results never qualify.
func (s *LockedRevisions) AdvanceAttempt(ctx context.Context, id string, attempt uint32) error {
	result, err := s.connection.ExecContext(ctx, `UPDATE settings_revisions r SET map_instance=NULL,map_revision=NULL,command_attempt=command_attempt+1
 WHERE r.id=? AND r.command_attempt=? AND r.status IN ('published','uncertain')
 AND EXISTS (SELECT 1 FROM game_server_commands c WHERE c.idempotency_key=CONCAT('settings:',r.id,':',r.command_attempt) AND c.type='battle_config.apply' AND c.status='failed')`, id, attempt)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrUnconfirmed
	}
	return nil
}

// SaveAdmission fixes the process and version before any command can be sent.
// The held publication lock serializes callers; the CAS also rejects stale attempts.
func (s *LockedRevisions) SaveAdmission(ctx context.Context, id string, attempt uint32, instance string, revision uint64) error {
	result, err := s.connection.ExecContext(ctx, `UPDATE settings_revisions SET map_instance=?,map_revision=?
 WHERE id=? AND command_attempt=? AND status IN ('published','uncertain') AND map_instance IS NULL AND map_revision IS NULL`, instance, revision, id, attempt)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrUnconfirmed
	}
	return nil
}
