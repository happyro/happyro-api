package httptransport

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func concreteRoute(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

// Enumerate the actual registered routes so newly added endpoints must obey the
// same authentication, permission and CSRF boundaries without updating a list.
func TestEveryBusinessRouteEnforcesItsIdentityBoundary(t *testing.T) {
	anonymous, _ := newBrowser(t)
	expectStatus(t, anonymous.request("GET", "/api/auth/csrf", "", "", false), 204)
	restricted, repo := newBrowser(t)
	restricted.login(t)
	repo.user.Permissions = nil
	privileged, _ := newBrowser(t)
	privileged.login(t)
	count := 0
	for _, route := range anonymous.handler.(*gin.Engine).Routes() {
		if !strings.HasPrefix(route.Path, "/api/") || route.Path == "/api/health" || strings.HasPrefix(route.Path, "/api/auth/") {
			continue
		}
		count++
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			path := concreteRoute(route.Path)
			expectStatus(t, anonymous.request(route.Method, path, "{}", "http://admin.local", true), 401)
			if strings.HasPrefix(route.Path, "/api/adventure-tools/") {
				// An authenticated administrator must not substitute for a game identity.
				expectStatus(t, privileged.request(route.Method, path, "{}", "http://admin.local", true), 401)
				return
			}
			expectStatus(t, restricted.request(route.Method, path, "{}", "http://admin.local", true), 403)
			if route.Method != "GET" && route.Method != "HEAD" {
				expectStatus(t, privileged.request(route.Method, path, "{}", "http://admin.local", false), 419)
				expectStatus(t, privileged.request(route.Method, path, "{}", "https://untrusted.invalid", true), 403)
			}
		})
	}
	if count == 0 {
		t.Fatal("no business routes registered")
	}
}
