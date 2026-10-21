package session

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"slices"

	"github.com/jmoiron/sqlx"
)

var ErrGameSession = errors.New("游戏会话无效。")

type GamePrincipal struct {
	AccountID, CharacterID int64
	GroupID                int
	IsAdmin                bool
}
type GameIdentity struct {
	Token   string `db:"web_auth_token"`
	GroupID int    `db:"group_id"`
}
type GameIdentityRepository interface {
	OnlineIdentity(context.Context, int64, int64) (GameIdentity, error)
}
type GameAuthentication struct {
	Repository  GameIdentityRepository
	AdminGroups []int
}

func (s *GameAuthentication) Authenticate(ctx context.Context, accountID, characterID int64, token string) (GamePrincipal, error) {
	if accountID <= 0 || characterID <= 0 || token == "" || len(token) > 64 {
		return GamePrincipal{}, ErrGameSession
	}
	identity, err := s.Repository.OnlineIdentity(ctx, accountID, characterID)
	if err != nil {
		return GamePrincipal{}, err
	}
	if identity.Token == "" || subtle.ConstantTimeCompare([]byte(identity.Token), []byte(token)) != 1 {
		return GamePrincipal{}, ErrGameSession
	}
	return GamePrincipal{AccountID: accountID, CharacterID: characterID, GroupID: identity.GroupID, IsAdmin: slices.Contains(s.AdminGroups, identity.GroupID)}, nil
}

type SQLGameIdentity struct{ DB *sqlx.DB }

func (r *SQLGameIdentity) OnlineIdentity(ctx context.Context, accountID, characterID int64) (GameIdentity, error) {
	var identity GameIdentity
	// Observe ownership, access restrictions and deletion intent in one statement.
	// A queued or failed deletion remains fenced even before the worker disables tokens.
	err := r.DB.GetContext(ctx, &identity, `SELECT COALESCE(a.web_auth_token,'') AS web_auth_token,a.group_id FROM login a
 JOIN `+"`char`"+` c ON c.account_id=a.account_id
 WHERE a.account_id=? AND c.char_id=? AND c.online=1 AND a.web_auth_token_enabled=1
 AND a.state=0 AND a.unban_time<=UNIX_TIMESTAMP()
 AND (a.expiration_time=0 OR a.expiration_time>=UNIX_TIMESTAMP())
 AND NOT EXISTS (SELECT 1 FROM happyro_admin_account_deletions d WHERE d.account_id=a.account_id)`, accountID, characterID)
	if errors.Is(err, sql.ErrNoRows) {
		return identity, ErrGameSession
	}
	return identity, err
}
