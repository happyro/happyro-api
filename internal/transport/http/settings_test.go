package httptransport

import (
	"context"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"net/http/httptest"
	"strings"
	"testing"
)

type settingsGateway struct {
	gameapi.Gateway
	policy int64
}

func (g *settingsGateway) BattleConfig(context.Context) (map[string]int64, error) {
	return map[string]int64{"game_tools_game_settings_policy": g.policy, "base_exp_rate": 250, "private_setting": 99}, nil
}
func TestSettingsReadsSeparateAdminPermissionAndAdventureGovernance(t *testing.T) {
	g := &settingsGateway{policy: 2}
	b, repo := newBrowser(t, func(s *Server) {
		s.GameGateway = g
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerLimits = &testAdventureLimit{}
	})
	expectStatus(t, b.request("GET", "/api/settings/game-settings", "", "", false), 401)
	b.login(t)
	w := b.request("GET", "/api/settings/game-settings", "", "", false)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"base_exp_rate":250`) || strings.Contains(w.Body.String(), "private_setting") {
		t.Fatal(w.Body.String())
	}
	repo.user.Permissions = []string{"players.view"}
	expectStatus(t, b.request("GET", "/api/settings/game-settings", "", "", false), 403)
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/adventure-tools/game-settings", nil)
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		return w
	}
	w = get()
	expectStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "game_tools_game_settings_policy") || strings.Contains(w.Body.String(), `"source"`) || !strings.Contains(w.Body.String(), `"base_exp_rate":250`) {
		t.Fatal(w.Body.String())
	}
	g.policy = 1
	expectStatus(t, get(), 403)
}

type settingUpdates struct {
	calls int
	actor gameapi.Actor
	input settings.Update
}

func (u *settingUpdates) Apply(_ context.Context, input settings.Update, actor gameapi.Actor) (settings.Revision, error) {
	u.calls++
	u.actor = actor
	u.input = input
	return settings.Revision{ID: "revision", Status: "applied"}, nil
}
func TestSettingsUpdateRequiresCSRFAndCorrectActor(t *testing.T) {
	updates := &settingUpdates{}
	b, repo := newBrowser(t, func(s *Server) { s.Settings = updates })
	b.login(t)
	body := `{"idempotency_key":"change","changes":{"base_exp_rate":200}}`
	expectStatus(t, b.request("PUT", "/api/settings/game-settings", body, "", false), 419)
	expectStatus(t, b.request("PUT", "/api/settings/game-settings", body, "", true), 200)
	if updates.calls != 1 || updates.actor.AdminUserID == nil || *updates.actor.AdminUserID != repo.user.ID || updates.actor.GameAccountID != nil || updates.input.Key != "change" {
		t.Fatalf("wrong actor/input %+v", updates)
	}
	expectStatus(t, b.request("PUT", "/api/settings/game-settings", `{"changes":{"base_exp_rate":1.5}}`, "", true), 422)
	repo.user.Permissions = []string{"players.view"}
	expectStatus(t, b.request("PUT", "/api/settings/game-settings", body, "", true), 403)
	expectStatus(t, b.request("GET", "/api/settings/game-settings/history", "", "", false), 403)
	if updates.calls != 1 {
		t.Fatal("invalid request reached settings service")
	}
}

type settingResume struct {
	calls int
	id    string
	user  identityapi.User
	err   error
}

func (r *settingResume) Resume(_ context.Context, id string, user identityapi.User, _ identityapi.Client) (settings.Revision, error) {
	r.calls++
	r.id, r.user = id, user
	return settings.Revision{ID: id, Status: "applied"}, r.err
}
func TestSettingsResumeEnforcesSessionPermissionAndCSRF(t *testing.T) {
	recovery := &settingResume{}
	b, repo := newBrowser(t, func(s *Server) { s.SettingsRecovery = recovery })
	path := "/api/settings/game-settings/revisions/revision-one/resume"
	// An anonymous browser can obtain a CSRF token but cannot recover settings.
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	expectStatus(t, b.request("POST", path, "", "", true), 401)
	b.login(t)
	expectStatus(t, b.request("POST", path, "", "", false), 419)
	expectStatus(t, b.request("POST", path, "", "http://foreign.local", true), 403)
	repo.user.Permissions = []string{"players.view"}
	expectStatus(t, b.request("POST", path, "", "", true), 403)
	if recovery.calls != 0 {
		t.Fatal("unauthorized recovery reached service")
	}
	repo.user.Permissions = []string{"settings.manage"}
	w := b.request("POST", path, "", "", true)
	expectStatus(t, w, 200)
	if recovery.calls != 1 || recovery.id != "revision-one" || recovery.user.ID != repo.user.ID || !strings.Contains(w.Body.String(), `"status":"applied"`) {
		t.Fatalf("incorrect recovery: %+v %s", recovery, w.Body.String())
	}
	recovery.err = settings.ErrRevisionNotFound
	w = b.request("POST", path, "", "", true)
	expectStatus(t, w, 404)
	if !strings.Contains(w.Body.String(), `"code":"revision_not_found"`) {
		t.Fatal(w.Body.String())
	}
	recovery.err = settings.ErrUnconfirmed
	w = b.request("POST", path, "", "", true)
	expectStatus(t, w, 409)
	if !strings.Contains(w.Body.String(), `"code":"settings_unconfirmed"`) || strings.Contains(w.Body.String(), `"status":"applied"`) {
		t.Fatal(w.Body.String())
	}
}
