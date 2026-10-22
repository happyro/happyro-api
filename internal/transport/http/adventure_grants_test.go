package httptransport

import (
	"context"
	"github.com/happyro/happyro-api/internal/catalog"
	gameapi "github.com/happyro/happyro-api/internal/game"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"net/http/httptest"
	"strings"
	"testing"
)

type rewardGateway struct {
	gameapi.Gateway
	policy int64
}

func (g *rewardGateway) BattleConfig(context.Context) (map[string]int64, error) {
	return map[string]int64{sessionauth.RewardPolicy: g.policy}, nil
}

type rewardItems struct{ kind string }

func (r *rewardItems) Item(context.Context, int64, string) (catalog.Object, error) {
	return catalog.Object{"Type": r.kind}, nil
}
func TestAdventureRewardsUseOwnCharacterAndSharedGrantValidation(t *testing.T) {
	gateway := &rewardGateway{policy: 2}
	commands := &adventureCommands{}
	items := &rewardItems{kind: "Weapon"}
	service := &rewardsapi.Service{Items: items, Commands: commands, Audit: playerAudit{}}
	b, _ := newBrowser(t, func(s *Server) {
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerLimits = &testAdventureLimit{}
		s.PlayerAccess = &sessionauth.PlayerAccess{Gateway: gateway}
		s.PlayerRewards = &rewardsapi.PlayerService{Access: s.PlayerAccess, Rewards: service}
	})
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/adventure-tools"+path, strings.NewReader(body))
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{
		`{"idempotency_key":"grant","target":{"type":"self","id":150001},"item_id":501,"amount":1}`,
		`{"idempotency_key":"grant","target":{"type":"character"},"item_id":501,"amount":1}`,
		`{"idempotency_key":"grant","target":{"type":"self"},"item_id":501,"amount":30001}`,
		`{"idempotency_key":"grant","target":{"type":"self"},"item_id":501,"amount":0}`,
	} {
		expectStatus(t, post("/items/grants", body), 422)
	}
	if commands.calls != 0 {
		t.Fatal("invalid grants executed")
	}
	valid := `{"idempotency_key":"grant","target":{"type":"self"},"item_id":501,"amount":1}`
	expectStatus(t, post("/items/grants", valid), 200)
	if commands.request.Target.ID != "150000" || commands.actor.GameAccountID == nil || *commands.actor.GameAccountID != 2000000 || commands.request.Payload["identify"] != true {
		t.Fatalf("wrong grant %+v", commands)
	}
	items.kind = "PetEgg"
	expectStatus(t, post("/items/grants", valid), 422)
	expectStatus(t, post("/currency/zeny/grants", `{"idempotency_key":"zeny","amount":2147483648}`), 422)
	expectStatus(t, post("/currency/zeny/grants", `{"idempotency_key":"zeny","amount":1,"char_id":150001}`), 422)
	expectStatus(t, post("/currency/zeny/grants", `{"idempotency_key":"zeny","amount":1}`), 200)
	if commands.request.Type != "character.currency.zeny_grant" || commands.request.Target.ID != "150000" {
		t.Fatal("wrong currency target")
	}
	gateway.policy = 0
	expectStatus(t, post("/items/grants", valid), 403)
	expectStatus(t, post("/currency/zeny/grants", `{"idempotency_key":"zeny","amount":1}`), 403)
	if commands.calls != 2 {
		t.Fatalf("unexpected commands %d", commands.calls)
	}
}
