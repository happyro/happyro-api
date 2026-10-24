package httptransport

import (
	"context"
	"encoding/json"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/config"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"go.uber.org/zap"
)

type identityRepository struct {
	user     identityapi.User
	attempts int
	events   []string
}

func (r *identityRepository) ByUsername(_ context.Context, name string) (identityapi.User, error) {
	if name != r.user.Username {
		return identityapi.User{}, identityapi.ErrNotFound
	}
	return r.user, nil
}
func (r *identityRepository) ByID(_ context.Context, id int64) (identityapi.User, error) {
	if id != r.user.ID {
		return identityapi.User{}, identityapi.ErrNotFound
	}
	return r.user, nil
}
func (r *identityRepository) Attempt(context.Context, string) (int, error) {
	r.attempts++
	if r.attempts > 5 {
		return 60, nil
	}
	return 0, nil
}
func (r *identityRepository) LoginSucceeded(_ context.Context, _ identityapi.User, _ identityapi.Client, _ string) error {
	r.events = append(r.events, "auth.login_succeeded")
	r.attempts = 0
	return nil
}
func (r *identityRepository) Audit(_ context.Context, event string, _ *identityapi.User, _ string, _ identityapi.Client) error {
	r.events = append(r.events, event)
	return nil
}
func (*identityRepository) Provision(context.Context, identityapi.NewUser, string, bool) (bool, error) {
	panic("unexpected provision")
}
func (*identityRepository) ChangePassword(context.Context, string, string) error {
	panic("unexpected password change")
}

type testHasher struct{}

func (testHasher) Hash(value string) (string, error) { return "test:" + value, nil }
func (testHasher) Verify(value, hash string) bool    { return hash == "test:"+value }

type browser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
}

func newBrowser(t *testing.T, configure ...func(*Server)) (*browser, *identityRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &identityRepository{user: identityapi.User{ID: 1, Username: "admin", Name: "管理员", PasswordHash: "test:admin", Active: true, Roles: []string{"super_admin"}, Permissions: []string{"*"}}}
	service, err := sessionauth.NewAdminService(repo, testHasher{})
	if err != nil {
		t.Fatal(err)
	}
	sessions := scs.New()
	sessions.Cookie.Name = "happyro_admin_session"
	sessions.Cookie.Persist = false
	app := &Server{Passwords: identityapi.NewSelfPasswordService(repo, testHasher{}), Auth: service, Sessions: sessions, Logger: zap.NewNop(), Config: config.Config{AdminPasswordChangeEnabled: true, AdminCSRFOriginCheckEnabled: true, PublicURL: "http://admin.local", SessionLifetime: 2 * time.Hour, RememberLifetime: 30 * 24 * time.Hour, InitialUsername: "admin", InitialPassword: "admin", ShowDefaultCredentials: true}}
	for _, setup := range configure {
		setup(app)
	}
	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &browser{handler: handler, cookies: map[string]*http.Cookie{}}, repo
}
func (b *browser) request(method, path, body, origin string, csrf bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://admin.local"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	for _, cookie := range b.cookies {
		request.AddCookie(cookie)
	}
	if csrf && b.cookies["XSRF-TOKEN"] != nil {
		request.Header.Set("X-XSRF-TOKEN", b.cookies["XSRF-TOKEN"].Value)
	}
	response := httptest.NewRecorder()
	b.handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
		} else {
			b.cookies[cookie.Name] = cookie
		}
	}
	return response
}
func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
}
func (b *browser) login(t *testing.T) {
	t.Helper()
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	expectStatus(t, b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "http://admin.local", true), 200)
}

func TestLoginRequiresSessionBoundCSRFAndTrustedOrigin(t *testing.T) {
	b, repo := newBrowser(t, func(s *Server) { s.Config.AdminCSRFOriginCheckEnabled = true })
	expectStatus(t, b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "", false), 419)
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	expectStatus(t, b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "https://attacker.example", true), 403)
	if repo.attempts != 0 {
		t.Fatal("untrusted requests reached authentication")
	}
}

func TestLoginRotatesSessionAndLogoutRevokesIt(t *testing.T) {
	b, _ := newBrowser(t)
	expectStatus(t, b.request("GET", "/api/auth/user", "", "", false), 401)
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	oldToken := b.cookies["happyro_admin_session"].Value
	b.login(t)
	cookie := b.cookies["happyro_admin_session"]
	if cookie.Value == oldToken {
		t.Fatal("session fixation: token did not rotate")
	}
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	response := b.request("GET", "/api/auth/user", "", "", false)
	expectStatus(t, response, 200)
	var data struct{ Data map[string]any }
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Data["username"] != "admin" || data.Data["access"] != "admin" {
		t.Fatalf("wrong identity: %s", response.Body.String())
	}
	if data.Data["password_hash"] != nil || data.Data["password"] != nil || strings.Contains(response.Body.String(), "test:admin") {
		t.Fatal("password information exposed")
	}
	stolen := *cookie
	expectStatus(t, b.request("POST", "/api/auth/logout", "", "http://admin.local", true), 204)
	b.cookies["happyro_admin_session"] = &stolen
	expectStatus(t, b.request("GET", "/api/auth/user", "", "", false), 401)
}

