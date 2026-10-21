package settings

import gameapi "github.com/happyro/happyro-api/internal/game"

// Definitions follow HappyRO server battle settings and the existing management contract.
type Definition struct {
	Key     string `json:"key"`
	Minimum int64  `json:"minimum"`
	Maximum int64  `json:"maximum"`
	Source  string `json:"source,omitempty"`
	Unit    string `json:"unit"`
}

func Definitions(adventure bool) map[string]Definition {
	result := map[string]Definition{}
	for _, d := range []Definition{
		{"base_exp_rate", 0, 2147483647, "conf/battle/exp.conf", "percent"},
		{"job_exp_rate", 0, 2147483647, "conf/battle/exp.conf", "percent"},
		{"item_rate_common", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_common_boss", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_common_mvp", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_heal", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_heal_boss", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_heal_mvp", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_use", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_use_boss", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_use_mvp", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_equip", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_equip_boss", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_equip_mvp", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_card", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_card_boss", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"item_rate_card_mvp", 0, 1000000, "conf/battle/drops.conf", "percent"},
		{"refine_chance_rate", 0, 1000000, "conf/battle/items.conf", "percent"},
		{"refine_always_success", 0, 1, "conf/battle/items.conf", "boolean"},
		{"navigation_teleport_policy", 0, 2, "conf/import/battle_conf.txt", "policy"},
		{"navigation_teleport_cross_map", 0, 1, "conf/import/battle_conf.txt", "boolean"},
		{"navigation_teleport_cooldown", 0, 3600, "conf/import/battle_conf.txt", "seconds"},
		{"navigation_map_channels_enabled", 0, 1, "conf/import/battle_conf.txt", "boolean"},
		{"game_tools_monster_spawn_policy", 0, 2, "conf/import/battle_conf.txt", "policy"},
		{"game_tools_monster_spawn_cooldown", 0, 3600, "conf/import/battle_conf.txt", "seconds"},
		{"game_tools_monster_spawn_duration", 1, 3600, "conf/import/battle_conf.txt", "seconds"},
		{"game_tools_monster_spawn_allow_boss", 0, 1, "conf/import/battle_conf.txt", "boolean"},
		{"game_tools_character_maintenance_policy", 1, 2, "conf/import/battle_conf.txt", "policy"},
		{"game_tools_game_settings_policy", 1, 2, "conf/import/battle_conf.txt", "policy"},
		{"game_tools_item_grant_policy", 1, 2, "conf/import/battle_conf.txt", "policy"},
	} {
		if adventure && (d.Key == "game_tools_character_maintenance_policy" || d.Key == "game_tools_game_settings_policy" || d.Key == "game_tools_item_grant_policy") {
			continue
		}
		if adventure {
			d.Source = ""
		}
		result[d.Key] = d
	}
	return result
}
func Validate(changes map[string]int64, adventure bool) error {
	if len(changes) == 0 {
		return &gameapi.ValidationError{Field: "changes", Message: "至少需要修改一项游戏设置。"}
	}
	definitions := Definitions(adventure)
	for key, value := range changes {
		d, ok := definitions[key]
		if !ok || value < d.Minimum || value > d.Maximum {
			return &gameapi.ValidationError{Field: key, Message: "游戏设置无效或超出范围：" + key + "。"}
		}
	}
	return nil
}
