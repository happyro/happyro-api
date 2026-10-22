package game

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperatorCommandValidation(t *testing.T) {
	cases := []struct {
		kind, payload string
		valid         bool
	}{
		{"character.progression.update", `{"base_level":99,"job_id":1}`, true},
		{"character.progression.update", `{"base_level":0}`, false},
		{"character.points.update", `{"skill_points":32767}`, true},
		{"character.points.update", `{"skill_points":32768}`, false},
		{"character.stats.update", `{}`, false},
		{"character.stats.update", `{"str":1.5}`, false},
		{"character.stats.update", `{"str":"10"}`, false},
		{"character.stats.update", `{"str":10}`, true},
		{"character.traits.update", `{"pow":0}`, true},
		{"character.traits.update", `{"pow":32768}`, false},
		{"character.stats.reset", `{}`, true},
		{"character.skills.reset", `{"amount":1}`, false},
		{"character.appearance.update", `{"appearance":39}`, true},
		{"character.appearance.update", `{"appearance":40}`, false},
		{"character.riding.update", `{"kind":"peco","enabled":true,"fee":3500,"ticket":false}`, true},
		{"character.riding.update", `{"kind":"peco","enabled":1,"fee":3500,"ticket":false}`, false},
		{"character.vitals.restore", `{"vitals":["hp","sp"]}`, true},
		{"character.vitals.restore", `{"vitals":["hp","hp"]}`, false},
		{"character.navigation.route", `{"action":"stop"}`, true},
		{"character.navigation.route", `{"action":"stop","map":"prontera"}`, false},
		{"character.navigation.route", `{"action":"walk","map":"prontera","x":1,"y":2}`, true},
		{"character.navigation.teleport", `{"map":"../escape","x":1,"y":2}`, false},
		{"monster.spawn", `{"monster_id":1002,"count":10}`, true},
		{"monster.spawn", `{"monster_id":1002,"count":11}`, false},
		{"monster.spawn", `{"monster_id":1002,"script":"unexpected"}`, false},
		{"character.inventory.item_grant", `{"item_id":501,"amount":1}`, false},
		{"battle_config.apply", `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.kind+tc.payload, func(t *testing.T) {
			var request Request
			raw := `{"idempotency_key":"key","type":"` + tc.kind + `","target":{"type":"character","id":"150000"},"payload":` + tc.payload + `}`
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&request); err != nil {
				t.Fatal(err)
			}
			err := ValidateOperator(request)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
