package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/settings"
)

type lostSettingsGame struct {
	*settingsGame
	mode     string
	lost     bool
	original gameapi.Command
}

func (g *lostSettingsGame) Execute(ctx context.Context, c gameapi.Command) (map[string]any, error) {
	if g.lost {
		return g.settingsGame.Execute(ctx, c)
	}
	g.lost = true
	g.original = c
	if g.mode == "response lost" {
		if _, err := g.settingsGame.Execute(ctx, c); err != nil {
			return nil, err
		}
	}
	return nil, &gameapi.GatewayError{Code: "unavailable", Message: "response lost", OutcomeUnknown: true}
}
func (g *lostSettingsGame) FenceSettings(ctx context.Context, instance string, revision uint64) error {
	g.fenceCalls++
	if g.mode == "command wins" {
		if _, err := g.settingsGame.Execute(ctx, g.original); err != nil {
			return err
		}
	}
	if instance != g.instanceID() || revision != g.revision {
		return errors.New("CAS conflict")
	}
	g.revision++
	g.lastCommand = ""
	g.lastStatus = "fenced"
	if g.mode == "barrier response lost" {
		return errors.New("response lost")
	}
	return nil
}
func TestSettingsUnknownRecoveryUsesReceiptOrBarrierWithoutRepeatingEffects(t *testing.T) {
	for _, mode := range []string{"response lost", "barrier wins", "barrier response lost", "command wins"} {
		t.Run(mode, func(t *testing.T) {
			db := testDatabase(t)
			ctx := context.Background()
			gateway := &lostSettingsGame{settingsGame: &settingsGame{values: map[string]int64{}}, mode: mode}
			commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}
			store := &settings.SQLRevisions{DB: db}
			service := &settings.Service{Revisions: store, File: settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}, Commands: commands, Gateway: gateway}
			actorID := int64(1)
			actor := gameapi.Actor{AdminUserID: &actorID}
			input := settings.Update{Key: "lost-settings", Changes: map[string]int64{"base_exp_rate": 250}}
			row, err := service.Apply(ctx, input, actor)
			if err == nil {
				t.Fatal("expected lost response")
			}
			if _, err = service.Apply(ctx, input, actor); err == nil || gateway.fenceCalls != 0 {
				t.Fatal("ordinary replay attempted recovery")
			}
			applied, err := service.Resume(ctx, input, actor)
			if err != nil || applied.Status != "applied" || gateway.values["base_exp_rate"] != 250 || gateway.calls != 1 {
				t.Fatalf("recovery repeated or lost effects: %+v %v %+v", applied, err, gateway.settingsGame)
			}
			barrierWon := mode == "barrier wins" || mode == "barrier response lost"
			expectedAttempt := uint32(0)
			if barrierWon {
				expectedAttempt = 1
			}
			if applied.Attempt != expectedAttempt {
				t.Fatalf("wrong attempt: %+v", applied)
			}
			var originalStatus string
			if err = db.Get(&originalStatus, "SELECT status FROM game_server_commands WHERE id=?", gateway.original.ID); err != nil {
				t.Fatal(err)
			}
			want := "succeeded"
			if barrierWon {
				want = "failed"
			}
			if originalStatus != want {
				t.Fatalf("wrong recovered command state: %s", originalStatus)
			}
			if barrierWon {
				// A request delayed past recovery cannot affect the current settings.
				gateway.values["base_exp_rate"] = 300
				_, err = gateway.settingsGame.Execute(ctx, gateway.original)
				var failure *gameapi.GatewayError
				if !errors.As(err, &failure) || failure.Code != "settings_stale_revision" || gateway.values["base_exp_rate"] != 300 {
					t.Fatal("delayed original mutated settings")
				}
			}
			if _, err = service.Resume(ctx, input, actor); err != nil {
				t.Fatal(err)
			}
			saved, err := store.ByID(ctx, row.ID)
			if err != nil || saved.Status != "applied" {
				t.Fatalf("missing durable recovery: %+v %v", saved, err)
			}
		})
	}
}
