package integration

import (
	"context"
	"errors"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"testing"
)

func TestRegistrationPolicyPersistsAndRejectsStaleChanges(t *testing.T) {
	db := gameDatabase(t)
	ctx := context.Background()
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	writer := &accountsapi.SQLWriter{DB: db}
	if group, err := writer.RegistrationGroup(ctx); err != nil || group != 0 {
		t.Fatalf("initial group %d %v", group, err)
	}
	if _, err := writer.SetRegistrationGroup(ctx, 0, 99); err != nil {
		t.Fatal(err)
	}
	// Initialization must preserve a saved demo policy.
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if group, err := (&accountsapi.SQLWriter{DB: db}).RegistrationGroup(ctx); err != nil || group != 99 {
		t.Fatalf("saved group %d %v", group, err)
	}
	if _, err := writer.SetRegistrationGroup(ctx, 0, 0); !errors.Is(err, accountsapi.ErrGroupChanged) {
		t.Fatal("stale write accepted", err)
	}
	if _, err := db.Exec("UPDATE happyro_registration_settings SET default_group_id=100 WHERE id=1"); err == nil {
		t.Fatal("invalid group accepted")
	}
	if _, err := writer.SetRegistrationGroup(ctx, 99, 0); err != nil {
		t.Fatal(err)
	}
}
