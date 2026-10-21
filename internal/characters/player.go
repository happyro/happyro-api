package characters

import (
	"context"
	"errors"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"slices"
	"strconv"

	gameapi "github.com/happyro/happyro-api/internal/game"
)

type Commands interface {
	Submit(context.Context, gameapi.Request, gameapi.Actor) (gameapi.Command, bool, error)
}
type PlayerService struct {
	Access   *sessionauth.PlayerAccess
	Gateway  gameapi.Gateway
	Commands Commands
}
type Maintenance struct {
	Key     string         `json:"idempotency_key"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func (s *PlayerService) Maintain(ctx context.Context, principal sessionauth.GamePrincipal, input Maintenance) (map[string]any, error) {
	if err := s.Access.Authorize(ctx, sessionauth.CharacterMaintenancePolicy, principal); err != nil {
		return nil, err
	}
	allowed := []string{"character.progression.update", "character.points.update", "character.skill_points.update", "character.stats.update", "character.stats.reset", "character.traits.update", "character.traits.reset", "character.skills.reset", "character.skills.learn_all", "character.appearance.update", "character.riding.update", "character.vitals.restore"}
	if !slices.Contains(allowed, input.Type) {
		return nil, &gameapi.ValidationError{Field: "type", Message: "角色维护操作无效。"}
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	request := gameapi.Request{IdempotencyKey: input.Key, Type: input.Type, Target: &gameapi.Target{Type: "character", ID: strconv.FormatInt(principal.CharacterID, 10)}, Payload: input.Payload}
	if err := gameapi.ValidateOperator(request); err != nil {
		return nil, err
	}
	if _, _, err := s.Commands.Submit(ctx, request, gameapi.Actor{GameAccountID: &principal.AccountID}); err != nil {
		return nil, err
	}
	return s.Gateway.CharacterSnapshot(ctx, principal.CharacterID)
}

func (s *PlayerService) Character(ctx context.Context, principal sessionauth.GamePrincipal) (map[string]any, error) {
	if err := s.Access.Authorize(ctx, sessionauth.CharacterMaintenancePolicy, principal); err != nil {
		return nil, err
	}
	return s.Gateway.CharacterSnapshot(ctx, principal.CharacterID)
}
func (s *PlayerService) AllocationLimits(ctx context.Context, principal sessionauth.GamePrincipal) (any, error) {
	snapshot, err := s.Gateway.CharacterSnapshot(ctx, principal.CharacterID)
	if err != nil {
		return nil, err
	}
	limits, ok := snapshot["allocation_limits"]
	if !ok || limits == nil {
		return nil, errors.New("game snapshot lacks allocation limits")
	}
	return limits, nil
}
