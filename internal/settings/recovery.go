package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

var ErrRevisionNotFound = errors.New("游戏设置修订不存在。")

type RevisionReader interface {
	ByID(context.Context, string) (Revision, error)
}
type RevisionResumer interface {
	Resume(context.Context, Update, gameapi.Actor) (Revision, error)
}
type Recovery struct {
	Revisions RevisionReader
	Settings  RevisionResumer
	Audit     audit.Writer
}

func (r *SQLRevisions) ByID(ctx context.Context, id string) (Revision, error) {
	var row Revision
	err := r.DB.GetContext(ctx, &row, "SELECT "+revisionColumns+" FROM settings_revisions WHERE id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrRevisionNotFound
	}
	return row, err
}

// Resume uses the original actor and request identity. Recovery authority is an
// administrator permission; it never grants access to other players' sessions.
func (s *Recovery) Resume(ctx context.Context, id string, user identityapi.User, client identityapi.Client) (Revision, error) {
	if !user.Can("settings.manage") {
		return Revision{}, errors.New("settings recovery requires settings.manage")
	}
	row, err := s.Revisions.ByID(ctx, id)
	if err != nil {
		return row, err
	}
	var changes map[string]int64
	if err = json.Unmarshal(row.Changes, &changes); err != nil {
		return row, err
	}
	if err = s.Audit.Write(ctx, audit.Event{ActorID: &user.ID, Username: user.Username, Action: "settings.resume_requested", IP: client.IP, UserAgent: client.UserAgent, Metadata: map[string]any{"revision_id": id}}); err != nil {
		return row, err
	}
	return s.Settings.Resume(ctx, Update{Key: row.Key, Changes: changes}, gameapi.Actor{AdminUserID: row.AdminID, GameAccountID: row.GameID})
}
