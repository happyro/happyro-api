package accounts

import (
	"context"
	"errors"

	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
)

// Group IDs mirror happyro-server/conf/groups.yml: Player=0, Admin=99.
const GMGroupID int64 = 99

var ErrGMPermission = errors.New("没有设置游戏 GM 的权限。")
var ErrGroupChanged = errors.New("账号权限组已变化，请刷新后重试。")

type GMUpdate struct {
	GroupID         *int64 `json:"group_id"`
	ExpectedGroupID *int64 `json:"expected_group_id"`
}
type GMWriter interface {
	SetGM(context.Context, int64, int64, int64) (database.Row, error)
}

func (s *Management) SetGM(ctx context.Context, id int64, input GMUpdate, user identityapi.User, client identityapi.Client) (database.Row, error) {
	if !user.Can("players.gm") {
		return nil, ErrGMPermission
	}
	if err := validAccountID(id); err != nil {
		return nil, err
	}
	if input.GroupID == nil || (*input.GroupID != 0 && *input.GroupID != GMGroupID) || input.ExpectedGroupID == nil || *input.ExpectedGroupID < 0 || *input.ExpectedGroupID > 255 {
		return nil, invalid("gm_group_invalid", "请选择普通玩家或 GM，并提供当前权限组。")
	}
	return s.mutate(ctx, "player.gm_changed", map[string]any{"account_id": id, "previous_group_id": *input.ExpectedGroupID, "group_id": *input.GroupID}, user, client, func() (database.Row, error) { return s.GM.SetGM(ctx, id, *input.ExpectedGroupID, *input.GroupID) })
}

func (r *SQLWriter) SetGM(ctx context.Context, id, expected, group int64) (database.Row, error) {
	conn, release, err := database.LockAccountOperation(ctx, r.DB, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = checkPlayerAccount(ctx, conn, id); err != nil {
		return nil, err
	}
	var deleting int
	if err = conn.GetContext(ctx, &deleting, "SELECT COUNT(*) FROM happyro_admin_account_deletions WHERE account_id=?", id); err != nil {
		return nil, err
	}
	if deleting != 0 {
		return nil, ErrAccountDeleting
	}
	var current int64
	if err = conn.GetContext(ctx, &current, "SELECT group_id FROM login WHERE account_id=?", id); err != nil {
		return nil, err
	}
	if current != expected {
		return nil, ErrGroupChanged
	}
	// The login server loads this field at authentication. Existing game sessions
	// keep their group until a full logout/login; never mutate live map state here.
	if current != group {
		result, err := conn.ExecContext(ctx, "UPDATE login SET group_id=? WHERE account_id=? AND group_id=? AND sex<>'S'", group, id, expected)
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, ErrGroupChanged
		}
	}
	rows, err := database.Rows(ctx, conn, "SELECT "+accountProjection+" FROM login WHERE account_id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrAccountNotFound
	}
	return rows[0], nil
}
