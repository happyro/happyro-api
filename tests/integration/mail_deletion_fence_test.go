package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
)

func TestMailDeliverySharesDeletionFenceAndPreservesCompletedReceipt(t *testing.T) {
	_, _, db, _ := mailSetup(t)
	ctx := context.Background()
	mail := &rewardsapi.SQLMail{DB: db}
	prepared := rewardsapi.PreparedMail{Request: mailInput(), Sender: "admin", AttachmentAmounts: []int64{1}}
	_, release, err := database.LockAccountOperation(ctx, db, 2000000)
	if err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = mail.Deliver(bounded, "blocked", "hash", prepared)
	cancel()
	release()
	if err == nil || !errors.Is(bounded.Err(), context.DeadlineExceeded) {
		t.Fatalf("mail bypassed deletion lock: %v", err)
	}
	countRows(t, db, "mail", 0)
	countRows(t, db, "mail_attachments", 0)
	countRows(t, db, "happyro_admin_mail_receipts", 0)
	// A single pool connection must suffice for the receipt, account lock and mail transaction.
	db.SetMaxOpenConns(1)
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	original, err := mail.Deliver(deadline, "original", "hash", prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','owner')"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"queued", "disconnecting", "deleting", "failed", "completed"} {
		if _, err = db.Exec("UPDATE happyro_admin_account_deletions SET status=? WHERE account_id=2000000", status); err != nil {
			t.Fatal(err)
		}
		if _, err = mail.Deliver(deadline, "new-"+status, "hash", prepared); !errors.Is(err, rewardsapi.ErrAccountDeleting) {
			t.Fatalf("%s delivery: %v", status, err)
		}
	}
	countRows(t, db, "mail", 1)
	countRows(t, db, "mail_attachments", 1)
	countRows(t, db, "happyro_admin_mail_receipts", 1)
	// The game worker may remove mail and character data, but the durable receipt
	// must still answer a retry without recreating delivered resources.
	for _, statement := range []string{"DELETE FROM mail_attachments", "DELETE FROM mail", "DELETE FROM `char`"} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	got, err := mail.Deliver(deadline, "original", "hash", prepared)
	if err != nil || got != original {
		t.Fatalf("completed replay: %d %v", got, err)
	}
	countRows(t, db, "mail", 0)
	countRows(t, db, "mail_attachments", 0)
}
