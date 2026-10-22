package integration

import (
	"bytes"
	"context"
	"encoding/json"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/happyro/happyro-api/internal/config"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	"github.com/happyro/happyro-api/internal/transport/http"
	"go.uber.org/zap"
)

type successfulGateway struct{ calls int }

func (*successfulGateway) Capabilities(context.Context) (gameapi.Capabilities, error) {
	return gameapi.Capabilities{ProtocolVersion: "1", Commands: []string{"character.stats.reset"}}, nil
}
func (*successfulGateway) BattleConfig(context.Context) (map[string]int64, error) {
	return map[string]int64{"base_exp_rate": 100}, nil
}
func (*successfulGateway) CharacterSnapshot(context.Context, int64) (map[string]any, error) {
	return map[string]any{"char_id": json.Number("150000")}, nil
}
func (g *successfulGateway) Execute(_ context.Context, c gameapi.Command) (map[string]any, error) {
	g.calls++
	return map[string]any{"char_id": 150000, "operation": c.Type}, nil
}

func TestCommandHTTPValidatesAuthorizesAndReplaysPersistedResult(t *testing.T) {
	db := testDatabase(t)
	repo := &identityapi.SQLRepository{DB: db}
	identity := identityapi.NewService(repo, identityapi.Argon2id{})
	auth, err := sessionauth.NewAdminService(repo, identityapi.Argon2id{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Provision(context.Background(), identityapi.NewUser{Username: "admin", Password: "admin", Role: "super_admin"}, true); err != nil {
		t.Fatal(err)
	}
	importer := catalog.Importer{Reader: catalog.JSONReader{}, Publisher: &catalog.SQLPublisher{DB: db}}
	if _, err = importer.Import(context.Background(), catalogPaths(t)); err != nil {
		t.Fatal(err)
	}
	catalogs := &catalog.Service{Repository: &catalog.SQLRepository{DB: db, Aliases: map[string]string{"Kafra": "카프라"}}, Assets: catalog.FilesystemAssets{}}
	gateway := &successfulGateway{}
	sessions := scs.New()
	sessions.Cookie.Secure = false
	app := &httptransport.Server{Catalog: catalogs, Audit: &audit.SQLStore{DB: db}, Auth: auth, GameGateway: gateway, GameCommands: &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: time.Second}, Sessions: sessions, Logger: zap.NewNop(), Config: config.Config{PublicURL: "http://admin.local", SessionLifetime: time.Hour, RememberLifetime: 24 * time.Hour}}
	game := gameDatabase(t)
	if err = prepareDeletions(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	if err = prepareMail(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	if _, err = game.Exec("INSERT INTO login (account_id,userid,user_pass,email,sex) VALUES (2000000,'test_one','secret','one@example.com','M'),(2000001,'testXone','secret','two@example.com','F')"); err != nil {
		t.Fatal(err)
	}
	if _, err = game.Exec("INSERT INTO `char` (char_id,account_id,name,sex) VALUES (150000,2000000,'测试角色','M'),(150001,2000001,'另一个角色','F')"); err != nil {
		t.Fatal(err)
	}
	records := &rewardsapi.SQLRecords{DB: db}
	app.Grants = &rewardsapi.Service{Items: catalogs, Records: records, Mail: &rewardsapi.SQLMail{DB: game}, Commands: app.GameCommands, Audit: &audit.SQLStore{DB: db}}
	app.GrantLookup = &rewardsapi.Lookup{Items: catalogs, Targets: &rewardsapi.SQLTargets{DB: game}}
	app.GrantHistory = records
	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	csrf := ""
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-XSRF-TOKEN", csrf)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.StatusCode, status, raw)
		}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "XSRF-TOKEN" {
				csrf = cookie.Value
			}
		}
		return raw
	}
	request("GET", "/api/operations/game-control/capabilities", "", 401)
	request("GET", "/api/auth/csrf", "", 204)
	request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, 200)
	for _, path := range []string{"/api/game-data/items?range=all", "/api/game-data/items/501", "/api/game-data/monsters", "/api/game-data/monsters/1002", "/api/game-data/npcs?query=Kafra", "/api/game-data/maps/prontera/npcs", "/api/settings/game-data", "/api/settings/game-data/history"} {
		body := request("GET", path, "", 200)
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if result["success"] != true || result["data"] == nil {
			t.Fatalf("invalid catalog response for %s: %s", path, body)
		}
	}
	request("GET", "/api/game-data/items?range=invalid", "", 422)
	request("GET", "/api/game-data/monsters?kind=invalid", "", 422)
	request("GET", "/api/game-data/npcs?visibility=invalid", "", 422)
	request("GET", "/api/game-data/items/999999", "", 404)
	valid := `{"idempotency_key":"http-reset","type":"character.stats.reset","target":{"type":"character","id":"150000"},"payload":{}}`
	raw := request("POST", "/api/operations/game-control/commands", valid, 202)
	var envelope struct{ Data gameapi.Command }
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Status != "succeeded" || envelope.Data.RequestedBy == nil {
		t.Fatalf("wrong persisted result: %s", raw)
	}
	request("POST", "/api/operations/game-control/commands", valid, 200)
	request("GET", "/api/operations/game-control/commands/"+envelope.Data.ID, "", 200)
	request("POST", "/api/operations/game-control/commands", `{"idempotency_key":"bad","type":"battle_config.apply","target":{"type":"character","id":"150000"},"payload":{}}`, 422)
	request("POST", "/api/operations/game-control/commands", valid+` {}`, 422)
	if gateway.calls != 1 {
		t.Fatalf("game command executed %d times", gateway.calls)
	}
	itemSearch := request("GET", "/api/operations/item-grant-items?target=501", "", 200)
	var items struct {
		Data []struct {
			ID int64 `json:"item_id"`
		}
	}
	if err = json.Unmarshal(itemSearch, &items); err != nil || len(items.Data) != 1 || items.Data[0].ID != 501 {
		t.Fatalf("item search %s: %v", itemSearch, err)
	}
	targetSearch := request("GET", "/api/operations/item-grant-targets?target=test_one", "", 200)
	var targets struct {
		Data []struct {
			ID int64 `json:"char_id"`
		}
	}
	if err = json.Unmarshal(targetSearch, &targets); err != nil || len(targets.Data) != 1 || targets.Data[0].ID != 150000 {
		t.Fatalf("literal target search %s: %v", targetSearch, err)
	}
	request("GET", "/api/operations/item-grant-targets?target=", "", 422)
	mail := `{"idempotency_key":"http-mail","item_id":501,"char_id":150000,"amount":2,"title":"奖励","message":"测试"}`
	firstMail := request("POST", "/api/operations/item-grants/mail", mail, 201)
	if string(request("POST", "/api/operations/item-grants/mail", mail, 201)) != string(firstMail) {
		t.Fatal("mail replay changed result")
	}
	request("POST", "/api/operations/item-grants/mail", `{"idempotency_key":"http-mail","item_id":501,"char_id":150000,"amount":1,"title":"奖励","message":"测试"}`, 409)
	request("POST", "/api/operations/item-grants/mail", `{"idempotency_key":"http-missing","item_id":999,"char_id":150000,"amount":1,"title":"奖励","message":"测试"}`, 422)
	request("POST", "/api/operations/item-grants/mail", `{"idempotency_key":"http-bound","item_id":501,"char_id":150000,"amount":1,"delivery":"inventory","bound":true}`, 422)
	inventory := `{"idempotency_key":"http-inventory","item_id":501,"char_id":150000,"amount":1,"delivery":"inventory"}`
	for range 2 {
		request("POST", "/api/operations/item-grants/mail", inventory, 201)
	}
	zeny := `{"idempotency_key":"http-zeny","char_id":150000,"amount":100}`
	for range 2 {
		request("POST", "/api/operations/zeny-grants", zeny, 200)
	}
	if gateway.calls != 3 {
		t.Fatalf("grants replayed: gateway calls=%d", gateway.calls)
	}
	request("GET", "/api/operations/item-grants", "", 200)
	request("GET", "/api/operations/item-grants?per_page=0", "", 422)
	countRows(t, game, "mail", 1)
	countRows(t, game, "mail_attachments", 2)
	var grantAudits int
	if err = db.Get(&grantAudits, "SELECT COUNT(*) FROM audit_logs WHERE operation_key LIKE 'command:%'"); err != nil || grantAudits != 2 {
		t.Fatalf("command audits %d: %v", grantAudits, err)
	}
	if _, err = game.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','test_one')"); err != nil {
		t.Fatal(err)
	}
	blockedMail := request("POST", "/api/operations/item-grants/mail", `{"idempotency_key":"http-deleting","item_id":501,"char_id":150000,"amount":1,"title":"奖励","message":"测试"}`, 409)
	if !bytes.Contains(blockedMail, []byte(`"code":"account_deleting"`)) {
		t.Fatal(string(blockedMail))
	}
	countRows(t, game, "mail", 1)
	if _, err = db.Exec("DELETE FROM user_roles WHERE user_id=?", *envelope.Data.RequestedBy); err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/operations/item-grants/mail", mail, 403)
	request("POST", "/api/operations/zeny-grants", zeny, 403)
	request("GET", "/api/operations/item-grants", "", 403)
	request("POST", "/api/operations/game-control/commands", valid, 403)
	request("GET", "/api/operations/game-control/commands/"+envelope.Data.ID, "", 403)
}
