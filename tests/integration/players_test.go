package integration

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"strings"
	"testing"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

//go:embed testdata/game-schema.sql
var gameSchema string

func gameDatabase(t *testing.T) *sqlx.DB {
	t.Helper()
	db := testDatabase(t)
	for _, statement := range strings.Split(gameSchema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestPlayerProjectionsPreserveNumbersAndHideCredentials(t *testing.T) {
	db := gameDatabase(t)
	for _, statement := range []string{
		`INSERT INTO login (account_id,userid,user_pass,email,sex) VALUES (2000000,'test_one','secret','one@example.com','M'),(2000001,'testXone','secret','two@example.com','F')`,
		"INSERT INTO `char` (char_id,account_id,name,sex,base_exp,last_map) VALUES (150000,2000000,'测试角色','M',9007199254740993,'prontera')",
		"INSERT INTO loginlog (time,ip,user,rcode,log) VALUES ('2026-10-22 00:00:00','127.0.0.1','test_one',0,'ok')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	repo := &accountsapi.SQLReader{Game: db, Log: db}
	page, err := repo.Accounts(context.Background(), accountsapi.AccountQuery{Username: "test_one", Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Data) != 1 {
		t.Fatalf("underscore acted as a wildcard: %+v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "user_pass") || strings.Contains(string(encoded), "web_auth_token") {
		t.Fatalf("sensitive field exposed: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"account_id":2000000`) {
		t.Fatalf("numeric ID became a string: %s", encoded)
	}
	row, err := (&charactersapi.SQLReader{Game: db}).Character(context.Background(), 150000)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"base_exp":9007199254740993`) {
		t.Fatalf("experience lost precision: %s", encoded)
	}
	logs, err := repo.LoginLogs(context.Background(), accountsapi.LoginLogQuery{Username: "test_one", Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil || logs.Total != 1 {
		t.Fatalf("login logs: %+v %v", logs, err)
	}
}

func TestGameIdentityRequiresOwnedOnlineCharacterAndEnabledToken(t *testing.T) {
	db := gameDatabase(t)
	if err := prepareDeletions(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO login (account_id,userid,web_auth_token,web_auth_token_enabled,group_id) VALUES (2000000,'owner','valid-token',1,99),(2000001,'other','other-token',1,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `char` (char_id,account_id,name,sex,online) VALUES (150000,2000000,'在线角色','M',1),(150001,2000001,'其他角色','M',1),(150002,2000000,'离线角色','M',0)"); err != nil {
		t.Fatal(err)
	}
	service := &sessionauth.GameAuthentication{Repository: &sessionauth.SQLGameIdentity{DB: db}, AdminGroups: []int{99}}
	principal, err := service.Authenticate(context.Background(), 2000000, 150000, "valid-token")
	if err != nil || !principal.IsAdmin {
		t.Fatalf("valid identity rejected: %+v %v", principal, err)
	}
	for _, input := range []struct {
		character int64
		token     string
	}{{150001, "valid-token"}, {150002, "valid-token"}, {150000, "wrong-token"}} {
		if _, err := service.Authenticate(context.Background(), 2000000, input.character, input.token); !errors.Is(err, sessionauth.ErrGameSession) {
			t.Fatalf("invalid identity accepted: %+v %v", input, err)
		}
	}
	if _, err := db.Exec("UPDATE login SET web_auth_token=NULL WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), 2000000, 150000, "valid-token"); !errors.Is(err, sessionauth.ErrGameSession) {
		t.Fatalf("null token was not rejected: %v", err)
	}
	if _, err := db.Exec("UPDATE login SET web_auth_token='valid-token', web_auth_token_enabled=0 WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), 2000000, 150000, "valid-token"); !errors.Is(err, sessionauth.ErrGameSession) {
		t.Fatalf("disabled token accepted: %v", err)
	}
}
