package integration

import (
	"context"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/happyro/happyro-api/internal/rewards"
	"testing"
	"time"
)

func TestTimestampProjectionsAreUnambiguousUTC(t *testing.T) {
	ctx := context.Background()
	db := gameDatabase(t)
	if err := prepareDeletions(ctx, db); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO login(account_id,userid,user_pass,email,sex,lastlogin) VALUES(2000000,'time_test','unused','time@example.com','M','2026-10-22 20:01:02')`,
		"INSERT INTO `char`(char_id,account_id,name,sex,last_login) VALUES(150000,2000000,'Time test','M','2026-10-22 20:01:02')",
		`INSERT INTO loginlog(time,ip,user,rcode,log) VALUES('2026-10-22 20:01:02','127.0.0.1','time_test',0,'ok')`,
		`INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name,created_at,updated_at) VALUES(2000000,1,'admin','time_test','2026-10-22 20:01:02.123456','2026-10-22 20:01:02.123456')`,
		`INSERT INTO audit_logs(action,metadata,created_at) VALUES('time_test','{}','2026-10-22 20:01:02.123456')`,
		`INSERT INTO mail_grants(idempotency_key,request_hash,item_id,char_id,amount,title,requested_by,prepared,created_at,updated_at) VALUES('time-test','hash',501,150000,1,'Time test',1,'{}','2026-10-22 20:01:02.123456','2026-10-22 20:01:02.123456')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	p := database.Pagination{Page: 1, Size: 20}
	reader := &accountsapi.SQLReader{Game: db, Log: db}
	accounts, err := reader.Accounts(ctx, accountsapi.AccountQuery{Pagination: p})
	if err != nil {
		t.Fatal(err)
	}
	character, err := (&charactersapi.SQLReader{Game: db}).Character(ctx, 150000)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := reader.LoginLogs(ctx, accountsapi.LoginLogQuery{Pagination: p})
	if err != nil {
		t.Fatal(err)
	}
	deletions, err := (&accountsapi.SQLDeletions{DB: db}).List(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	history, err := (&audit.SQLStore{DB: db}).History(ctx, "time_test", p)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := (&rewards.SQLRecords{DB: db}).History(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{
		"account": accounts.Data[0]["lastlogin"], "character": character["last_login"], "login log": logs.Data[0]["time"],
		"deletion created": deletions.Data[0].Created, "deletion updated": deletions.Data[0].Updated,
		"audit": history.Data[0]["created_at"], "grant created": grants.Data[0]["created_at"], "grant updated": grants.Data[0]["updated_at"],
	}
	for name, value := range values {
		raw, ok := value.(string)
		if !ok {
			t.Fatalf("%s: %T", name, value)
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || parsed.UTC().Format("2006-01-02 15:04:05") != "2026-10-22 20:01:02" {
			t.Fatalf("%s: %q %v", name, raw, err)
		}
		_, offset := parsed.Zone()
		if offset != 0 {
			t.Fatalf("%s not UTC: %q", name, raw)
		}
	}
	if grants.Data[0]["created_at"] != "2026-10-22T20:01:02.123456Z" {
		t.Fatal("fractional seconds lost")
	}
	if _, err := db.Exec("UPDATE `char` SET last_login=NULL WHERE char_id=150000"); err != nil {
		t.Fatal(err)
	}
	character, err = (&charactersapi.SQLReader{Game: db}).Character(ctx, 150000)
	if err != nil || character["last_login"] != nil {
		t.Fatalf("never logged in: %v %v", character["last_login"], err)
	}
}
