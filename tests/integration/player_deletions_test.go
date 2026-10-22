package integration

import (
	"context"
	"errors"
	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/platform/database"
	"testing"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

func TestDeletionQueueRequiresWorkerAndKeepsGameDataUntilCleanup(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO login(account_id,userid,user_pass,email,sex) VALUES(1,'server','secret','a@a.com','S'),(2000000,'player_one','secret','a@a.com','M')"); err != nil {
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
	service := accountsapi.Deletions{Store: &accountsapi.SQLDeletions{DB: db}, Accounts: &accountsapi.SQLWriter{DB: db}, Audit: &audit.SQLStore{DB: db}}
	if _, err := service.Request(ctx, 2000000, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrDeletionWorker) {
		t.Fatalf("missing worker accepted %v", err)
	}
	countRows(t, db, "happyro_admin_account_deletions", 0)
	if _, err := db.Exec("INSERT INTO happyro_admin_workers(worker_name,heartbeat_at)VALUES('account-deletions',UTC_TIMESTAMP(6))"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		job, err := service.Request(ctx, 2000000, user, identityapi.Client{})
		if err != nil || job.Status != "queued" || job.AccountID != 2000000 {
			t.Fatalf("queue %+v %v", job, err)
		}
	}
	countRows(t, db, "happyro_admin_account_deletions", 1)
	countRows(t, db, "login", 2)
	if _, err := service.Request(ctx, 1, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server deletion accepted %v", err)
	}
	if _, err := service.Retry(ctx, 2000000, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrDeletionState) {
		t.Fatalf("active deletion restarted %v", err)
	}
	if _, err := db.Exec("UPDATE happyro_admin_account_deletions SET status='failed',error_code='database_error' WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	job, err := service.Retry(ctx, 2000000, user, identityapi.Client{})
	if err != nil || job.Status != "queued" || job.ErrorCode != nil {
		t.Fatalf("failed deletion retry %+v %v", job, err)
	}
	// The completed tombstone remains available after the game server deletes login.
	if _, err := db.Exec("DELETE FROM login WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE happyro_admin_account_deletions SET status='completed',completed_at=UTC_TIMESTAMP(6) WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	job, err = service.Request(ctx, 2000000, user, identityapi.Client{})
	if err != nil || job.Status != "completed" {
		t.Fatalf("completed deletion replay %+v %v", job, err)
	}
}

func TestBatchDeletionRecoversPartialAcceptanceAndListsDurableJobs(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO login(account_id,userid,user_pass,email,sex) VALUES(1,'server','secret','a@a.com','S'),(2000000,'player_one','secret','a@a.com','M'),(2000001,'player_two','secret','a@a.com','F')",
		"INSERT INTO happyro_admin_workers(worker_name,heartbeat_at)VALUES('account-deletions',UTC_TIMESTAMP(6))",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
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
	deletions := &accountsapi.Deletions{Store: &accountsapi.SQLDeletions{DB: db}, Accounts: &accountsapi.SQLWriter{DB: db}, Audit: &audit.SQLStore{DB: db}}
	batches := &accountsapi.Batches{Deletions: deletions, Accounts: &accountsapi.SQLWriter{DB: db}, Records: &accountsapi.SQLBatches{DB: db}, Audit: &audit.SQLStore{DB: db}}
	if _, err := batches.Run(ctx, accountsapi.BatchInput{Key: "protected", Action: "delete", IDs: []int64{2000000, 1}}, user, identityapi.Client{}); !errors.Is(err, accountsapi.ErrProtected) {
		t.Fatalf("server protection: %v", err)
	}
	countRows(t, db, "happyro_admin_account_deletions", 0)
	// Simulate a game worker finishing the first job before the whole batch is saved.
	if _, err := deletions.Request(ctx, 2000000, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"UPDATE happyro_admin_account_deletions SET status='completed' WHERE account_id=2000000", "DELETE FROM login WHERE account_id=2000000"} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	input := accountsapi.BatchInput{Key: "delete-two", Action: "delete", IDs: []int64{2000001, 2000000, 2000001}}
	for range 2 {
		result, err := batches.Run(ctx, input, user, identityapi.Client{})
		if err != nil || result.Status != "accepted" || result.Count != 2 || len(result.DeletionIDs) != 2 {
			t.Fatalf("acceptance: %+v %v", result, err)
		}
	}
	countRows(t, db, "happyro_admin_account_deletions", 2)
	first, err := deletions.List(ctx, database.Pagination{Page: 1, Size: 1})
	if err != nil || first.Total != 2 || len(first.Data) != 1 || first.Data[0].AccountID != 2000001 {
		t.Fatalf("active job first: %+v %v", first, err)
	}
	second, err := deletions.List(ctx, database.Pagination{Page: 2, Size: 1})
	if err != nil || len(second.Data) != 1 || second.Data[0].Status != "completed" {
		t.Fatalf("history: %+v %v", second, err)
	}
	input.IDs = []int64{2000001}
	if _, err := batches.Run(ctx, input, user, identityapi.Client{}); !errors.Is(err, gameapi.ErrConflict) {
		t.Fatalf("changed selection: %v", err)
	}
}
