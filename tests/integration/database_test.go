package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

func testDatabase(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("TEST_ADMIN_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_ADMIN_MYSQL_DSN to a disposable MariaDB server")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	name := "happyro_go_test_" + hex.EncodeToString(token)
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	cfg.DBName = name
	db, err := sqlx.Connect("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	// Re-running migrations must neither fail nor reset existing data.
	if err = database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInitialAdministratorIsNeverReset(t *testing.T) {
	db := testDatabase(t)
	repo := &identityapi.SQLRepository{DB: db}
	service := identityapi.NewService(repo, identityapi.Argon2id{})
	auth, err := sessionauth.NewAdminService(repo, identityapi.Argon2id{})
	if err != nil {
		t.Fatal(err)
	}
	input := identityapi.NewUser{Username: "admin", Password: "first-password", Role: "super_admin"}
	created, err := service.Provision(context.Background(), input, true)
	if err != nil || !created {
		t.Fatalf("first init: created=%v err=%v", created, err)
	}
	input.Password = "replacement"
	created, err = service.Provision(context.Background(), input, true)
	if err != nil || created {
		t.Fatalf("repeat init: created=%v err=%v", created, err)
	}
	user, err := auth.Login(context.Background(), "admin", "first-password", identityapi.Client{IP: "127.0.0.1"})
	if err != nil || !user.Can("*") {
		t.Fatalf("original login: user=%+v err=%v", user, err)
	}
	if err = service.ChangePassword(context.Background(), "admin", "changed-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Login(context.Background(), "admin", "first-password", identityapi.Client{IP: "127.0.0.1"}); !errors.Is(err, sessionauth.ErrCredentials) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err = auth.Login(context.Background(), "admin", "changed-password", identityapi.Client{IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentLoginAttemptsCannotBypassLimit(t *testing.T) {
	db := testDatabase(t)
	repo := &identityapi.SQLRepository{DB: db}
	var allowed atomic.Int32
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			retry, err := repo.Attempt(context.Background(), "same-user-ip")
			if err != nil {
				t.Error(err)
				return
			}
			if retry == 0 {
				allowed.Add(1)
			}
		})
	}
	workers.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d attempts, want 5", allowed.Load())
	}
}

func TestCommandClaimsAndIdempotencyAreDurable(t *testing.T) {
	db := testDatabase(t)
	repo := &gameapi.SQLRepository{DB: db}
	id := int64(1)
	actor := gameapi.Actor{AdminUserID: &id}
	request := gameapi.Request{IdempotencyKey: "one-operation", Type: "character.stats.reset", Target: &gameapi.Target{Type: "character", ID: "2"}, Payload: map[string]any{}}
	command, created, err := repo.Submit(context.Background(), request, actor)
	if err != nil || !created {
		t.Fatalf("submit: created=%v err=%v", created, err)
	}
	replay, created, err := repo.Submit(context.Background(), request, actor)
	if err != nil || created || replay.ID != command.ID {
		t.Fatalf("replay: %+v created=%v err=%v", replay, created, err)
	}
	request.Payload["unexpected"] = 1
	if _, _, err = repo.Submit(context.Background(), request, actor); !errors.Is(err, gameapi.ErrConflict) {
		t.Fatalf("conflicting key accepted: %v", err)
	}
	var claimed atomic.Int32
	var workers sync.WaitGroup
	for range 10 {
		workers.Go(func() {
			_, err := repo.Claim(context.Background(), command.ID, time.Minute)
			if err == nil {
				claimed.Add(1)
			} else if !errors.Is(err, gameapi.ErrRunning) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("claimed %d times", claimed.Load())
	}
	if _, err = db.Exec("UPDATE game_server_commands SET deadline=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE id=?", command.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := repo.Find(context.Background(), command.ID)
	if err != nil || expired.Status != "indeterminate" {
		t.Fatalf("interrupted command not preserved: %+v %v", expired, err)
	}
	if _, err = repo.Claim(context.Background(), command.ID, time.Minute); !errors.Is(err, gameapi.ErrRunning) {
		t.Fatalf("uncertain command claimed again: %v", err)
	}
}

type uncertainGateway struct {
	calls  int
	cancel context.CancelFunc
}

func (*uncertainGateway) Capabilities(context.Context) (gameapi.Capabilities, error) {
	panic("unexpected call")
}
func (*uncertainGateway) BattleConfig(context.Context) (map[string]int64, error) {
	panic("unexpected call")
}
func (*uncertainGateway) CharacterSnapshot(context.Context, int64) (map[string]any, error) {
	panic("unexpected call")
}
func (g *uncertainGateway) Execute(context.Context, gameapi.Command) (map[string]any, error) {
	g.calls++
	if g.cancel != nil {
		g.cancel()
	}
	return nil, &gameapi.GatewayError{Code: "unavailable", Message: "连接中断。", OutcomeUnknown: true}
}

func TestDisconnectedCommandPersistsUnknownOutcomeAndDoesNotReplay(t *testing.T) {
	db := testDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway := &uncertainGateway{cancel: cancel}
	repo := &gameapi.SQLRepository{DB: db}
	service := &gameapi.Service{Commands: repo, Gateway: gateway, Timeout: time.Second}
	id := int64(1)
	actor := gameapi.Actor{AdminUserID: &id}
	request := gameapi.Request{IdempotencyKey: "disconnected", Type: "character.stats.reset", Target: &gameapi.Target{Type: "character", ID: "2"}, Payload: map[string]any{}}
	command, _, err := service.Submit(ctx, request, actor)
	var uncertain *gameapi.GatewayError
	if !errors.As(err, &uncertain) || !uncertain.OutcomeUnknown || command.Status != "indeterminate" {
		t.Fatalf("unknown outcome not persisted: %+v %v", command, err)
	}
	_, _, err = service.Submit(context.Background(), request, actor)
	if !errors.As(err, &uncertain) || !uncertain.OutcomeUnknown {
		t.Fatalf("replay outcome changed: %v", err)
	}
	if gateway.calls != 1 {
		t.Fatalf("executed %d times", gateway.calls)
	}
}
