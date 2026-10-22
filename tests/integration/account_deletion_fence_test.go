package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

func TestAccountWritesCannotCrossDeletionFence(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO login(account_id,userid,user_pass,email,state) VALUES(2000000,'owner','original','old@example.com',5)"); err != nil {
		t.Fatal(err)
	}
	writer := &accountsapi.SQLWriter{DB: db}
	store := &accountsapi.SQLDeletions{DB: db}
	// Hold the connection-owned lock used by enqueue, including on MyISAM.
	tx, err := db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	id := int64(2000000)
	var acquired int
	if err = tx.GetContext(ctx, &acquired, "SELECT GET_LOCK('happyro-account-operation:2000000',0)"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err = writer.SetState(bounded, id, 0); err == nil || !errors.Is(bounded.Err(), context.DeadlineExceeded) {
		t.Fatalf("unban bypassed account lock: %v", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','owner')"); err != nil {
		t.Fatal(err)
	}
	if err = tx.GetContext(ctx, &acquired, "SELECT RELEASE_LOCK('happyro-account-operation:2000000')"); err != nil {
		t.Fatal(err)
	}
	email := "new@example.com"
	for _, status := range []string{"queued", "disconnecting", "deleting", "failed", "completed"} {
		if _, err = db.Exec("UPDATE happyro_admin_account_deletions SET status=? WHERE account_id=?", status, id); err != nil {
			t.Fatal(err)
		}
		if err = writer.SetState(ctx, id, 0); !errors.Is(err, accountsapi.ErrAccountDeleting) {
			t.Fatalf("%s unban: %v", status, err)
		}
		if _, err = writer.Update(ctx, id, accountsapi.AccountUpdate{Email: &email}); !errors.Is(err, accountsapi.ErrAccountDeleting) {
			t.Fatalf("%s edit: %v", status, err)
		}
		if _, err = writer.Password(ctx, id, "replacement"); !errors.Is(err, accountsapi.ErrAccountDeleting) {
			t.Fatalf("%s password: %v", status, err)
		}
	}
	var state int
	var password, actualEmail string
	if err = db.QueryRow("SELECT state,user_pass,email FROM login WHERE account_id=?", id).Scan(&state, &password, &actualEmail); err != nil {
		t.Fatal(err)
	}
	if state != 5 || password != "original" || actualEmail != "old@example.com" {
		t.Fatalf("deleting account mutated: %d %q %q", state, password, actualEmail)
	}
	// The enqueue writer must use the same advisory lock.
	tx2, err := db.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Close()
	defer tx2.ExecContext(context.Background(), "SELECT RELEASE_LOCK('happyro-account-operation:2000000')")
	if err = tx2.GetContext(ctx, &acquired, "SELECT GET_LOCK('happyro-account-operation:2000000',0)"); err != nil {
		t.Fatal(err)
	}
	bounded2, cancel2 := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel2()
	if _, err = store.Enqueue(bounded2, id, identityapi.User{ID: 1, Username: "admin"}); err == nil || !errors.Is(bounded2.Err(), context.DeadlineExceeded) {
		t.Fatalf("enqueue bypassed account lock: %v", err)
	}
}
