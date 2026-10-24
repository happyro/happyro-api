package integration

import (
	"context"
	"errors"
	"github.com/happyro/happyro-api/internal/identity"
	"sync"
	"testing"
)

func TestSelfPasswordConcurrentUpdateAndAuditRollback(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	repo := &identity.SQLRepository{DB: db}
	hasher := identity.Argon2id{}
	admin := identity.NewService(repo, hasher)
	if _, err := admin.Provision(ctx, identity.NewUser{Username: "operator", Password: "original-password", Role: "operator"}, false); err != nil {
		t.Fatal(err)
	}
	actor, err := repo.ByUsername(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}
	service := identity.NewSelfPasswordService(repo, hasher)
	client := identity.Client{IP: "127.0.0.1", UserAgent: "password-test"}
	if err := service.Change(ctx, actor.ID, "wrong-password", "replacement-password", "replacement-password", client); !errors.Is(err, identity.ErrCurrentPassword) {
		t.Fatalf("old verification: %v", err)
	}
	if err := service.Change(ctx, actor.ID, "original-password", "replacement-password", "mismatch", client); !errors.Is(err, identity.ErrPasswordInput) {
		t.Fatalf("confirmation: %v", err)
	}
	hash, err := hasher.Hash("replacement-password")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.ReplacePassword(ctx, actor, hash, client) }()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, identity.ErrCurrentPassword) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("concurrency: %d %d", success, conflicts)
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM audit_logs WHERE action='user.password_changed' AND actor_id=? AND ip_address=? AND user_agent=?", actor.ID, client.IP, client.UserAgent); err != nil || count != 1 {
		t.Fatalf("audit: %d %v", count, err)
	}
	fresh, err := repo.ByID(ctx, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasher.Verify("replacement-password", fresh.PasswordHash) || hasher.Verify("original-password", fresh.PasswordHash) {
		t.Fatal("hash verification")
	}
	// An audit failure must roll back the password update.
	db.MustExec("CREATE TRIGGER fail_password_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test audit failure'")
	other, err := hasher.Hash("third-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePassword(ctx, fresh, other, client); err == nil {
		t.Fatal("expected rollback")
	}
	after, err := repo.ByID(ctx, actor.ID)
	if err != nil || after.PasswordHash != fresh.PasswordHash {
		t.Fatal("password changed without audit")
	}
}
