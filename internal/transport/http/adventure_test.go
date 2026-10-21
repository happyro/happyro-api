package httptransport

import (
	"context"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	gameapi "github.com/happyro/happyro-api/internal/game"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"net/http/httptest"
	"strings"
	"testing"
)

type gameIdentity struct{}

func (gameIdentity) OnlineIdentity(_ context.Context, a, c int64) (sessionauth.GameIdentity, error) {
	if a != 2000000 || c != 150000 {
		return sessionauth.GameIdentity{}, sessionauth.ErrGameSession
	}
	return sessionauth.GameIdentity{Token: "game-token", GroupID: 0}, nil
}

type adventureGateway struct {
	gameapi.Gateway
	policy int64
	seen   int64
}

func (g *adventureGateway) BattleConfig(context.Context) (map[string]int64, error) {
	return map[string]int64{sessionauth.CharacterMaintenancePolicy: g.policy}, nil
}
func (g *adventureGateway) CharacterSnapshot(_ context.Context, id int64) (map[string]any, error) {
	g.seen = id
	return map[string]any{"allocation_limits": map[string]int{"max_stat": 130}}, nil
}
func TestAdventureReadsRequireOwnedGameSessionAndLivePolicy(t *testing.T) {
	g := &adventureGateway{policy: 2}
	b, _ := newBrowser(t, func(s *Server) {
		s.PlayerLimits = &testAdventureLimit{}
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}, AdminGroups: []int{99}}
		s.PlayerAccess = &sessionauth.PlayerAccess{Gateway: g}
		s.PlayerCharacters = &charactersapi.PlayerService{Access: s.PlayerAccess, Gateway: g}
	})
	request := func(path, token, character string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", character)
		r.Header.Set("X-HappyRO-Auth-Token", token)
		response := httptest.NewRecorder()
		b.handler.ServeHTTP(response, r)
		return response
	}
	b.login(t)
	expectStatus(t, b.request("GET", "/api/adventure-tools/bootstrap", "", "", false), 401)
	expectStatus(t, request("/api/adventure-tools/bootstrap", "wrong", "150000"), 401)
	expectStatus(t, request("/api/adventure-tools/bootstrap", "game-token", "150001"), 401)
	response := request("/api/adventure-tools/bootstrap", "game-token", "150000")
	expectStatus(t, response, 200)
	if !strings.Contains(response.Body.String(), `"characterMaintenanceAllowed":true`) || !strings.Contains(response.Body.String(), `"gameSettingsAllowed":false`) {
		t.Fatal(response.Body.String())
	}
	expectStatus(t, request("/api/adventure-tools/character?character_id=150001", "game-token", "150000"), 200)
	if g.seen != 150000 {
		t.Fatal("untrusted target used")
	}
	g.policy = 1
	expectStatus(t, request("/api/adventure-tools/character", "game-token", "150000"), 403)
	response = request("/api/adventure-tools/character/allocation-limits", "game-token", "150000")
	expectStatus(t, response, 200)
	if !strings.Contains(response.Body.String(), `"max_stat":130`) {
		t.Fatal(response.Body.String())
	}
}

type adventureCommands struct {
	calls   int
	request gameapi.Request
	actor   gameapi.Actor
	err     error
}

func (s *adventureCommands) Submit(_ context.Context, r gameapi.Request, a gameapi.Actor) (gameapi.Command, bool, error) {
	s.calls++
	s.request = r
	s.actor = a
	return gameapi.Command{}, true, s.err
}
func TestAdventureMaintenanceRejectsForeignTargetsAndOperatorCommands(t *testing.T) {
	g := &adventureGateway{policy: 2}
	commands := &adventureCommands{}
	b, _ := newBrowser(t, func(s *Server) {
		s.PlayerLimits = &testAdventureLimit{}
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerAccess = &sessionauth.PlayerAccess{Gateway: g}
		s.PlayerCharacters = &charactersapi.PlayerService{Access: s.PlayerAccess, Gateway: g, Commands: commands}
	})
	post := func(body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/adventure-tools/character/commands", strings.NewReader(body))
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", token)
		response := httptest.NewRecorder()
		b.handler.ServeHTTP(response, r)
		return response
	}
	valid := `{"idempotency_key":"reset","type":"character.stats.reset"}`
	expectStatus(t, post(valid, ""), 401)
	for _, body := range []string{
		`{"idempotency_key":"reset","type":"character.stats.reset","target":{"type":"character","id":"150001"}}`,
		`{"idempotency_key":"reset","type":"monster.spawn","payload":{"monster_id":1002}}`,
		`{"idempotency_key":"reset","type":"character.navigation.teleport","payload":{"map":"prontera"}}`,
		`{"idempotency_key":"reset","type":"character.stats.update","payload":{"str":0}}`,
		`{"idempotency_key":"reset","type":"character.stats.reset","payload":{"extra":1}}`,
		`{"idempotency_key":"","type":"character.stats.reset"}`,
	} {
		expectStatus(t, post(body, "game-token"), 422)
	}
	if commands.calls != 0 {
		t.Fatal("invalid command submitted")
	}
	expectStatus(t, post(valid, "game-token"), 200)
	if commands.calls != 1 || commands.request.Target.ID != "150000" || commands.actor.GameAccountID == nil || *commands.actor.GameAccountID != 2000000 || commands.actor.AdminUserID != nil {
		t.Fatalf("wrong target or actor: %+v", commands)
	}
	g.policy = 0
	expectStatus(t, post(valid, "game-token"), 403)
	if commands.calls != 1 {
		t.Fatal("disabled policy ignored")
	}
	g.policy = 2
	commands.err = gameapi.ErrConflict
	expectStatus(t, post(valid, "game-token"), 409)
	commands.err = gameapi.ErrRunning
	expectStatus(t, post(valid, "game-token"), 409)
}

type testAdventureLimit struct{ retry int }

func (s *testAdventureLimit) Allow(context.Context, int64, string, int) (int, error) {
	return s.retry, nil
}
func TestAdventureRateLimitReturnsRetryDelay(t *testing.T) {
	b, _ := newBrowser(t, func(s *Server) {
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerLimits = &testAdventureLimit{retry: 42}
	})
	r := httptest.NewRequest("GET", "/api/adventure-tools/bootstrap", nil)
	r.Header.Set("X-HappyRO-Account-ID", "2000000")
	r.Header.Set("X-HappyRO-Character-ID", "150000")
	r.Header.Set("X-HappyRO-Auth-Token", "game-token")
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	expectStatus(t, w, 429)
	if w.Header().Get("Retry-After") != "42" {
		t.Fatal("missing retry delay")
	}
}
