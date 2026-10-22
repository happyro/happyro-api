package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/jmoiron/sqlx"
)

type stopAfterAdmission struct {
	db *sqlx.DB
	t  *testing.T
}

func (s stopAfterAdmission) Submit(ctx context.Context, request gameapi.Request, actor gameapi.Actor) (gameapi.Command, bool, error) {
	var row struct {
		Instance string `db:"map_instance"`
		Revision uint64 `db:"map_revision"`
	}
	if err := s.db.GetContext(ctx, &row, "SELECT map_instance,map_revision FROM settings_revisions"); err != nil {
		s.t.Fatal(err)
	}
	if row.Instance != request.Payload["instance_id"] || row.Revision != request.Payload["expected_revision"] {
		s.t.Fatal("command sent before its exact admission was durable")
	}
	return gameapi.Command{}, false, errors.New("process interrupted before command submission")
}

func TestSettingsAdmissionSurvivesInterruptionAndRejectsStaleAttempt(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	store := &settings.SQLRevisions{DB: db}
	gateway := &settingsGame{values: map[string]int64{}}
	file := settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}
	service := &settings.Service{Revisions: store, File: file, Commands: stopAfterAdmission{db: db, t: t}, Gateway: gateway}
	actorID := int64(1)
	actor := gameapi.Actor{AdminUserID: &actorID}
	input := settings.Update{Key: "admission-interruption", Changes: map[string]int64{"base_exp_rate": 250}}
	row, err := service.Apply(ctx, input, actor)
	if err == nil || row.MapInstance == nil || row.MapRevision == nil || *row.MapRevision != 0 {
		t.Fatalf("missing durable admission: %+v %v", row, err)
	}
	countRows(t, db, "game_server_commands", 0)
	// Another command wins while this API process is gone. A restarted API must
	// retain the old expectation and get a rejection, not silently rebase it.
	gateway.revision = 1
	commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}
	restarted := &settings.Service{Revisions: store, File: file, Commands: commands, Gateway: gateway}
	_, err = restarted.Apply(ctx, input, actor)
	var failure *gameapi.GatewayError
	if !errors.As(err, &failure) || failure.Code != "settings_stale_revision" || gateway.revision != 1 || len(gateway.values) != 0 {
		t.Fatalf("stale command executed: %v %+v", err, gateway)
	}
	saved, err := store.ByID(ctx, row.ID)
	if err != nil || saved.MapRevision == nil || *saved.MapRevision != 0 {
		t.Fatalf("admission was rebased: %+v %v", saved, err)
	}
	// Explicit authorized recovery can create a fresh attempt after that rejection.
	applied, err := restarted.Resume(ctx, input, actor)
	if err != nil || applied.Status != "applied" || applied.Attempt != 1 || applied.MapRevision == nil || *applied.MapRevision != 1 || gateway.revision != 2 {
		t.Fatalf("recovery failed: %+v %v", applied, err)
	}
	locked, err := store.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	if err = locked.SaveAdmission(ctx, row.ID, 0, "replacement", 99); !errors.Is(err, settings.ErrUnconfirmed) {
		t.Fatalf("stale attempt admission accepted: %v", err)
	}
	countRows(t, db, "game_server_commands", 2)
}

func (s stopAfterAdmission) ResolveSettings(context.Context, string, bool) (gameapi.Command, error) {
	panic("unexpected recovery before submit")
}