func TestPasswordChangeAndDisablingUserInvalidateSessions(t *testing.T) {
	for _, scenario := range []string{"password", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			b, repo := newBrowser(t)
			b.login(t)
			if scenario == "password" {
				repo.user.PasswordHash = "test:new-password"
			} else {
				repo.user.Active = false
			}
			expectStatus(t, b.request("GET", "/api/auth/user", "", "", false), 401)
		})
	}
}

func TestFailedLoginsAreLimitedAndAudited(t *testing.T) {
	b, repo := newBrowser(t)
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	for range 5 {
		expectStatus(t, b.request("POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, "", true), 422)
	}
	response := b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "", true)
	expectStatus(t, response, 429)
	if response.Header().Get("Retry-After") != "60" {
		t.Fatal("missing retry delay")
	}
	if len(repo.events) != 6 || repo.events[5] != "auth.login_blocked" {
		t.Fatalf("unexpected audit events: %v", repo.events)
	}
}

func TestRememberMeSetsPersistentCookie(t *testing.T) {
	b, _ := newBrowser(t)
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	expectStatus(t, b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin","remember":true}`, "", true), 200)
	if b.cookies["happyro_admin_session"].Expires.IsZero() {
		t.Fatal("remember-me session is not persistent")
	}
}

func TestPlayerAndMapRoutesRequireTheirOwnPermissions(t *testing.T) {
	for _, path := range []string{"/api/operations/item-grant-items", "/api/operations/item-grant-targets", "/api/operations/item-grants", "/api/players/accounts", "/api/players/characters", "/api/players/characters/150000", "/api/players/login-logs", "/api/game-data/maps", "/api/game-data/maps/prontera/image", "/api/game-data/items", "/api/game-data/items/501", "/api/game-data/items/501/icon", "/api/game-data/monsters", "/api/game-data/monsters/1002", "/api/game-data/monsters/1002/image", "/api/game-data/npcs", "/api/game-data/npcs/10/image", "/api/game-data/maps/prontera/npcs", "/api/settings/game-data", "/api/settings/game-data/history"} {
		t.Run(path, func(t *testing.T) {
			b, repo := newBrowser(t)
			expectStatus(t, b.request("GET", path, "", "", false), 401)
			b.login(t)
			repo.user.Permissions = []string{"unrelated.permission"}
			expectStatus(t, b.request("GET", path, "", "", false), 403)
		})
	}
}

func TestPlayerPaginationRejectsInvalidBoundsBeforeQuery(t *testing.T) {
	b, _ := newBrowser(t)
	b.login(t)
	for _, query := range []string{"page=0", "page=-1", "page=1000001", "page=hello", "perPage=0", "perPage=101", "perPage=hello"} {
		expectStatus(t, b.request("GET", "/api/players/accounts?"+query, "", "", false), 422)
	}
}

func TestPlayerWritesRequireViewAndEditPermissions(t *testing.T) {
	for _, route := range []struct{ method, path string }{{"GET", "/api/players/account-deletions"}, {"GET", "/api/players/accounts/2000000/deletion"}, {"POST", "/api/players/accounts/2000000/deletion/retry"}, {"DELETE", "/api/players/accounts/2000000"}, {"POST", "/api/players/accounts/batch"}, {"POST", "/api/players/accounts"}, {"PATCH", "/api/players/accounts/2000000"}, {"POST", "/api/players/accounts/2000000/password"}} {
		t.Run(route.method+route.path, func(t *testing.T) {
			b, repo := newBrowser(t)
			b.login(t)
			repo.user.Permissions = []string{"players.view"}
			expectStatus(t, b.request(route.method, route.path, `{}`, "", true), 403)
			repo.user.Permissions = []string{"players.edit"}
			expectStatus(t, b.request(route.method, route.path, `{}`, "", true), 403)
		})
	}
}

func TestDisabledOriginCheckStillRequiresCSRF(t *testing.T) {
	b, repo := newBrowser(t, func(s *Server) { s.Config.AdminCSRFOriginCheckEnabled = false })
	body := `{"username":"admin","password":"admin"}`
	expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
	expectStatus(t, b.request("POST", "/api/auth/login", body, "http://10.24.1.24:8000", false), 419)
	if repo.attempts != 0 {
		t.Fatal("missing token reached authentication")
	}
	expectStatus(t, b.request("POST", "/api/auth/login", body, "http://10.24.1.24:8000", true), 200)
}

func TestOriginSwitchControlsFetchMetadataWithoutBypassingToken(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		b, _ := newBrowser(t, func(s *Server) { s.Config.AdminCSRFOriginCheckEnabled = enabled })
		expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
		request := httptest.NewRequest("POST", "http://admin.local/api/auth/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://admin.local")
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		request.Header.Set("X-XSRF-TOKEN", b.cookies["XSRF-TOKEN"].Value)
		for _, cookie := range b.cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		b.handler.ServeHTTP(response, request)
		want := 200
		if enabled {
			want = 403
		}
		expectStatus(t, response, want)
	}
}

func (r *identityRepository) ReplacePassword(_ context.Context, user identityapi.User, hash string, _ identityapi.Client) error {
	if user.ID != r.user.ID || user.PasswordHash != r.user.PasswordHash || !r.user.Active {
		return identityapi.ErrCurrentPassword
	}
	r.user.PasswordHash = hash
	r.events = append(r.events, "user.password_changed")
	return nil
}
func TestSelfPasswordChange(t *testing.T) {
	b, r := newBrowser(t)
	b.request("GET", "/api/auth/csrf", "", "", false)
	if got := b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "http://admin.local", true); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	oldCookies := map[string]*http.Cookie{}
	for k, v := range b.cookies {
		oldCookies[k] = v
	}
	other := &browser{handler: b.handler, cookies: oldCookies}
	body := `{"currentPassword":"admin","newPassword":"changed-password","confirmPassword":"changed-password","userId":999}`
	if got := b.request("POST", "/api/auth/password", body, "http://admin.local", false); got.Code != 419 {
		t.Fatalf("csrf: %d", got.Code)
	}
	if got := b.request("POST", "/api/auth/password", strings.Replace(body, `"currentPassword":"admin"`, `"currentPassword":"wrong"`, 1), "http://admin.local", true); got.Code != 422 {
		t.Fatalf("wrong: %d", got.Code)
	}
	if r.user.PasswordHash != "test:admin" {
		t.Fatal("changed on invalid request")
	}
	if got := b.request("POST", "/api/auth/password", body, "http://admin.local", true); got.Code != 204 {
		t.Fatalf("change: %d %s", got.Code, got.Body)
	}
	if r.user.PasswordHash != "test:changed-password" {
		t.Fatal("wrong user or password")
	}
	if got := b.request("GET", "/api/auth/user", "", "", false); got.Code != 401 {
		t.Fatalf("current session: %d", got.Code)
	}
	if got := other.request("GET", "/api/auth/user", "", "", false); got.Code != 401 {
		t.Fatalf("other session: %d", got.Code)
	}
	b.request("GET", "/api/auth/csrf", "", "", false)
	if got := b.request("POST", "/api/auth/login", `{"username":"admin","password":"changed-password"}`, "http://admin.local", true); got.Code != 200 {
		t.Fatalf("new login: %d", got.Code)
	}
}
func TestSelfPasswordDisabledAndUnauthenticated(t *testing.T) {
	b, r := newBrowser(t, func(s *Server) { s.Config.AdminPasswordChangeEnabled = false })
	b.request("GET", "/api/auth/csrf", "", "", false)
	body := `{"currentPassword":"admin","newPassword":"changed-password","confirmPassword":"changed-password"}`
	if got := b.request("POST", "/api/auth/password", body, "http://admin.local", true); got.Code != 401 {
		t.Fatalf("auth: %d", got.Code)
	}
	b.request("POST", "/api/auth/login", `{"username":"admin","password":"admin"}`, "http://admin.local", true)
	if got := b.request("GET", "/api/auth/user", "", "", false); !strings.Contains(got.Body.String(), `"passwordChangeEnabled":false`) {
		t.Fatal(got.Body.String())
	}
	if got := b.request("POST", "/api/auth/password", body, "http://admin.local", true); got.Code != 403 {
		t.Fatalf("disabled: %d", got.Code)
	}
	if r.user.PasswordHash != "test:admin" {
		t.Fatal("disabled mutation")
	}
}

