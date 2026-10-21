package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

func TestConcurrentAccountCreationAndSafePasswordReset(t *testing.T) {
	ctx := context.Background()
	game := gameDatabase(t)
	if err := prepareDeletions(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	admin := testDatabase(t)
	if err := prepareAccounts(ctx, game); err != nil {
		t.Fatal(err)
	}
	if err := prepareAccounts(ctx, game); err != nil {
		t.Fatal(err)
	}
	identity := identityapi.NewService(&identityapi.SQLRepository{DB: admin}, identityapi.Argon2id{})
	if _, err := identity.Provision(ctx, identityapi.NewUser{Username: "admin", Password: "admin", Role: "super_admin"}, true); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := admin.Get(&userID, "SELECT id FROM users WHERE username='admin'"); err != nil {
		t.Fatal(err)
	}
	user := identityapi.User{ID: userID, Username: "admin"}
	service := accountsapi.Management{Accounts: &accountsapi.SQLWriter{DB: game}, Audit: &audit.SQLStore{DB: admin}}
	input := accountsapi.NewAccount{Username: "player_one", Password: "original-secret", Email: "one@example.com", Sex: "M"}
	const concurrency = 8
	failures := make(chan error, concurrency)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() { _, err := service.Create(ctx, input, user, identityapi.Client{}); failures <- err })
	}
	wg.Wait()
	close(failures)
	succeeded := 0
	for err := range failures {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, accountsapi.ErrAccountExists) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("created %d accounts", succeeded)
	}
	var id int64
	if err := game.Get(&id, "SELECT account_id FROM login WHERE userid='player_one'"); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Exec("UPDATE login SET web_auth_token='existing-token',web_auth_token_enabled=1 WHERE account_id=?", id); err != nil {
		t.Fatal(err)
	}
	row, err := service.Password(ctx, id, "replacement-secret", user, identityapi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "user_pass") || strings.Contains(string(raw), "token") {
		t.Fatalf("credentials exposed %s", raw)
	}
	var password string
	var enabled int
	var token *string
	if err = game.QueryRow("SELECT user_pass,web_auth_token,web_auth_token_enabled FROM login WHERE account_id=?", id).Scan(&password, &token, &enabled); err != nil {
		t.Fatal(err)
	}
	if password != "replacement-secret" || token != nil || enabled != 0 {
		t.Fatal("password or token revocation failed")
	}
	email := "updated@example.com"
	state := int64(1)
	if _, err = service.Update(ctx, id, accountsapi.AccountUpdate{Email: &email, State: &state}, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, id, accountsapi.AccountUpdate{Email: &email, State: &state}, user, identityapi.Client{}); err != nil {
		t.Fatalf("unchanged update failed %v", err)
	}
	if _, err = service.Password(ctx, 999999, "replacement-secret", user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrAccountNotFound) {
		t.Fatalf("missing account %v", err)
	}
	if _, err = game.Exec("INSERT INTO login(account_id,userid,user_pass,sex,email) VALUES(1,'server','protected-secret','S','server@example.com')"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Password(ctx, 1, "replacement-secret", user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server password change %v", err)
	}
	if _, err = service.Update(ctx, 1, accountsapi.AccountUpdate{State: &state}, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server state change %v", err)
	}
	if err = game.Get(&password, "SELECT user_pass FROM login WHERE account_id=1"); err != nil || password != "protected-secret" {
		t.Fatalf("protected account mutated %v", err)
	}
	var metadata []string
	if err = admin.Select(&metadata, "SELECT CAST(metadata AS CHAR) FROM audit_logs"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range metadata {
		if strings.Contains(entry, "secret") || strings.Contains(entry, "user_pass") {
			t.Fatalf("audit leaked password %s", entry)
		}
	}
}
func TestAccountPreparationRejectsDuplicateNamesWithoutChangingData(t *testing.T) {
	game := gameDatabase(t)
	if err := prepareDeletions(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Exec("INSERT INTO login(userid,user_pass,email,sex) VALUES('duplicate','secret','one@example.com','M'),('duplicate','secret','two@example.com','F')"); err != nil {
		t.Fatal(err)
	}
	if err := prepareAccounts(context.Background(), game); err == nil {
		t.Fatal("duplicate data accepted")
	}
	countRows(t, game, "login", 2)
	if _, err := (&accountsapi.SQLWriter{DB: game}).Create(context.Background(), accountsapi.NewAccount{Username: "another", Password: "secret", Email: "three@example.com", Sex: "M"}); err == nil {
		t.Fatal("creation without schema constraint accepted")
	}
	countRows(t, game, "login", 2)
}
func TestAccountMutationRequiresAuditIntentBeforeWriting(t *testing.T) {
	game := gameDatabase(t)
	if err := prepareDeletions(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	admin := testDatabase(t)
	if err := prepareAccounts(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE TRIGGER reject_player_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'"); err != nil {
		t.Fatal(err)
	}
	s := accountsapi.Management{Accounts: &accountsapi.SQLWriter{DB: game}, Audit: &audit.SQLStore{DB: admin}}
	_, err := s.Create(context.Background(), accountsapi.NewAccount{Username: "player_one", Password: "secret", Email: "one@example.com", Sex: "M"}, identityapi.User{ID: 1, Username: "admin"}, identityapi.Client{})
	if err == nil {
		t.Fatal("mutation accepted without audit")
	}
	countRows(t, game, "login", 0)
}
