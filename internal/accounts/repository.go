package accounts

import (
	"context"
	"strings"

	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type SQLReader struct{ Game, Log *sqlx.DB }

// Escape SQL LIKE metacharacters so names containing '%' or '_' are searched
// literally. The explicit escape character also works under NO_BACKSLASH_ESCAPES.
func search(value string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
}

func (r *SQLReader) Accounts(ctx context.Context, q AccountQuery) (database.Page, error) {
	where := " WHERE 1=1"
	args := []any{}
	if q.Username != "" {
		where += " AND userid LIKE ? ESCAPE '!'"
		args = append(args, search(q.Username))
	}
	if q.Email != "" {
		where += " AND email LIKE ? ESCAPE '!'"
		args = append(args, search(q.Email))
	}
	if q.State == "0" {
		where += " AND state=0"
	} else if q.State == "1" {
		where += " AND state>0"
	}
	return database.Paginate(ctx, r.Game, "account_id,userid AS username,email,sex,group_id,state,DATE_FORMAT(lastlogin,'%Y-%m-%dT%H:%i:%s.%fZ') AS lastlogin,last_ip", "login", where, "account_id DESC", args, q.Pagination)
}

func (r *SQLReader) LoginLogs(ctx context.Context, q LoginLogQuery) (database.Page, error) {
	where := ""
	args := []any{}
	if q.Username != "" {
		where = " WHERE `user` LIKE ? ESCAPE '!'"
		args = append(args, search(q.Username))
	}
	return database.Paginate(ctx, r.Log, "DATE_FORMAT(time,'%Y-%m-%dT%H:%i:%s.%fZ') AS time,ip,`user` AS username,rcode,log", "loginlog", where, "time DESC", args, q.Pagination)
}
