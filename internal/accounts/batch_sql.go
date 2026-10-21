package accounts

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jmoiron/sqlx"
)

type SQLBatches struct{ DB *sqlx.DB }

func (r *SQLBatches) Prepare(ctx context.Context, input BatchInput, hash string, actor int64) (BatchRecord, error) {
	ids, err := json.Marshal(input.IDs)
	if err != nil {
		return BatchRecord{}, err
	}
	if _, err = r.DB.ExecContext(ctx, `INSERT INTO player_batches (idempotency_key,request_hash,actor_id,action,account_ids) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE idempotency_key=VALUES(idempotency_key)`, input.Key, hash, actor, input.Action, ids); err != nil {
		return BatchRecord{}, err
	}
	var row struct {
		ID     int64  `db:"id"`
		Hash   string `db:"request_hash"`
		Result []byte `db:"result"`
	}
	if err = r.DB.GetContext(ctx, &row, "SELECT id,request_hash,result FROM player_batches WHERE idempotency_key=?", input.Key); err != nil {
		return BatchRecord{}, err
	}
	record := BatchRecord{ID: row.ID, Hash: row.Hash}
	if len(row.Result) > 0 {
		record.Result = &BatchResult{}
		if err = json.Unmarshal(row.Result, record.Result); err != nil {
			return BatchRecord{}, err
		}
	}
	return record, nil
}
func (r *SQLBatches) Complete(ctx context.Context, id int64, result BatchResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = r.DB.ExecContext(ctx, "UPDATE player_batches SET result=?,completed_at=CURRENT_TIMESTAMP(6) WHERE id=? AND result IS NULL", raw, id)
	return err
}
func (r *SQLWriter) Eligible(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, errors.New("empty account ids")
	}
	query, args, err := sqlx.In("SELECT account_id,sex FROM login WHERE account_id IN (?) ORDER BY account_id", ids)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID  int64  `db:"account_id"`
		Sex string `db:"sex"`
	}
	if err = r.DB.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	result := []int64{}
	for _, row := range rows {
		if row.Sex == "S" {
			return nil, ErrProtected
		}
		result = append(result, row.ID)
	}
	return result, nil
}
func (r *SQLWriter) SetState(ctx context.Context, id, state int64) error {
	_, err := r.Update(ctx, id, AccountUpdate{State: &state})
	return err
}
