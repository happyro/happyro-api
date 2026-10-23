package integration

import (
	"context"
	"errors"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"testing"
)

func TestAccountGMStoredPermissionsAndFences(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareAccounts(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO login(account_id,userid,user_pass,email,sex,group_id) VALUES(2000000,'gmtest','secret','test@example.com','M',0),(2000001,'server','secret','server@example.com','S',0)"); err != nil {
		t.Fatal(err)
	}
	w := &accountsapi.SQLWriter{DB: db}
	if _, err := w.SetGM(ctx, 2000000, 0, 99); err != nil {
		t.Fatal(err)
	}
	var group int
	if err := db.Get(&group, "SELECT group_id FROM login WHERE account_id=2000000"); err != nil || group != 99 {
		t.Fatalf("promotion: %d %v", group, err)
	}
	if _, err := w.SetGM(ctx, 2000000, 0, 0); !errors.Is(err, accountsapi.ErrGroupChanged) {
		t.Fatalf("stale update: %v", err)
	}
	if _, err := w.SetGM(ctx, 2000000, 99, 99); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	if _, err := w.SetGM(ctx, 2000000, 99, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetGM(ctx, 2000001, 0, 99); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server account: %v", err)
	}
	if _, err := w.SetGM(ctx, 2999999, 0, 99); !errors.Is(err, accountsapi.ErrAccountNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := db.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','gmtest')"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetGM(ctx, 2000000, 0, 99); !errors.Is(err, accountsapi.ErrAccountDeleting) {
		t.Fatalf("deletion: %v", err)
	}
	if err := db.Get(&group, "SELECT group_id FROM login WHERE account_id=2000000"); err != nil || group != 0 {
		t.Fatalf("demotion: %d %v", group, err)
	}
}
