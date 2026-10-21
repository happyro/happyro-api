package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/happyro/happyro-api/internal/platform/database"
)

func TestRuntimeRequiresPreparedGameSchemaWithoutCreatingIt(t *testing.T) {
	db := gameDatabase(t)
	ctx := context.Background()
	if err := database.CheckRuntime(ctx, db); err == nil {
		t.Fatal("unprepared game schema accepted")
	}
	var receipts int
	if err := db.Get(&receipts, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='happyro_admin_mail_receipts'"); err != nil || receipts != 0 {
		t.Fatalf("runtime created schema: %d %v", receipts, err)
	}
	if err := prepareAccounts(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := prepareMail(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckRuntime(ctx, db); err == nil || !strings.Contains(err.Error(), "server SQL initialization") {
		t.Fatalf("missing deletion queue: %v", err)
	}
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckRuntime(ctx, db); err != nil {
		t.Fatal(err)
	}
	// An older/incomplete schema must fail before the first queue API request.
	if _, err := db.Exec("ALTER TABLE happyro_admin_account_deletions DROP COLUMN deleted_characters"); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckRuntime(ctx, db); err == nil || !strings.Contains(err.Error(), "server SQL initialization") {
		t.Fatalf("incomplete deletion schema: %v", err)
	}
}
