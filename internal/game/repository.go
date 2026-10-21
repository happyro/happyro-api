package game

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

type SQLRepository struct{ DB *sqlx.DB }

type commandRow struct {
	ID           string     `db:"id"`
	Key          string     `db:"idempotency_key"`
	Hash         string     `db:"request_hash"`
	Type         string     `db:"type"`
	Status       string     `db:"status"`
	TargetType   *string    `db:"target_type"`
	TargetID     *string    `db:"target_id"`
	Payload      []byte     `db:"payload"`
	Result       []byte     `db:"result"`
	ErrorCode    *string    `db:"error_code"`
	ErrorMessage *string    `db:"error_message"`
	AdminID      *int64     `db:"requested_by"`
	GameID       *int64     `db:"requested_game_account_id"`
	Started      *time.Time `db:"started_at"`
	Completed    *time.Time `db:"completed_at"`
	Deadline     *time.Time `db:"deadline"`
	Created      time.Time  `db:"created_at"`
	Updated      time.Time  `db:"updated_at"`
}

func (r commandRow) command() (Command, error) {
	command := Command{ID: r.ID, IdempotencyKey: r.Key, Type: r.Type, Status: r.Status, TargetType: r.TargetType, TargetID: r.TargetID, ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, RequestedBy: r.AdminID, RequestedGameAccountID: r.GameID, StartedAt: r.Started, CompletedAt: r.Completed, CreatedAt: r.Created, UpdatedAt: r.Updated}
	decode := func(raw []byte, target any) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		return decoder.Decode(target)
	}
	if err := decode(r.Payload, &command.Payload); err != nil {
		return command, err
	}
	if len(r.Result) > 0 {
		if err := decode(r.Result, &command.Result); err != nil {
			return command, err
		}
	}
	return command, nil
}

func (r *SQLRepository) Submit(ctx context.Context, request Request, actor Actor) (Command, bool, error) {
	hash, err := request.Fingerprint(actor)
	if err != nil {
		return Command{}, false, err
	}
	id, err := NewID()
	if err != nil {
		return Command{}, false, err
	}
	payload, err := json.Marshal(request.Payload)
	if err != nil {
		return Command{}, false, err
	}
	var targetType, targetID any
	if request.Target != nil {
		targetType, targetID = request.Target.Type, request.Target.ID
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO game_server_commands
 (id,idempotency_key,request_hash,type,status,target_type,target_id,payload,requested_by,requested_game_account_id)
 VALUES (?,?,?,?,'pending',?,?,?,?,?)`, id, request.IdempotencyKey, hash, request.Type, targetType, targetID, payload, actor.AdminUserID, actor.GameAccountID)
	created := err == nil
	if err != nil {
		var duplicate *mysql.MySQLError
		if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
			return Command{}, false, err
		}
	}
	var row commandRow
	if err = r.DB.GetContext(ctx, &row, "SELECT * FROM game_server_commands WHERE idempotency_key=?", request.IdempotencyKey); err != nil {
		return Command{}, false, err
	}
	if row.Hash != hash {
		return Command{}, false, ErrConflict
	}
	// Find also resolves expired running commands after a process crash.
	command, err := r.Find(ctx, row.ID)
	return command, created, err
}

func (r *SQLRepository) Find(ctx context.Context, id string) (Command, error) {
	_, err := r.DB.ExecContext(ctx, `UPDATE game_server_commands SET status='indeterminate',
 error_code='execution_interrupted',error_message='执行中断，无法确认游戏命令结果。',completed_at=UTC_TIMESTAMP(6)
 WHERE id=? AND status='running' AND deadline<UTC_TIMESTAMP(6)`, id)
	if err != nil {
		return Command{}, err
	}
	var row commandRow
	err = r.DB.GetContext(ctx, &row, "SELECT * FROM game_server_commands WHERE id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, err
	}
	return row.command()
}

func (r *SQLRepository) Claim(ctx context.Context, id string, lease time.Duration) (Command, error) {
	result, err := r.DB.ExecContext(ctx, `UPDATE game_server_commands SET status='running',
 started_at=UTC_TIMESTAMP(6),deadline=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))
 WHERE id=? AND status='pending'`, lease.Microseconds(), id)
	if err != nil {
		return Command{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Command{}, err
	}
	if count != 1 {
		return Command{}, ErrRunning
	}
	return r.Find(ctx, id)
}

func (r *SQLRepository) Finish(ctx context.Context, id, status string, result map[string]any, failure *GatewayError) (Command, error) {
	if status != "succeeded" && status != "failed" && status != "indeterminate" {
		return Command{}, errors.New("invalid terminal command state")
	}
	var raw any
	if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return Command{}, err
		}
		raw = encoded
	}
	var code, message any
	if failure != nil {
		code, message = failure.Code, failure.Message
	}
	changed, err := r.DB.ExecContext(ctx, `UPDATE game_server_commands SET status=?,result=?,error_code=?,error_message=?,completed_at=UTC_TIMESTAMP(6)
 WHERE id=? AND status IN ('running','indeterminate')`, status, raw, code, message, id)
	if err != nil {
		return Command{}, err
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return Command{}, err
	}
	if count != 1 {
		return Command{}, ErrRunning
	}
	return r.Find(ctx, id)
}
