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
				values["HAPPYRO_ADMIN_CSRF_ORIGIN_CHECK_ENABLED"] = tc.value
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

func TestAdminPasswordChangeEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", true}, {"true", true}, {"false", false}} {
		values := map[string]string{}
		if tc.value != "" {
			values["HAPPYRO_ADMIN_PASSWORD_CHANGE_ENABLED"] = tc.value
		}
		var c Config
		if err := env.ParseWithOptions(&c, env.Options{Environment: values}); err != nil {
			t.Fatal(err)
		}
		if c.AdminPasswordChangeEnabled != tc.want {
			t.Fatalf("%q: %v", tc.value, c.AdminPasswordChangeEnabled)
		}
	}
}

func TestRegistrationSettingsEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", true}, {"false", false}, {"true", true}} {
		values := map[string]string{}
		if tc.value != "" {
			values["HAPPYRO_REGISTRATION_DEFAULT_GROUP_EDITABLE"] = tc.value
		}
		var c Config
		if err := env.ParseWithOptions(&c, env.Options{Environment: values}); err != nil {
			t.Fatal(err)
		}
		if c.RegistrationDefaultGroupEditable != tc.want {
			t.Fatalf("got %v", c.RegistrationDefaultGroupEditable)
		}
	}
}

func TestRegistrationDefaultGroupEnvironment(t *testing.T) {
	for _, value := range []string{"", "0", "99", "1", "-1", "abc"} {
		t.Run(value, func(t *testing.T) {
			var c Config
			err := env.ParseWithOptions(&c, env.Options{Environment: map[string]string{"HAPPYRO_REGISTRATION_DEFAULT_GROUP_ID": value}})
			if err == nil {
				err = c.Validate()
			}
			valid := value == "" || value == "0" || value == "99"
			if (err == nil) != valid {
				t.Fatalf("value %q: %v", value, err)
			}
			if value == "" && c.RegistrationDefaultGroupID != nil {
				t.Fatal("empty default must preserve database policy")
			}
			if value == "99" && (c.RegistrationDefaultGroupID == nil || *c.RegistrationDefaultGroupID != 99) {
				t.Fatal("GM default lost")
			}
		})
	}
}

func TestRuntimeBooleanEnvironmentRejectsAmbiguousValues(t *testing.T) {
	for _, name := range []string{"HAPPYRO_REGISTRATION_DEFAULT_GROUP_EDITABLE", "HAPPYRO_ADMIN_PASSWORD_CHANGE_ENABLED", "HAPPYRO_ADMIN_CSRF_ORIGIN_CHECK_ENABLED", "HAPPYRO_ADMIN_SESSION_SECURE_COOKIE_ENABLED"} {
		t.Run(name, func(t *testing.T) {
			for _, value := range []string{"1", "0", "TRUE", "yes", "false "} {
				t.Setenv(name, value)
				if _, err := Load(); err == nil {
					t.Fatalf("accepted %s=%s", name, value)
				}
			}
		})
	}
}
