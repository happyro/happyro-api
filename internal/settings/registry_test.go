package settings

import "testing"

func TestSettingsValidationSeparatesGovernanceFromPlayerChanges(t *testing.T) {
	if err := Validate(map[string]int64{"base_exp_rate": 2147483647, "item_rate_card": 1000000}, true); err != nil {
		t.Fatal(err)
	}
	for _, changes := range []map[string]int64{nil, {"unknown": 1}, {"base_exp_rate": -1}, {"item_rate_card": 1000001}, {"game_tools_game_settings_policy": 2}} {
		if err := Validate(changes, true); err == nil {
			t.Fatalf("accepted %+v", changes)
		}
	}
	if err := Validate(map[string]int64{"game_tools_game_settings_policy": 1}, false); err != nil {
		t.Fatal(err)
	}
	if err := Validate(map[string]int64{"game_tools_game_settings_policy": 0}, false); err == nil {
		t.Fatal("invalid governance value")
	}
	for _, d := range Definitions(true) {
		if d.Source != "" {
			t.Fatal("source exposed")
		}
	}
}
