package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/happyro/happyro-api/internal/catalog"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	"github.com/jmoiron/sqlx"
)

type grantItems struct{ kind string }

func (i grantItems) Item(context.Context, int64, string) (catalog.Object, error) {
	return catalog.Object{"Type": i.kind}, nil
}
func mailSetup(t *testing.T) (*rewardsapi.Service, *sqlx.DB, *sqlx.DB, identityapi.User) {
	t.Helper()
	ctx := context.Background()
	admin := testDatabase(t)
	game := gameDatabase(t)
	if err := prepareDeletions(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Exec("INSERT INTO `char` (char_id,account_id,name,sex) VALUES (150000,2000000,'测试角色','M')"); err != nil {
		t.Fatal(err)
	}
	identity := identityapi.NewService(&identityapi.SQLRepository{DB: admin}, identityapi.Argon2id{})
	if _, err := identity.Provision(ctx, identityapi.NewUser{Username: "admin", Password: "admin", Role: "super_admin"}, true); err != nil {
		t.Fatal(err)
	}
	if err := prepareMail(ctx, game); err != nil {
		t.Fatal(err)
	}
	if err := prepareMail(ctx, game); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := admin.Get(&id, "SELECT id FROM users WHERE username='admin'"); err != nil {
		t.Fatal(err)
	}
	return &rewardsapi.Service{Items: grantItems{"Armor"}, Records: &rewardsapi.SQLRecords{DB: admin}, Mail: &rewardsapi.SQLMail{DB: game}}, admin, game, identityapi.User{ID: id, Username: "admin"}
}
func mailInput() rewardsapi.ItemRequest {
	return rewardsapi.ItemRequest{Key: "mail-retry", ItemID: 501, CharacterID: 150000, Amount: 3, Title: "奖励", Message: "测试内容", Bound: true}
}
func countRows(t *testing.T, db *sqlx.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.Get(&got, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d want=%d", table, got, want)
	}
}
func TestConcurrentMailAndRetryAfterCollectionNeverDuplicate(t *testing.T) {
	s, admin, game, user := mailSetup(t)
	ctx := context.Background()
	input := mailInput()
	const concurrency = 8
	ids := make(chan int64, concurrency)
	failures := make(chan error, concurrency)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() { id, err := s.MailItem(ctx, input, user, identityapi.Client{}); ids <- id; failures <- err })
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var original int64
	for id := range ids {
		if original != 0 && id != original {
			t.Fatalf("duplicate ids %d/%d", original, id)
		}
		original = id
	}
	countRows(t, game, "mail", 1)
	countRows(t, game, "mail_attachments", 3)
	countRows(t, game, "happyro_admin_mail_receipts", 1)
	countRows(t, admin, "mail_grants", 1)
	var audits int
	if err := admin.Get(&audits, "SELECT COUNT(*) FROM audit_logs WHERE action='operations.item_mailed'"); err != nil || audits != 1 {
		t.Fatalf("audit count %d: %v", audits, err)
	}
	var amount, bound int
	if err := game.QueryRow("SELECT SUM(amount),MIN(bound) FROM mail_attachments").Scan(&amount, &bound); err != nil || amount != 3 || bound != 1 {
		t.Fatalf("attachments amount=%d bound=%d err=%v", amount, bound, err)
	}
	input.Amount = 2
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); !errors.Is(err, rewardsapi.ErrConflict) {
		t.Fatalf("changed payload accepted: %v", err)
	}
	input = mailInput()
	other := user
	other.ID++
	if _, err := s.MailItem(ctx, input, other, identityapi.Client{}); !errors.Is(err, rewardsapi.ErrConflict) {
		t.Fatalf("changed actor accepted: %v", err)
	}
	if _, err := game.Exec("DELETE FROM mail"); err != nil {
		t.Fatal(err)
	}
	// Simulate an admin-record restore after the game already delivered and collected mail.
	if _, err := admin.Exec("DELETE FROM mail_grants"); err != nil {
		t.Fatal(err)
	}
	id, err := s.MailItem(ctx, input, user, identityapi.Client{})
	if err != nil || id != original {
		t.Fatalf("collected replay=%d err=%v", id, err)
	}
	countRows(t, game, "mail", 0)
	countRows(t, game, "mail_attachments", 0)
}
func TestMailRollsBackAttachmentsAndCanRecoverAfterAdminWriteFailure(t *testing.T) {
	s, admin, game, user := mailSetup(t)
	ctx := context.Background()
	input := mailInput()
	if _, err := game.Exec("CREATE TRIGGER reject_attachment BEFORE INSERT ON mail_attachments FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected attachment failure'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); err == nil {
		t.Fatal("expected attachment failure")
	}
	countRows(t, game, "mail", 0)
	countRows(t, game, "happyro_admin_mail_receipts", 0)
	if _, err := game.Exec("DROP TRIGGER reject_attachment"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE TRIGGER reject_grant_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); err == nil {
		t.Fatal("expected audit failure")
	}
	countRows(t, game, "mail", 1)
	countRows(t, game, "mail_attachments", 3)
	var status string
	if err := admin.Get(&status, "SELECT status FROM mail_grants"); err != nil || status != "pending" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	if _, err := admin.Exec("DROP TRIGGER reject_grant_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	countRows(t, game, "mail", 1)
	history, err := (&rewardsapi.SQLRecords{DB: admin}).History(ctx, database.Pagination{Page: 1, Size: 20}, "")
	if err != nil || history.Meta.Total != 1 || history.Data[0]["status"] != "sent" {
		t.Fatalf("history %+v %v", history, err)
	}
}
func TestMailRequiresExistingCharacterAndTransactionalTables(t *testing.T) {
	s, _, game, user := mailSetup(t)
	ctx := context.Background()
	input := mailInput()
	input.CharacterID++
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); !errors.Is(err, rewardsapi.ErrCharacter) {
		t.Fatalf("missing char: %v", err)
	}
	countRows(t, game, "mail", 0)
	countRows(t, game, "happyro_admin_mail_receipts", 0)
	if _, err := game.Exec("ALTER TABLE happyro_admin_mail_receipts ENGINE=MyISAM"); err != nil {
		t.Fatal(err)
	}
	input = mailInput()
	input.Key = "engine-check"
	if _, err := s.MailItem(ctx, input, user, identityapi.Client{}); err == nil {
		t.Fatal("unsafe engine accepted")
	}
	countRows(t, game, "mail", 0)
}
