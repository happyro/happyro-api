package settings

import (
	"context"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

type Updater interface {
	Apply(context.Context, Update, gameapi.Actor) (Revision, error)
}

type HistoryReader interface {
	History(context.Context, int, int) (History, error)
}

type RecoveryRunner interface {
	Resume(context.Context, string, identityapi.User, identityapi.Client) (Revision, error)
}
