package integration

import (
	"context"
	"errors"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"testing"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

func TestGameIdentityRejectsRestrictedAccountsBeforeTokenRevocation(t *testing.T) {
	db := gameDatabase(t)
	ctx := context.Background()
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO login(account_id,userid,web_auth_token,web_auth_token_enabled) VALUES(2000000,'owner','valid-token',1)",
		"INSERT INTO `char`(char_id,account_id,name,sex,online) VALUES(150000,2000000,'Online','M',1)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	service := sessionauth.GameAuthentication{Repository: &sessionauth.SQLGameIdentity{DB: db}}
	check := func(t *testing.T, allowed bool) {
		t.Helper()
		_, err := service.Authenticate(ctx, 2000000, 150000, "valid-token")
		if allowed && err != nil {
			t.Fatalf("valid session rejected: %v", err)
		}
		if !allowed && !errors.Is(err, sessionauth.ErrGameSession) {
			t.Fatalf("restricted session result: %v", err)
		}
	}
	check(t, true)
	for _, tc := range []struct {
		name, change string
		allowed      bool
	}{
		{"disabled", "state=5", false},
		{"temporary ban", "state=0,unban_time=UNIX_TIMESTAMP()+3600", false},
		{"expired ban", "unban_time=UNIX_TIMESTAMP()-3600", true},
		{"expired account", "expiration_time=UNIX_TIMESTAMP()-3600", false},
		{"future expiration", "expiration_time=UNIX_TIMESTAMP()+3600", true},
		{"unlimited account", "expiration_time=0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec("UPDATE login SET state=0,unban_time=0,expiration_time=0 WHERE account_id=2000000"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE login SET " + tc.change + " WHERE account_id=2000000"); err != nil {
				t.Fatal(err)
			}
			check(t, tc.allowed)
		})
	}
	// Enqueue via the real repository; do not run the worker or revoke the token.
	store := accountsapi.SQLDeletions{DB: db}
	if _, err := store.Enqueue(ctx, 2000000, identityapi.User{ID: 1, Username: "operator"}); err != nil {
		t.Fatal(err)
	}
	check(t, false)
	for _, status := range []string{"disconnecting", "deleting", "failed", "completed"} {
		if _, err := db.Exec("UPDATE happyro_admin_account_deletions SET status=? WHERE account_id=2000000", status); err != nil {
			t.Fatal(err)
		}
		check(t, false)
	}
}
