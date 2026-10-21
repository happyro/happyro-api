package game

import (
	"context"
	"errors"

	"github.com/happyro/happyro-api/internal/platform/database"
)

type CharacterReader interface {
	Character(context.Context, int64) (database.Row, error)
}
type Snapshots struct {
	Gateway    Gateway
	Characters CharacterReader
}

func (s *Snapshots) Read(ctx context.Context, id int64) (map[string]any, error) {
	snapshot, err := s.Gateway.CharacterSnapshot(ctx, id)
	if err == nil {
		snapshot["online"] = true
		return snapshot, nil
	}
	var failure *GatewayError
	if !errors.As(err, &failure) || failure.Code != "character_offline" {
		return nil, err
	}
	row, err := s.Characters.Character(ctx, id)
	if err != nil {
		return nil, err
	}
	row["online"] = false
	for target, source := range map[string]string{"job_id": "class", "status_points": "status_point", "skill_points": "skill_point", "map": "last_map", "x": "last_x", "y": "last_y"} {
		row[target] = row[source]
	}
	return row, nil
}
