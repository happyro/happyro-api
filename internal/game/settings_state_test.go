package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSettingsStateRequiresCompleteExecutionEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"success", func(map[string]any) {}, true},
		{"idle", func(r map[string]any) {
			r["last_status"] = "idle"
			r["revision"] = uint64(0)
			r["last_command_id"] = ""
		}, true},
		{"barrier", func(r map[string]any) { r["last_status"] = "fenced"; r["last_command_id"] = "" }, true},
		{"failed", func(r map[string]any) { r["last_status"] = "failed" }, true},
		{"running", func(r map[string]any) { r["last_status"] = "running" }, true},
		{"missing revision", func(r map[string]any) { delete(r, "revision") }, false},
		{"null revision", func(r map[string]any) { r["revision"] = nil }, false},
		{"negative revision", func(r map[string]any) { r["revision"] = -1 }, false},
		{"fractional revision", func(r map[string]any) { r["revision"] = 1.5 }, false},
		{"invalid instance", func(r map[string]any) { r["instance_id"] = "not-an-instance" }, false},
		{"short instance", func(r map[string]any) { r["instance_id"] = "abcd" }, false},
		{"missing values", func(r map[string]any) { delete(r, "values") }, false},
		{"missing receipt", func(r map[string]any) { delete(r, "last_command_id") }, false},
		{"empty receipt", func(r map[string]any) { r["last_command_id"] = "" }, false},
		{"invalid receipt", func(r map[string]any) { r["last_command_id"] = "a\x00b" }, false},
		{"oversized receipt", func(r map[string]any) { r["last_command_id"] = strings.Repeat("a", 65) }, false},
		{"unknown status", func(r map[string]any) { r["last_status"] = "unknown" }, false},
		{"idle with receipt", func(r map[string]any) { r["last_status"] = "idle"; r["revision"] = 0 }, false},
		{"idle with revision", func(r map[string]any) { r["last_status"] = "idle"; r["last_command_id"] = "" }, false},
		{"fence with receipt", func(r map[string]any) { r["last_status"] = "fenced" }, false},
		{"fence without revision", func(r map[string]any) { r["last_status"] = "fenced"; r["last_command_id"] = ""; r["revision"] = 0 }, false},
		{"success without revision", func(r map[string]any) { r["revision"] = 0 }, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			result := map[string]any{"environment_overrides": map[string]string{"navigation_teleport_policy": "HAPPYRO_NAVIGATION_TELEPORT_POLICY"}, "values": map[string]int64{"base_exp_rate": 325}, "instance_id": "0123456789abcdef0123456789abcdef", "revision": uint64(9007199254740993), "last_command_id": "original-command", "last_status": "succeeded"}
			scenario.change(result)
			raw, err := json.Marshal(map[string]any{"data": map[string]any{"result": result}})
			if err != nil {
				t.Fatal(err)
			}
			gateway := &HTTPGateway{BaseURL: "http://game.local", Token: "secret", Client: doerFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != "GET" || request.URL.Path != "/game-control/v1/battle-config" || request.Header.Get("Authorization") != "Bearer secret" {
					t.Fatal("incorrect state request")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}
			state, err := gateway.SettingsState(context.Background())
			if !scenario.valid {
				var failure *GatewayError
				if !errors.As(err, &failure) || failure.Code != "invalid_response" || failure.OutcomeUnknown {
					t.Fatalf("invalid evidence accepted: %#v, %v", state, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if state.Values["base_exp_rate"] != 325 || state.LastStatus != result["last_status"] {
				t.Fatalf("wrong state: %#v", state)
			}
			if state.EnvironmentOverrides["navigation_teleport_policy"] != "HAPPYRO_NAVIGATION_TELEPORT_POLICY" {
				t.Fatal("environment override metadata lost")
			}
			if scenario.name == "success" && state.Revision != 9007199254740993 {
				t.Fatal("revision precision lost")
			}
		})
	}
}
