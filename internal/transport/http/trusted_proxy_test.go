package httptransport

import (
	"context"
	"net/http"
	"testing"

	identityapi "github.com/happyro/happyro-api/internal/identity"
	sessionauth "github.com/happyro/happyro-api/internal/session"
)

type proxyIdentityRepository struct {
	identityRepository
	counts map[string]int
	ips    []string
}

func (r *proxyIdentityRepository) Attempt(_ context.Context, key string) (int, error) {
	r.counts[key]++
	if r.counts[key] > 5 {
		return 60, nil
	}
	return 0, nil
}

func (r *proxyIdentityRepository) Audit(_ context.Context, _ string, _ *identityapi.User, _ string, client identityapi.Client) error {
	r.ips = append(r.ips, client.IP)
	return nil
}

func TestAdminProxyPreservesClientRateLimitsAndRejectsUntrustedHeaders(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		name := "untrusted"
		if trusted {
			name = "trusted"
		}
		t.Run(name, func(t *testing.T) {
			repo := &proxyIdentityRepository{counts: map[string]int{}}
			b, _ := newBrowser(t, func(s *Server) {
				s.Config.TrustedProxies = []string{"172.30.81.2"}
				var err error
				s.Auth, err = sessionauth.NewAdminService(repo, testHasher{})
				if err != nil {
					t.Fatal(err)
				}
			})
			remote := "172.30.81.3:12345"
			if trusted {
				remote = "172.30.81.2:12345"
			}
			clientIP := "198.51.100.10"
			handler := b.handler
			b.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.RemoteAddr = remote
				r.Header.Set("X-Forwarded-For", clientIP)
				r.Header.Set("X-Real-IP", clientIP)
				handler.ServeHTTP(w, r)
			})
			expectStatus(t, b.request("GET", "/api/auth/csrf", "", "", false), 204)
			login := func() int {
				return b.request("POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, "http://admin.local", true).Code
			}
			for i := 0; i < 5; i++ {
				if code := login(); code != 422 {
					t.Fatalf("attempt %d: %d", i, code)
				}
			}
			if code := login(); code != 429 {
				t.Fatalf("limited client: %d", code)
			}
			clientIP = "198.51.100.11"
			wantCode, wantIP := 429, "172.30.81.3"
			if trusted {
				wantCode, wantIP = 422, clientIP
			}
			if code := login(); code != wantCode {
				t.Fatalf("second client: %d, want %d", code, wantCode)
			}
			if ip := repo.ips[len(repo.ips)-1]; ip != wantIP {
				t.Fatalf("audit IP = %s, want %s", ip, wantIP)
			}
		})
	}
}
