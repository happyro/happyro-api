package config

import "testing"

func TestDisplayTimeZone(t *testing.T) {
	for _, zone := range []string{"Asia/Shanghai", "UTC", "America/New_York"} {
		t.Setenv("HAPPYRO_TIMEZONE", zone)
		cfg, err := Load()
		if err != nil || cfg.TimeZone != zone {
			t.Fatalf("zone %s: %v", zone, err)
		}
	}
	for _, zone := range []string{"Local", "Invalid/Zone"} {
		t.Setenv("HAPPYRO_TIMEZONE", zone)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted %s", zone)
		}
	}
}
