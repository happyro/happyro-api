package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type Event struct {
	OperationKey                    string
	ActorID                         *int64
	Username, Action, IP, UserAgent string
	Metadata                        map[string]any
}
type Writer interface {
	Write(context.Context, Event) error
}
type HistoryReader interface {
	History(context.Context, string, database.Pagination) (History, error)
}
type History struct {
	Data []database.Row `json:"data"`
	Meta Meta           `json:"meta"`
}
type Meta struct {
	Current int   `json:"current_page"`
	Last    int64 `json:"last_page"`
	Total   int64 `json:"total"`
}
type SQLStore struct{ DB *sqlx.DB }

func (s *SQLStore) Write(ctx context.Context, event Event) error {
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	agent := []rune(event.UserAgent)
	if len(agent) > 1024 {
		agent = agent[:1024]
	}
	var key any
	if event.OperationKey != "" {
		key = event.OperationKey
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO audit_logs (actor_id,username,action,ip_address,user_agent,metadata,operation_key) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE operation_key=VALUES(operation_key)`, event.ActorID, event.Username, event.Action, event.IP, string(agent), raw, key)
	return err
}
func (s *SQLStore) History(ctx context.Context, action string, p database.Pagination) (History, error) {
	if !p.Valid() {
		return History{}, errors.New("invalid history pagination")
	}
	page, err := database.Paginate(ctx, s.DB, "a.id,a.actor_id AS user_id,a.username,a.action AS event,a.ip_address,a.user_agent,a.metadata,CAST(a.created_at AS CHAR) AS created_at,u.name AS actor_name,u.username AS actor_username", "audit_logs a LEFT JOIN users u ON u.id=a.actor_id", " WHERE a.action=?", "a.id DESC", []any{action}, p)
	if err != nil {
		return History{}, err
	}
	for _, row := range page.Data {
		var metadata any
		raw, ok := row["metadata"].(string)
		if !ok {
			return History{}, errors.New("invalid audit metadata")
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&metadata); err != nil {
			return History{}, err
		}
		row["metadata"] = metadata
		row["user"] = nil
		if row["actor_username"] != nil {
			row["user"] = map[string]any{"id": row["user_id"], "name": row["actor_name"], "username": row["actor_username"]}
		}
		delete(row, "actor_name")
		delete(row, "actor_username")
	}
	return History{Data: page.Data, Meta: Meta{Current: p.Page, Last: max(1, (page.Total+int64(p.Size)-1)/int64(p.Size)), Total: page.Total}}, nil
}
