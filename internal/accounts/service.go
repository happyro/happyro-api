package accounts

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/platform/database"
)

var ErrFilter = errors.New("查询参数无效。")

type AccountQuery struct {
	Username, Email, State string
	Pagination             database.Pagination
}
type LoginLogQuery struct {
	Username   string
	Pagination database.Pagination
}

type Reader interface {
	Accounts(context.Context, AccountQuery) (database.Page, error)
	LoginLogs(context.Context, LoginLogQuery) (database.Page, error)
}

type Service struct {
	Reader Reader
}

func validFilter(value string) bool { return len(value) <= 256 && utf8.ValidString(value) }
func (s *Service) Accounts(ctx context.Context, q AccountQuery) (database.Page, error) {
	if !q.Pagination.Valid() || !validFilter(q.Username) || !validFilter(q.Email) || (q.State != "" && q.State != "0" && q.State != "1") {
		return database.Page{}, ErrFilter
	}
	return s.Reader.Accounts(ctx, q)
}
func (s *Service) LoginLogs(ctx context.Context, q LoginLogQuery) (database.Page, error) {
	if !q.Pagination.Valid() || !validFilter(q.Username) {
		return database.Page{}, ErrFilter
	}
	return s.Reader.LoginLogs(ctx, q)
}
