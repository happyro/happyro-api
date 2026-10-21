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

type interruptedSettingsResolution struct {
	gameapi.Repository
	interrupt bool
}

func (r *interruptedSettingsResolution) Finish(ctx context.Context, id, status string, result map[string]any, failure *gameapi.GatewayError) (gameapi.Command, error) {
	if r.interrupt && (result["reconciled"] == true || failure != nil && failure.Code == "settings_fenced") {
		r.interrupt = false
		return gameapi.Command{}, errors.New("resolution write interrupted")
	}
	return r.Repository.Finish(ctx, id, status, result, failure)
}

func TestSettingsRecoverySurvivesResolutionWriteFailure(t *testing.T) {
	for _, mode := range []string{"response lost", "barrier wins"} {
		t.Run(mode, func(t *testing.T) {
			db := testDatabase(t)
			ctx := context.Background()
			repository := &interruptedSettingsResolution{Repository: &gameapi.SQLRepository{DB: db}, interrupt: true}
			gateway := &lostSettingsGame{settingsGame: &settingsGame{values: map[string]int64{}}, mode: mode}
			commands := &gameapi.Service{Commands: repository, Gateway: gateway, Timeout: time.Second}
			service := &settings.Service{Revisions: &settings.SQLRevisions{DB: db}, File: settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}, Commands: commands, Gateway: gateway}
			actorID := int64(1)
			actor := gameapi.Actor{AdminUserID: &actorID}
			input := settings.Update{Key: "resolution-interruption", Changes: map[string]int64{"base_exp_rate": 250}}
			if _, err := service.Apply(ctx, input, actor); err == nil {
				t.Fatal("expected unknown result")
			}
			if _, err := service.Resume(ctx, input, actor); err == nil {
				t.Fatal("expected resolution write failure")
			}
			var status string
			if err := db.Get(&status, "SELECT status FROM game_server_commands WHERE id=?", gateway.original.ID); err != nil || status != "indeterminate" {
				t.Fatalf("lost unresolved command: %s %v", status, err)
			}
			barriers := gateway.fenceCalls
			// A new service/repository instance has no in-memory recovery state.
			commands.Commands = &gameapi.SQLRepository{DB: db}
			restarted := &settings.Service{Revisions: &settings.SQLRevisions{DB: db}, File: service.File, Commands: commands, Gateway: gateway}
			applied, err := restarted.Resume(ctx, input, actor)
			if err != nil || applied.Status != "applied" || gateway.calls != 1 || gateway.fenceCalls != barriers {
				t.Fatalf("recovery repeated effects: %+v %v %+v", applied, err, gateway.settingsGame)
			}
		})
	}
}

func TestSettingsRecoveryAfterMapRestartRejectsOldInstance(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	gateway := &lostSettingsGame{settingsGame: &settingsGame{values: map[string]int64{}}, mode: "response lost"}
	commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}
	service := &settings.Service{Revisions: &settings.SQLRevisions{DB: db}, File: settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}, Commands: commands, Gateway: gateway}
	actorID := int64(1)
	actor := gameapi.Actor{AdminUserID: &actorID}
	input := settings.Update{Key: "map-restart", Changes: map[string]int64{"base_exp_rate": 250}}
	if _, err := service.Apply(ctx, input, actor); err == nil {
		t.Fatal("expected unknown result")
	}
	gateway.instance = "fedcba9876543210fedcba9876543210"
	gateway.revision = 0
	gateway.lastCommand = ""
	gateway.lastStatus = "idle"
	gateway.values = map[string]int64{}
	applied, err := service.Resume(ctx, input, actor)
	if err != nil || applied.Status != "applied" || applied.Attempt != 1 || gateway.fenceCalls != 0 || gateway.values["base_exp_rate"] != 250 {
		t.Fatalf("restart recovery failed: %+v %v", applied, err)
	}
	if _, err = gateway.settingsGame.Execute(ctx, gateway.original); err == nil {
		t.Fatal("old instance command accepted")
	}
}

func TestSettingsResolutionDoesNotOverrideLiveOrOtherCommands(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	repository := &gameapi.SQLRepository{DB: db}
	service := &gameapi.Service{Commands: repository}
	actorID := int64(1)
	actor := gameapi.Actor{AdminUserID: &actorID}
	for _, kind := range []string{"battle_config.apply", "character.currency.zeny_grant"} {
		command, _, err := repository.Submit(ctx, gameapi.Request{IdempotencyKey: kind, Type: kind, Payload: map[string]any{}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = repository.Claim(ctx, command.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
		if _, err = service.ResolveSettings(ctx, command.ID, true); !errors.Is(err, gameapi.ErrRunning) {
			t.Fatalf("live lease resolved: %v", err)
		}
		if _, err = db.Exec("UPDATE game_server_commands SET deadline=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", command.ID); err != nil {
			t.Fatal(err)
		}
		resolved, err := service.ResolveSettings(ctx, command.ID, true)
		if kind == "battle_config.apply" {
			if err != nil || resolved.Status != "succeeded" {
				t.Fatalf("expired settings not resolved: %+v %v", resolved, err)
			}
		} else if !errors.Is(err, gameapi.ErrRunning) {
			t.Fatalf("unrelated grant resolved: %v", err)
		}
	}
}
