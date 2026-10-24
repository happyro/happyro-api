package admin

import (
	"github.com/alexedwards/scs/v2"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	"github.com/happyro/happyro-api/internal/config"
	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/identity"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

type Handler struct {
	response.Responder
	SettingsRecovery  settings.RecoveryRunner
	Settings          settings.Updater
	SettingsHistory   settings.HistoryReader
	AccountDeletions  *accountsapi.Deletions
	AccountBatches    *accountsapi.Batches
	AccountManagement *accountsapi.Management
	Grants            *rewardsapi.Service
	GrantLookup       *rewardsapi.Lookup
	GrantHistory      rewardsapi.RecordHistory
	Audit             audit.HistoryReader
	Config            config.Config
	Catalog           *catalog.Service
	Passwords         *identity.SelfPasswordService
	Auth              *sessionauth.AdminService
	Accounts          *accountsapi.Service
	Characters        *charactersapi.Service
	Maps              *catalog.Maps
	GameGateway       gameapi.Gateway
	GameCommands      *gameapi.Service
	Snapshots         *gameapi.Snapshots
	Sessions          *scs.SessionManager
}
