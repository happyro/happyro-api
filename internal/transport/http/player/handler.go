package player

import (
	"github.com/happyro/happyro-api/internal/catalog"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	gameapi "github.com/happyro/happyro-api/internal/game"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

type Handler struct {
	response.Responder
	Settings         settings.Updater
	PlayerLimits     sessionauth.PlayerLimiter
	GameAuth         *sessionauth.GameAuthentication
	PlayerAccess     *sessionauth.PlayerAccess
	PlayerCharacters *charactersapi.PlayerService
	PlayerRewards    *rewardsapi.PlayerService
	Catalog          *catalog.Service
	Maps             *catalog.Maps
	GameGateway      gameapi.Gateway
}
