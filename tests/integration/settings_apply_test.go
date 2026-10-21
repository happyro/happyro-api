package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/settings"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type settingsGame struct {
	gameapi.Gateway
	instance       string
	revision       uint64
	lastCommand    string
	lastStatus     string
	fenceCalls     int
	calls          int
	values         map[string]int64
	readFailure    bool
	executeFailure error
}

func (g *settingsGame) Execute(_ context.Context, c gameapi.Command) (map[string]any, error) {
	g.calls++
	if g.executeFailure != nil {
		return nil, g.executeFailure
	}
	if c.Type != "battle_config.apply" || c.TargetType == nil || *c.TargetType != "server" {
		return nil, errors.New("wrong settings command")
	}
	expected, ok := c.Payload["expected_revision"].(json.Number)
	if !ok || expected.String() != strconv.FormatUint(g.revision, 10) || c.Payload["instance_id"] != g.instanceID() {
		return nil, &gameapi.GatewayError{Code: "settings_stale_revision", Message: "stale settings"}
	}
	g.revision++
	g.lastCommand, g.lastStatus = c.ID, "succeeded"
	// Use the real SQL command decoder's JSON representation.
	for _, raw := range c.Payload["changes"].([]any) {
		entry := raw.(map[string]any)
		value, _ := gameapi.Integer(entry["value"])
		g.values[entry["key"].(string)] = value
	}
	return map[string]any{"applied": true}, nil
}
func (g *settingsGame) BattleConfig(context.Context) (map[string]int64, error) {
	if g.readFailure {
		return nil, errors.New("read unavailable")
	}
	return g.values, nil
}
func TestSettingsApplyRecoversReadbackFailureWithoutRepeatingCommand(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	file := settings.File{Path: filepath.Join(t.TempDir(), "battle_conf.txt")}
	gateway := &settingsGame{values: map[string]int64{}, readFailure: true}
	service := &settings.Service{Revisions: &settings.SQLRevisions{DB: db}, File: file, Commands: &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}, Gateway: gateway}
	id := int64(1)
	actor := gameapi.Actor{AdminUserID: &id}
	input := settings.Update{Key: "settings-request", Changes: map[string]int64{"base_exp_rate": 250}}
	row, err := service.Apply(ctx, input, actor)
	if err == nil || row.Status != "published" || gateway.calls != 1 {
		t.Fatalf("read failure %+v %v calls=%d", row, err, gateway.calls)
	}
	content, err := file.Read()
	if err != nil || !strings.Contains(content, "base_exp_rate: 250") {
		t.Fatal("published config lost")
	}
	if _, err = service.Apply(ctx, settings.Update{Key: "different", Changes: map[string]int64{"job_exp_rate": 200}}, actor); !errors.Is(err, settings.ErrBusy) {
		t.Fatalf("unfinished overwritten %v", err)
	}
	gateway.readFailure = false
	recovery := &settings.Recovery{Revisions: &settings.SQLRevisions{DB: db}, Settings: service, Audit: &audit.SQLStore{DB: db}}
	if _, err = recovery.Resume(ctx, row.ID, identityapi.User{ID: 2, Permissions: []string{"players.view"}}, identityapi.Client{}); err == nil {
		t.Fatal("read-only user recovered settings")
	}
	row, err = recovery.Resume(ctx, row.ID, identityapi.User{ID: 2, Username: "operator", Permissions: []string{"settings.manage"}}, identityapi.Client{})
	if err != nil || row.Status != "applied" || gateway.calls != 1 {
		t.Fatalf("retry %+v %v calls=%d", row, err, gateway.calls)
	}
	// A later live change is not reverted by replaying a completed request.
	gateway.values["base_exp_rate"] = 300
	if _, err = service.Apply(ctx, input, actor); err != nil || gateway.calls != 1 {
		t.Fatal("completed request replayed")
	}
	input.Changes["base_exp_rate"] = 400
	if _, err = service.Apply(ctx, input, actor); !errors.Is(err, gameapi.ErrConflict) {
		t.Fatalf("payload conflict %v", err)
	}
	countRows(t, db, "settings_revisions", 1)
	var recoveryActor int64
	if err = db.Get(&recoveryActor, "SELECT actor_id FROM audit_logs WHERE action='settings.resume_requested'"); err != nil || recoveryActor != 2 {
		t.Fatalf("recovery audit: %d %v", recoveryActor, err)
	}
	if row.AdminID == nil || *row.AdminID != 1 {
		t.Fatal("recovery changed original actor")
	}
	if _, err = recovery.Resume(ctx, "missing", identityapi.User{ID: 2, Permissions: []string{"settings.manage"}}, identityapi.Client{}); !errors.Is(err, settings.ErrRevisionNotFound) {
		t.Fatalf("missing revision: %v", err)
	}
	countRows(t, db, "game_server_commands", 1)
}

func (g *settingsGame) SettingsState(context.Context) (gameapi.SettingsState, error) {
	return gameapi.SettingsState{InstanceID: g.instanceID(), Revision: g.revision, Values: g.values, LastCommandID: g.lastCommand, LastStatus: g.lastStatus}, nil
}

func (g *settingsGame) FenceSettings(context.Context, string, uint64) error {
	g.fenceCalls++
	return errors.New("fence unavailable")
}

func (g *settingsGame) instanceID() string {
	if g.instance != "" {
		return g.instance
	}
	return "0123456789abcdef0123456789abcdef"
}
