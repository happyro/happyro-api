package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

type BatchInput struct {
	Key    string  `json:"idempotency_key"`
	Action string  `json:"action"`
	IDs    []int64 `json:"account_ids"`
}
type BatchResult struct {
	Status      string  `json:"status"`
	Count       int     `json:"count"`
	DeletionIDs []int64 `json:"deletion_ids,omitempty"`
}
type BatchRecord struct {
	ID     int64
	Hash   string
	Result *BatchResult
}
type BatchRecords interface {
	Prepare(context.Context, BatchInput, string, int64) (BatchRecord, error)
	Complete(context.Context, int64, BatchResult) error
}
type BatchAccounts interface {
	Eligible(context.Context, []int64) ([]int64, error)
	SetState(context.Context, int64, int64) error
}
type BatchCommands interface {
	Submit(context.Context, gameapi.Request, gameapi.Actor) (gameapi.Command, bool, error)
}
type BatchDeletions interface {
	Request(context.Context, int64, identityapi.User, identityapi.Client) (Deletion, error)
}
type Batches struct {
	Deletions BatchDeletions
	Records   BatchRecords
	Accounts  BatchAccounts
	Commands  BatchCommands
	Audit     audit.Writer
}

func normalizedBatch(input BatchInput) (BatchInput, error) {
	if len(input.Key) < 1 || len(input.Key) > 64 || !utf8.ValidString(input.Key) || strings.TrimSpace(input.Key) != input.Key || strings.ContainsRune(input.Key, 0) {
		return input, invalid("invalid_request_key", "请求标识必须包含 1 至 64 个字节。")
	}
	if !slices.Contains([]string{"ban", "unban", "kick", "delete"}, input.Action) {
		return input, invalid("invalid_action", "批量操作无效。")
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		return input, invalid("invalid_account_ids", "每次请选择 1 至 100 个账号。")
	}
	input.IDs = slices.Clone(input.IDs)
	for _, id := range input.IDs {
		if err := validAccountID(id); err != nil {
			return input, invalid("invalid_account_ids", "账号编号无效。")
		}
	}
	slices.Sort(input.IDs)
	input.IDs = slices.Compact(input.IDs)
	return input, nil
}
func (s *Batches) Run(ctx context.Context, input BatchInput, user identityapi.User, client identityapi.Client) (BatchResult, error) {
	input, err := normalizedBatch(input)
	if err != nil {
		return BatchResult{}, err
	}
	if user.ID <= 0 {
		return BatchResult{}, errors.New("invalid batch actor")
	}
	raw, _ := json.Marshal(struct {
		Input BatchInput
		Actor int64
	}{input, user.ID})
	digest := sha256.Sum256(raw)
	record, err := s.Records.Prepare(ctx, input, hex.EncodeToString(digest[:]), user.ID)
	if err != nil {
		return BatchResult{}, err
	}
	if record.Hash != hex.EncodeToString(digest[:]) {
		return BatchResult{}, gameapi.ErrConflict
	}
	if record.Result != nil {
		return *record.Result, nil
	}
	ids, err := s.Accounts.Eligible(ctx, input.IDs)
	if err != nil {
		return BatchResult{}, err
	}
	event := audit.Event{OperationKey: fmt.Sprintf("player-batch:%d:requested", record.ID), ActorID: &user.ID, Username: user.Username, Action: "player.batch_" + input.Action + ".requested", IP: client.IP, UserAgent: client.UserAgent, Metadata: map[string]any{"batch_id": record.ID, "account_ids": ids}}
	if err = s.Audit.Write(ctx, event); err != nil {
		return BatchResult{}, err
	}
	result := BatchResult{Status: "ok"}
	if input.Action == "delete" {
		// Include completed tombstones on a partial retry, even after login is gone.
		ids = input.IDs
		result.Status = "accepted"
	}
	for _, id := range ids {
		if input.Action == "delete" {
			if _, err = s.Deletions.Request(ctx, id, user, client); err != nil {
				return BatchResult{}, err
			}
			result.DeletionIDs = append(result.DeletionIDs, id)
			result.Count++
			continue
		}
		if input.Action != "kick" {
			state := int64(0)
			if input.Action == "ban" {
				state = 1
			}
			if err = s.Accounts.SetState(ctx, id, state); err != nil {
				return BatchResult{}, err
			}
		}
		if input.Action != "unban" {
			command, _, err := s.Commands.Submit(ctx, gameapi.Request{IdempotencyKey: fmt.Sprintf("player-batch:%d:%d", record.ID, id), Type: "account.disconnect", Target: &gameapi.Target{Type: "account", ID: strconv.FormatInt(id, 10)}, Payload: map[string]any{}}, gameapi.Actor{AdminUserID: &user.ID})
			if err != nil {
				return BatchResult{}, err
			}
			returnedID, idOK := command.Result["account_id"].(json.Number)
			if !idOK || returnedID.String() != strconv.FormatInt(id, 10) {
				return BatchResult{}, errors.New("disconnect response account mismatch")
			}
			disconnected, ok := command.Result["disconnected"].(bool)
			if !ok {
				return BatchResult{}, errors.New("invalid disconnect response")
			}
			if input.Action == "kick" && !disconnected {
				continue
			}
		}
		result.Count++
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	event.OperationKey = fmt.Sprintf("player-batch:%d:result", record.ID)
	event.Action = "player.batch_" + input.Action
	event.Metadata["count"] = result.Count
	if err = s.Audit.Write(persist, event); err != nil {
		return BatchResult{}, err
	}
	if err = s.Records.Complete(persist, record.ID, result); err != nil {
		return BatchResult{}, err
	}
	return result, nil
}
