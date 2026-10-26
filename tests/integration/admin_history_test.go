package integration

import (
	"context"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	"github.com/happyro/happyro-api/internal/settings"
	"testing"
)

func TestUnifiedGrantHistoryFiltersAndPaginatesAcrossSources(t *testing.T) {
	service, db, _, user := mailSetup(t)
	ctx := context.Background()
	if _, err := service.MailItem(ctx, mailInput(), user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id, kind, status, payload string
		actor                     any
		player                    any
	}{
		{"inventory-one", "character.inventory.item_grant", "succeeded", `{"item_id":501,"amount":7}`, user.ID, nil},
		{"zeny-two", "character.currency.zeny_grant", "indeterminate", `{"amount":12345}`, nil, 2000000},
		{"unrelated", "account.disconnect", "succeeded", `{}`, user.ID, nil},
	} {
		_, err := db.Exec(`INSERT INTO game_server_commands(id,idempotency_key,request_hash,type,status,target_type,target_id,payload,requested_by,requested_game_account_id,created_at) VALUES(?,?,'hash',?,?,'character','150000',?,?,?,'2030-01-01')`, row.id, row.id, row.kind, row.status, row.payload, row.actor, row.player)
		if err != nil {
			t.Fatal(err)
		}
	}
	history := &rewardsapi.SQLRecords{DB: db}
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		got, err := history.History(ctx, database.Pagination{Page: page, Size: 1}, "")
		if err != nil || got.Meta.Total != 3 || len(got.Data) != 1 {
			t.Fatalf("page %d: %+v %v", page, got, err)
		}
		id := got.Data[0]["id"].(string)
		if seen[id] {
			t.Fatal("duplicate page record", id)
		}
		seen[id] = true
	}
	for _, kind := range []string{"mail", "inventory", "zeny"} {
		got, err := history.History(ctx, database.Pagination{Page: 1, Size: 20}, kind)
		if err != nil || got.Meta.Total != 1 || got.Data[0]["kind"] != kind {
			t.Fatalf("filter %s: %+v %v", kind, got, err)
		}
		row := got.Data[0]
		if kind == "zeny" && (row["status"] != "indeterminate" || row["amount"] != int64(12345) && row["amount"] != uint64(12345)) {
			t.Fatalf("zeny data %+v", row)
		}
		if kind == "inventory" && row["requester"] == nil {
			t.Fatal("missing administrator")
		}
	}
	if _, err := history.History(ctx, database.Pagination{Page: 1, Size: 20}, "bad"); err == nil {
		t.Fatal("invalid filter accepted")
	}
}

func TestRegistrationAndBattleHistoryIncludesStartupAndUnresolvedWrites(t *testing.T) {
	_, db, game, user := mailSetup(t)
	ctx := context.Background()
	log := &audit.SQLStore{DB: db}
	service := &accountsapi.Management{Registration: &accountsapi.SQLWriter{DB: game}, Audit: log, RegistrationDefaultGroupEditable: true}
	gm, zero := int64(99), int64(0)
	if err := service.ApplyRegistrationDefault(ctx, &gm); err != nil {
		t.Fatal(err)
	}
	user.Permissions = []string{"settings.manage"}
	if _, err := service.UpdateRegistration(ctx, accountsapi.RegistrationUpdate{DefaultGroupID: &zero, ExpectedGroupID: &gm}, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	if err := log.Write(ctx, audit.Event{OperationKey: "interrupted:requested", ActorID: &user.ID, Username: user.Username, Action: "registration.group_changed.requested", Metadata: map[string]any{"previous_group_id": 0, "default_group_id": 99}}); err != nil {
		t.Fatal(err)
	}
	revisions := &settings.SQLRevisions{DB: db}
	lock, err := revisions.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lock.Create(ctx, "battle-change", "hash", map[string]int64{"base_exp_rate": 200}, "", "", gameapi.Actor{AdminUserID: &user.ID})
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	got, err := revisions.History(ctx, 1, 20)
	if err != nil || got.Meta["total"] != 4 {
		t.Fatalf("history %+v %v", got, err)
	}
	applied, uncertain, battle := 0, 0, 0
	for _, row := range got.Data {
		if row.Category == "battle" {
			battle++
			continue
		}
		if row.ActorName == nil || len(row.PreviousChanges) == 0 {
			t.Fatalf("missing audit details %+v", row)
		}
		if row.Status == "applied" {
			applied++
		} else if row.Status == "uncertain" {
			uncertain++
		}
	}
	if applied != 2 || uncertain != 1 || battle != 1 {
		t.Fatalf("counts %d %d %d", applied, uncertain, battle)
	}
	first, err := revisions.History(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := revisions.History(ctx, 2, 1)
	if err != nil || first.Data[0].ID == second.Data[0].ID || second.Meta["total"] != 4 {
		t.Fatalf("pagination %+v %v", second, err)
	}
}
