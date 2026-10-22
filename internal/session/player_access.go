package session

import (
	"context"
	"errors"
)

var ErrPlayerForbidden = errors.New("当前游戏设置不允许使用此功能。")

const CharacterMaintenancePolicy = "game_tools_character_maintenance_policy"
const RewardPolicy = "game_tools_item_grant_policy"

type PolicySource interface {
	BattleConfig(context.Context) (map[string]int64, error)
}
type PlayerAccess struct{ Gateway PolicySource }

func Allows(policy int64, principal GamePrincipal) bool {
	return policy == 2 || policy == 1 && principal.IsAdmin
}
func (s *PlayerAccess) Authorize(ctx context.Context, policy string, principal GamePrincipal) error {
	values, err := s.Gateway.BattleConfig(ctx)
	if err != nil {
		return err
	}
	// Missing policy is unavailable, never implicitly open to every player.
	if !Allows(values[policy], principal) {
		return ErrPlayerForbidden
	}
	return nil
}
func (s *PlayerAccess) Bootstrap(ctx context.Context, principal GamePrincipal) (map[string]bool, error) {
	values, err := s.Gateway.BattleConfig(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]bool{
		"administrator":               principal.IsAdmin,
		"characterMaintenanceAllowed": Allows(values[CharacterMaintenancePolicy], principal),
		"gameSettingsAllowed":         Allows(values["game_tools_game_settings_policy"], principal),
		"itemGrantAllowed":            Allows(values["game_tools_item_grant_policy"], principal),
	}, nil
}
