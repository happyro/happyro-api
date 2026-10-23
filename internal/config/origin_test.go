package config

import (
	"github.com/caarlos0/env/v11"
	"testing"
)

func TestAdminOriginCheckEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", false}, {"false", false}, {"true", true}} {
		t.Run(tc.value, func(t *testing.T) {
			values := map[string]string{}
			if tc.value != "" {
				values["ADMIN_CSRF_ORIGIN_CHECK_ENABLED"] = tc.value
			}
			var c Config
			if err := env.ParseWithOptions(&c, env.Options{Environment: values}); err != nil {
				t.Fatal(err)
			}
			if c.AdminCSRFOriginCheckEnabled != tc.want {
				t.Fatalf("got %v", c.AdminCSRFOriginCheckEnabled)
			}
		})
	}
}
