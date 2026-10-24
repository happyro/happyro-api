package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/settings"
)

func TestSettingsAutomaticallyUnblocksAbandonedPublication(t *testing.T) {
	for _, mode := range []string{"response lost", "barrier wins", "barrier response lost", "command wins"} {
		for _, background := range []bool{false, true} {
			name := mode + " on submission"
			if background {
				name = mode + " background"
			}
			t.Run(name, func(t *testing.T) {
				db := testDatabase(t)
				ctx := context.Background()
				gateway := &lostSettingsGame{settingsGame: &settingsGame{values: map[string]int64{"base_exp_rate": 100}}, mode: mode}
				store := &settings.SQLRevisions{DB: db}
				service := &settings.Service{Revisions: store, File: settings.File{Path: filepath.Join(t.TempDir(), "battle.conf")}, Commands: &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}, Gateway: gateway}
				id := int64(1)
				actor := gameapi.Actor{AdminUserID: &id}
				old, err := service.Apply(ctx, settings.Update{Key: "abandoned", Changes: map[string]int64{"base_exp_rate": 250}}, actor)
				if err == nil {
					t.Fatal("expected lost response")
				}
				if background {
					if err = service.RecoverPending(ctx); err != nil {
						t.Fatalf("background recovery: %v", err)
					}
					if err = service.RecoverPending(ctx); err != nil {
						t.Fatalf("second sweep: %v", err)
					}
				}
				next, err := service.Apply(ctx, settings.Update{Key: "next", Changes: map[string]int64{"base_exp_rate": 300}}, actor)
				if err != nil || next.Status != "applied" {
					t.Fatalf("next request blocked: %+v %v", next, err)
				}
				old, err = store.ByID(ctx, old.ID)
				want := "failed"
				if mode == "response lost" || mode == "command wins" {
					want = "applied"
				}
				if err != nil || old.Status != want {
					t.Fatalf("old revision=%+v want=%s err=%v", old, want, err)
				}
				if _, err = gateway.settingsGame.Execute(ctx, gateway.original); err == nil {
					t.Fatal("late original command was accepted")
				}
				if gateway.values["base_exp_rate"] != 300 {
					t.Fatal("late command overwrote new setting")
				}
			})
		}
	}
}
