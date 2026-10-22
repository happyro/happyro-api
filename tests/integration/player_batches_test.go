package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

type disconnectGateway struct {
	successfulGateway
	callsByAccount map[int64]int
	failID         int64
}

func (g *disconnectGateway) Execute(_ context.Context, command gameapi.Command) (map[string]any, error) {
	if command.Type != "account.disconnect" || command.TargetType == nil || *command.TargetType != "account" {
		return nil, errors.New("incorrect disconnect command")
	}
	id, _ := strconv.ParseInt(*command.TargetID, 10, 64)
	g.callsByAccount[id]++
	if g.failID == id {
		return nil, &gameapi.GatewayError{Code: "unavailable", Message: "disconnection result unknown", OutcomeUnknown: true}
	}
	return map[string]any{"account_id": json.Number(strconv.FormatInt(id, 10)), "disconnected": id == 2000000}, nil
}
func TestPlayerBatchPersistsDisconnectsAndProtectsServerAccounts(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO login(account_id,userid,user_pass,email,sex,web_auth_token,web_auth_token_enabled) VALUES(1,'server','secret','a@a.com','S',NULL,0),(2000000,'player_one','secret','a@a.com','M','online-token',1),(2000001,'player_two','secret','a@a.com','F',NULL,0)"); err != nil {
		t.Fatal(err)
	}
	identity := identityapi.NewService(&identityapi.SQLRepository{DB: db}, identityapi.Argon2id{})
	if _, err := identity.Provision(ctx, identityapi.NewUser{Username: "admin", Password: "admin", Role: "super_admin"}, true); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.Get(&id, "SELECT id FROM users WHERE username='admin'"); err != nil {
		t.Fatal(err)
	}
	user := identityapi.User{ID: id, Username: "admin"}
	gateway := &disconnectGateway{callsByAccount: map[int64]int{}}
	batches := accountsapi.Batches{Accounts: &accountsapi.SQLWriter{DB: db}, Records: &accountsapi.SQLBatches{DB: db}, Commands: &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}, Audit: &audit.SQLStore{DB: db}}
	input := accountsapi.BatchInput{Key: "batch-ban", Action: "ban", IDs: []int64{2000001, 2000000, 2000000}}
	for range 2 {
		result, err := batches.Run(ctx, input, user, identityapi.Client{})
		if err != nil || result.Count != 2 {
			t.Fatalf("ban %+v %v", result, err)
		}
	}
	if gateway.callsByAccount[2000000] != 1 || gateway.callsByAccount[2000001] != 1 {
		t.Fatalf("duplicate disconnects %+v", gateway.callsByAccount)
	}
	var disabled int
	if err := db.Get(&disabled, "SELECT COUNT(*) FROM login WHERE account_id>=2000000 AND state=1 AND web_auth_token IS NULL AND web_auth_token_enabled=0"); err != nil || disabled != 2 {
		t.Fatalf("accounts not disabled %d %v", disabled, err)
	}
	input.Action = "unban"
	if _, err := batches.Run(ctx, input, user, identityapi.Client{}); !errors.Is(err, gameapi.ErrConflict) {
		t.Fatalf("key reuse accepted %v", err)
	}
	input.Key = "batch-unban"
	if _, err := batches.Run(ctx, input, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&disabled, "SELECT COUNT(*) FROM login WHERE account_id>=2000000 AND state=0"); err != nil || disabled != 2 {
		t.Fatalf("unban failed %v", err)
	}
	// A successful old batch must not reapply its state after a newer operation.
	input.Action = "ban"
	input.Key = "batch-ban"
	if _, err := batches.Run(ctx, input, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&disabled, "SELECT COUNT(*) FROM login WHERE account_id>=2000000 AND state=0"); err != nil || disabled != 2 {
		t.Fatalf("old batch reapplied state %v", err)
	}
	input.Key = "batch-kick"
	input.Action = "kick"
	result, err := batches.Run(ctx, input, user, identityapi.Client{})
	if err != nil || result.Count != 1 {
		t.Fatalf("kick count %+v %v", result, err)
	}
	input.Key = "batch-server"
	input.Action = "ban"
	input.IDs = []int64{2000000, 1}
	if _, err := batches.Run(ctx, input, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server account accepted %v", err)
	}
	if err := db.Get(&disabled, "SELECT COUNT(*) FROM login WHERE state<>0"); err != nil || disabled != 0 {
		t.Fatal("protected batch partially changed accounts")
	}
}
func TestUnknownBatchDisconnectDoesNotRepeatEarlierAccounts(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO login(account_id,userid,user_pass,email,sex) VALUES(2000000,'player_one','secret','a@a.com','M'),(2000001,'player_two','secret','a@a.com','F')"); err != nil {
		t.Fatal(err)
	}
	// The audit foreign key requires a real administrator.
	identity := identityapi.NewService(&identityapi.SQLRepository{DB: db}, identityapi.Argon2id{})
	if _, err := identity.Provision(ctx, identityapi.NewUser{Username: "admin", Password: "admin", Role: "super_admin"}, true); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.Get(&id, "SELECT id FROM users WHERE username='admin'"); err != nil {
		t.Fatal(err)
	}
	gateway := &disconnectGateway{callsByAccount: map[int64]int{}, failID: 2000001}
	batches := accountsapi.Batches{Accounts: &accountsapi.SQLWriter{DB: db}, Records: &accountsapi.SQLBatches{DB: db}, Commands: &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}, Audit: &audit.SQLStore{DB: db}}
	input := accountsapi.BatchInput{Key: "partial-kick", Action: "kick", IDs: []int64{2000000, 2000001}}
	for range 2 {
		if _, err := batches.Run(ctx, input, identityapi.User{ID: id, Username: "admin"}, identityapi.Client{}); err == nil {
			t.Fatal("unknown outcome reported as success")
		}
	}
	if gateway.callsByAccount[2000000] != 1 || gateway.callsByAccount[2000001] != 1 {
		t.Fatalf("unknown command retried %+v", gateway.callsByAccount)
	}
}
