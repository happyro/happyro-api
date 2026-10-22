package integration

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/settings"
)

type interruptedSettingSubmit struct {
	delegate  settings.Commands
	interrupt bool
}

func (s *interruptedSettingSubmit) Submit(ctx context.Context, request gameapi.Request, actor gameapi.Actor) (gameapi.Command, bool, error) {
	if s.interrupt && strings.HasSuffix(request.IdempotencyKey, ":1") {
		s.interrupt = false
		return gameapi.Command{}, false, errors.New("interrupted before submitting the persisted retry")
	}
	return s.delegate.Submit(ctx, request, actor)
}

func TestSettingsRecoveryRetriesOnlyDefinitivelyRejectedCommands(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "rejected"
		if unknown {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			db := testDatabase(t)
			ctx := context.Background()
			gateway := &settingsGame{values: map[string]int64{}, executeFailure: &gameapi.GatewayError{Code: "authentication_failed", Message: "rejected", OutcomeUnknown: unknown}}
			commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}
			submit := &interruptedSettingSubmit{delegate: commands}
			store := &settings.SQLRevisions{DB: db}
			service := &settings.Service{Revisions: store, File: settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}, Commands: submit, Gateway: gateway}
			actorID := int64(1)
			actor := gameapi.Actor{AdminUserID: &actorID}
			input := settings.Update{Key: "recovery-attempt", Changes: map[string]int64{"base_exp_rate": 250}}
			row, err := service.Apply(ctx, input, actor)
			if err == nil || gateway.calls != 1 {
				t.Fatalf("initial failure: %+v %v calls=%d", row, err, gateway.calls)
			}
			// Ordinary request replay never retries a terminal command.
			if _, err = service.Apply(ctx, input, actor); err == nil || gateway.calls != 1 {
				t.Fatal("ordinary replay executed again")
			}
			recovery := &settings.Recovery{Revisions: store, Settings: service, Audit: &audit.SQLStore{DB: db}}
			user := identityapi.User{ID: 2, Username: "operator", Permissions: []string{"settings.manage"}}
			gateway.executeFailure = nil
			if unknown {
				locked, lockErr := store.Lock(ctx)
				if lockErr != nil {
					t.Fatal(lockErr)
				}
				advanceErr := locked.AdvanceAttempt(ctx, row.ID, 0)
				closeErr := locked.Close()
				if !errors.Is(advanceErr, settings.ErrUnconfirmed) || closeErr != nil {
					t.Fatalf("unknown command allowed new identity: %v %v", advanceErr, closeErr)
				}
				if _, err = recovery.Resume(ctx, row.ID, user, identityapi.Client{}); err == nil || gateway.calls != 1 {
					t.Fatal("unknown outcome was retried")
				}
				saved, err := store.ByID(ctx, row.ID)
				if err != nil || saved.Attempt != 0 || saved.Status != "published" {
					t.Fatalf("unknown state changed: %+v %v", saved, err)
				}
				countRows(t, db, "game_server_commands", 1)
				return
			}
			// An interruption after the new identity is committed must not lose it or
			// create another attempt when a fresh API process resumes the revision.
			submit.interrupt = true
			if _, err = recovery.Resume(ctx, row.ID, user, identityapi.Client{}); err == nil || gateway.calls != 1 {
				t.Fatal("interruption did not stop before retry")
			}
			saved, err := store.ByID(ctx, row.ID)
			if err != nil || saved.Attempt != 1 || saved.Status != "published" {
				t.Fatalf("retry identity lost: %+v %v", saved, err)
			}
			service.Commands = commands
			applied, err := recovery.Resume(ctx, row.ID, user, identityapi.Client{})
			if err != nil || applied.Status != "applied" || applied.Attempt != 1 || gateway.calls != 2 {
				t.Fatalf("retry: %+v %v calls=%d", applied, err, gateway.calls)
			}
			// The rejected attempt is immutable; the new successful command is distinct.
			var statuses []string
			if err = db.Select(&statuses, "SELECT status FROM game_server_commands ORDER BY idempotency_key"); err != nil || len(statuses) != 2 || statuses[0] != "failed" || statuses[1] != "succeeded" {
				t.Fatalf("command history: %v %v", statuses, err)
			}
			if _, err = recovery.Resume(ctx, row.ID, user, identityapi.Client{}); err != nil || gateway.calls != 2 {
				t.Fatal("completed recovery replayed")
			}
			if _, err = service.Apply(ctx, input, actor); err != nil || gateway.calls != 2 {
				t.Fatal("completed original request replayed")
			}
		})
	}
}

func (s *interruptedSettingSubmit) ResolveSettings(ctx context.Context, id string, succeeded bool) (gameapi.Command, error) {
	return s.delegate.ResolveSettings(ctx, id, succeeded)
}
