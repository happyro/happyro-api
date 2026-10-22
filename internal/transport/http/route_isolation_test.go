package httptransport

import (
	"errors"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexedwards/scs/v2"
)

type unavailableSessionStore struct {
	scs.Store
	reads int
}

func (s *unavailableSessionStore) Find(string) ([]byte, bool, error) {
	s.reads++
	return nil, false, errors.New("session database unavailable")
}

func TestPlayerAPIAndHealthDoNotLoadAdministratorSessions(t *testing.T) {
	store := &unavailableSessionStore{}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("frontend"), 0644); err != nil {
		t.Fatal(err)
	}
	b, _ := newBrowser(t, func(s *Server) {
		s.Config.FrontendDir = root
		store.Store = s.Sessions.Store
		s.Sessions.Store = store
		s.PlayerLimits = &testAdventureLimit{}
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}, AdminGroups: []int{99}}
		s.PlayerAccess = &sessionauth.PlayerAccess{Gateway: &adventureGateway{policy: 2}}
	})
	for _, path := range []string{"/", "/api/health", "/api/public-config", "/api/adventure-tools/bootstrap", "/api/missing"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "happyro_admin_session", Value: "stale-admin-session"})
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		expected := http.StatusOK
		if path == "/api/missing" {
			expected = http.StatusNotFound
		}
		expectStatus(t, w, expected)
		if store.reads != 0 {
			t.Fatalf("%s loaded administrator sessions", path)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatalf("%s issued session cookies", path)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/auth/user", nil)
	r.AddCookie(&http.Cookie{Name: "happyro_admin_session", Value: "stale-admin-session"})
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	expectStatus(t, w, http.StatusInternalServerError)
	if store.reads != 1 {
		t.Fatalf("administrator endpoint did not load its session: %d", store.reads)
	}
}

func TestGameCredentialsCannotAuthorizeAdministratorRoutes(t *testing.T) {
	b, _ := newBrowser(t)
	for _, path := range []string{"/api/auth/user", "/api/players/accounts", "/api/settings/game-settings", "/api/operations/game-control/capabilities"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		expected := http.StatusUnauthorized
		expectStatus(t, w, expected)
	}
	// The administrator write path still requires CSRF, even with game headers.
	r := httptest.NewRequest(http.MethodDelete, "/api/players/accounts/2000000", nil)
	r.Header.Set("X-HappyRO-Auth-Token", "game-token")
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	expectStatus(t, w, 419)
}
