package game

import (
	"context"
	"errors"
	"testing"

	"github.com/happyro/happyro-api/internal/platform/database"
)

type snapshotGateway struct {
	Gateway
	failure error
}

func (g snapshotGateway) CharacterSnapshot(context.Context, int64) (map[string]any, error) {
	if g.failure != nil {
		return nil, g.failure
	}
	return map[string]any{"char_id": int64(150000), "hp": 100}, nil
}

type characterFixture struct{ calls int }

func (r *characterFixture) Character(context.Context, int64) (database.Row, error) {
	r.calls++
	return database.Row{"char_id": int64(150000), "class": 7, "status_point": 12, "skill_point": 3, "last_map": "prontera", "last_x": 100, "last_y": 101}, nil
}
func TestSnapshotsUseStoredCharacterOnlyWhenExplicitlyOffline(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failure  error
		database bool
	}{
		{"online", nil, false},
		{"offline", &GatewayError{Code: "character_offline"}, true},
		{"unavailable", &GatewayError{Code: "unavailable"}, false},
		{"invalid response", &GatewayError{Code: "invalid_response"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &characterFixture{}
			snapshots := Snapshots{Gateway: snapshotGateway{failure: tc.failure}, Characters: reader}
			row, err := snapshots.Read(context.Background(), 150000)
			if tc.failure != nil && !tc.database {
				if !errors.Is(err, tc.failure) {
					t.Fatalf("upstream error hidden: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if row["online"] == tc.database {
					t.Fatalf("wrong online status: %+v", row)
				}
				if tc.database && (row["job_id"] != 7 || row["map"] != "prontera" || row["skill_points"] != 3) {
					t.Fatalf("wrong offline projection: %+v", row)
				}
			}
			expectedCalls := 0
			if tc.database {
				expectedCalls = 1
			}
			if reader.calls != expectedCalls {
				t.Fatalf("database reads=%d want=%d", reader.calls, expectedCalls)
			}
		})
	}
}