func TestSelfPasswordValidationAndRateLimit(t *testing.T) {
	b, r := newBrowser(t)
	b.login(t)
	r.user.Permissions = nil
	for _, body := range []string{
		`{"currentPassword":"admin","newPassword":"admin","confirmPassword":"admin"}`,
		`{"currentPassword":"admin","newPassword":"new-password","confirmPassword":"different"}`,
		`{"currentPassword":"admin","newPassword":"tiny","confirmPassword":"tiny"}`,
	} {
		expectStatus(t, b.request("POST", "/api/auth/password", body, "http://admin.local", true), 422)
	}
	body := `{"currentPassword":"wrong","newPassword":"new-password","confirmPassword":"new-password"}`
	for i := 0; i < 5; i++ {
		expectStatus(t, b.request("POST", "/api/auth/password", body, "http://admin.local", true), 422)
	}
	got := b.request("POST", "/api/auth/password", body, "http://admin.local", true)
	expectStatus(t, got, 429)
	if got.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry delay")
	}
	if r.user.PasswordHash != "test:admin" {
		t.Fatal("invalid request changed password")
	}
	// Own-password changes do not require settings-management privileges.
	r.attempts = 0
	expectStatus(t, b.request("POST", "/api/auth/password", `{"currentPassword":"admin","newPassword":"new-password","confirmPassword":"new-password"}`, "http://admin.local", true), 204)
}
