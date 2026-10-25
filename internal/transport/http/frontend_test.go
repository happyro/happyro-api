package httptransport

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIOnlyRejectsFrontendAndUnknownRoutes(t *testing.T) {
	b, _ := newBrowser(t)
	for _, path := range []string{"/", "/index.html", "/user/login", "/players/accounts", "/app.js", "/api", "/api/unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := b.request(method, path, "", "", false)
			expectStatus(t, response, http.StatusNotFound)
			if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("%s %s returned non-JSON response", method, path)
			}
		}
	}
}
