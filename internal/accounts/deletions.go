package accounts

import (
	"context"
	"errors"
	"github.com/happyro/happyro-api/internal/platform/database"

	"fmt"
	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

var ErrDeletionWorker = errors.New("账号清理服务暂时不可用，请稍后重试。")
var ErrDeletionState = errors.New("只有失败的账号删除任务可以重试。")

type Deletion struct {
	AccountID   int64   `json:"account_id" db:"account_id"`
	AccountName string  `json:"account_name" db:"account_name"`
	Status      string  `json:"status" db:"status"`
	ErrorCode   *string `json:"error_code" db:"error_code"`
	Total       int     `json:"total_characters" db:"total_characters"`
	Deleted     int     `json:"deleted_characters" db:"deleted_characters"`
	Created     string  `json:"created_at" db:"created_at"`
	Updated     string  `json:"updated_at" db:"updated_at"`
}
type DeletionPage struct {
	Data  []Deletion `json:"data"`
	Total int64      `json:"total"`
}
type DeletionStore interface {
	List(context.Context, database.Pagination) (DeletionPage, error)
	Find(context.Context, int64) (*Deletion, error)
	Ready(context.Context) error
	Enqueue(context.Context, int64, identityapi.User) (Deletion, error)
	Retry(context.Context, int64) (Deletion, error)
}
type Deletions struct {
	Store    DeletionStore
	Accounts BatchAccounts
	Audit    audit.Writer
}

func (s *Deletions) Request(ctx context.Context, id int64, user identityapi.User, client identityapi.Client) (Deletion, error) {
	if err := validAccountID(id); err != nil {
		return Deletion{}, err
	}
	if user.ID <= 0 {
		return Deletion{}, errors.New("invalid deletion actor")
	}
	existing, err := s.Store.Find(ctx, id)
	if err != nil {
		return Deletion{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	ids, err := s.Accounts.Eligible(ctx, []int64{id})
	if err != nil {
		return Deletion{}, err
	}
	if len(ids) != 1 {
		return Deletion{}, ErrAccountNotFound
	}
	if err = s.Store.Ready(ctx); err != nil {
		return Deletion{}, err
	}
	if err = s.Audit.Write(ctx, audit.Event{OperationKey: fmt.Sprintf("account-deletion:%d:requested", id), ActorID: &user.ID, Username: user.Username, Action: "player.deletion_requested", IP: client.IP, UserAgent: client.UserAgent, Metadata: map[string]any{"account_id": id}}); err != nil {
		return Deletion{}, err
	}
	return s.Store.Enqueue(ctx, id, user)
}
func (s *Deletions) Status(ctx context.Context, id int64) (Deletion, error) {
	if err := validAccountID(id); err != nil {
		return Deletion{}, err
	}
	row, err := s.Store.Find(ctx, id)
	if err != nil {
		return Deletion{}, err
	}
	if row == nil {
		return Deletion{}, ErrAccountNotFound
	}
	return *row, nil
}
func (s *Deletions) Retry(ctx context.Context, id int64, user identityapi.User, client identityapi.Client) (Deletion, error) {
	if err := validAccountID(id); err != nil {
		return Deletion{}, err
	}
	if user.ID <= 0 {
		return Deletion{}, errors.New("invalid deletion actor")
	}
	if err := s.Store.Ready(ctx); err != nil {
		return Deletion{}, err
	}
	if err := s.Audit.Write(ctx, audit.Event{ActorID: &user.ID, Username: user.Username, Action: "player.deletion_retry_requested", IP: client.IP, UserAgent: client.UserAgent, Metadata: map[string]any{"account_id": id}}); err != nil {
		return Deletion{}, err
	}
	return s.Store.Retry(ctx, id)
}

func (s *Deletions) List(ctx context.Context, page database.Pagination) (DeletionPage, error) {
	if !page.Valid() {
		return DeletionPage{}, ErrFilter
	}
	return s.Store.List(ctx, page)
}
