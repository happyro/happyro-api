package settings

import (
	"context"
	"math"

	gameapi "github.com/happyro/happyro-api/internal/game"
)

func (s *Service) reconcile(ctx context.Context, row *Revision, command gameapi.Command) (gameapi.Command, error) {
	state, err := s.Gateway.SettingsState(ctx)
	if err != nil {
		return command, err
	}
	if state.InstanceID == *row.MapInstance && state.Revision == *row.MapRevision {
		// Both the original request and this barrier carry the same CAS. Exactly one
		// can win on the map event loop. Always read back, including after a timeout.
		fenceErr := s.Gateway.FenceSettings(ctx, *row.MapInstance, *row.MapRevision)
		state, err = s.Gateway.SettingsState(ctx)
		if err != nil {
			return command, err
		}
		if state.InstanceID == *row.MapInstance && state.Revision == *row.MapRevision {
			if fenceErr != nil {
				return command, fenceErr
			}
			return command, ErrUnconfirmed
		}
	}
	if state.InstanceID != *row.MapInstance {
		// The old process cannot accept its request anymore. A new attempt synchronizes
		// the restarted map with the already published configuration.
		return s.Commands.ResolveSettings(ctx, command.ID, false)
	}
	if *row.MapRevision == math.MaxUint64 || state.Revision != *row.MapRevision+1 {
		return command, ErrUnconfirmed
	}
	if state.LastCommandID == command.ID {
		switch state.LastStatus {
		case "succeeded":
			return s.Commands.ResolveSettings(ctx, command.ID, true)
		case "failed":
			return s.Commands.ResolveSettings(ctx, command.ID, false)
		}
	}
	if state.LastStatus == "fenced" && state.LastCommandID == "" {
		return s.Commands.ResolveSettings(ctx, command.ID, false)
	}
	return command, ErrUnconfirmed
}
