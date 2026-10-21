package rewards

import (
	"context"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	sessionauth "github.com/happyro/happyro-api/internal/session"
)

type PlayerService struct {
	Access  *sessionauth.PlayerAccess
	Rewards Rewards
}

type Rewards interface {
	Inventory(context.Context, ItemRequest, gameapi.Actor, identityapi.Client) (map[string]any, error)
	Zeny(context.Context, ZenyRequest, gameapi.Actor, identityapi.Client) (map[string]any, error)
}
type ItemGrant struct {
	Key    string `json:"idempotency_key"`
	Target struct {
		Type string `json:"type"`
	} `json:"target"`
	ItemID int64 `json:"item_id"`
	Amount int64 `json:"amount"`
}
type ZenyGrant struct {
	Key    string `json:"idempotency_key"`
	Amount int64  `json:"amount"`
}

func (s *PlayerService) GrantItem(ctx context.Context, p sessionauth.GamePrincipal, input ItemGrant, client identityapi.Client) (map[string]any, error) {
	if err := s.Access.Authorize(ctx, sessionauth.RewardPolicy, p); err != nil {
		return nil, err
	}
	if input.Target.Type != "self" {
		return nil, &ValidationError{Message: "只能向当前角色发放物品。"}
	}
	return s.Rewards.Inventory(ctx, ItemRequest{Key: input.Key, CharacterID: p.CharacterID, ItemID: input.ItemID, Amount: input.Amount}, gameapi.Actor{GameAccountID: &p.AccountID}, client)
}
func (s *PlayerService) GrantZeny(ctx context.Context, p sessionauth.GamePrincipal, input ZenyGrant, client identityapi.Client) (map[string]any, error) {
	if err := s.Access.Authorize(ctx, sessionauth.RewardPolicy, p); err != nil {
		return nil, err
	}
	return s.Rewards.Zeny(ctx, ZenyRequest{Key: input.Key, CharacterID: p.CharacterID, Amount: input.Amount}, gameapi.Actor{GameAccountID: &p.AccountID}, client)
}
