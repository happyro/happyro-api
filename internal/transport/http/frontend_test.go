package httptransport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendServesExportedDirectoryRoutesAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "user/login"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.html": "<html>SPA root</html>", "user/login/index.html": "<html>Exported login</html>", "app.js": "console.log('app')"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := newBrowser(t, func(s *Server) { s.Config.FrontendDir = root })
	for _, tc := range []struct{ path, body string }{{"/", "SPA root"}, {"/user/login", "Exported login"}, {"/user/login/", "Exported login"}, {"/players/new-route", "SPA root"}, {"/app.js", "console.log"}} {
		response := b.request("GET", tc.path, "", "", false)
		expectStatus(t, response, 200)
		if !strings.Contains(response.Body.String(), tc.body) {
			t.Fatalf("%s: %s", tc.path, response.Body.String())
		}
	}
	for _, path := range []string{"/missing.js", "/api/unknown"} {
		response := b.request("GET", path, "", "", false)
		expectStatus(t, response, 404)
		if strings.Contains(response.Body.String(), "SPA root") {
			t.Fatal("missing asset/API returned HTML")
		}
	}
}
