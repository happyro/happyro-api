package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicConfigurationExposesOnlyDisplayTimeZone(t *testing.T) {
	b, _ := newBrowser(t, func(s *Server) { s.Config.TimeZone = "America/New_York" })
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public-config", nil))
	expectStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"timeZone":"America/New_York"}` {
		t.Fatal(w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("runtime config must not be cached")
	}
}
