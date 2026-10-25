// Chinese messages migrated from backend/lang/zh_CN/messages.php.
package game

var messages = map[string]string{
	"appearance_busy":                  "当前状态无法切换职业外观，请结束对话、交易或仓库等操作后重试",
	"appearance_job_required":          "当前职业、阶段或性别无法使用此职业外观",
	"character_progression_busy":       "请先结束 NPC 对话，再修改职业或等级",
	"riding_dismount_warg":             "请先从战狼上下骑",
	"riding_warg_required":             "请先召唤战狼",
	"riding_falcon_conflict":           "请先召回战狼",
	"riding_warg_conflict":             "请先解除猎鹰",
	"riding_skill_busy":                "请等待当前技能动作结束后重试",
	"riding_skill_failed":              "操作未完成，请检查 SP、所需道具及技能使用条件",
	"riding_busy":                      "当前状态无法操作，请结束对话、交易或仓库等操作后重试",
	"riding_job_required":              "当前职业无法使用此功能",
	"riding_skill_required":            "请先学习对应的职业技能",
	"riding_conflict":                  "请先解除当前坐骑",
	"riding_insufficient_zeny":         "Zeny 不足",
	"riding_state_changed":             "骑乘状态已变化，请重新选择",
	"riding_quote_changed":             "租用费用已变化，请重新确认",
	"player_database_unconfigured":     "玩家数据库尚未配置",
	"player_account_not_found":         "玩家账号不存在",
	"item_not_found":                   "物品不存在",
	"character_not_found":              "角色不存在",
	"account_deleting":                 "账号正在删除，无法执行角色操作。",
	"character_offline":                "角色不在线。",
	"inventory_full":                   "背包已满。",
	"inventory_overweight":             "背包超重。",
	"item_amount_exceeded":             "物品数量超出限制。",
	"zeny_amount_exceeded":             "Zeny 数量超出限制。",
	"command_not_replayable":           "该操作尚未完成，不能重复执行。",
	"no_spawn_cell":                    "当前位置无法召唤魔物。",
	"reset_failed":                     "角色重置失败。",
	"item_not_grantable":               "该物品不能发放。",
	"invalid_parameter":                "请求参数无效。",
	"authentication_failed":            "游戏服务认证失败。",
	"unsupported_command":              "游戏服务不支持该操作。",
	"request_failed":                   "游戏服务请求失败。",
	"request_rejected":                 "游戏服务拒绝了该请求。",
	"invalid_response":                 "游戏服务返回了无效响应。",
	"unavailable":                      "游戏服务暂时不可用。",
	"map_server_unavailable":           "地图服务暂时不可用。",
	"game_data_client_version_invalid": "客户端资源版本不存在",
	"game_data_server_version_invalid": "服务端资料版本不存在",
	"item_grant_idempotency_conflict":  "该请求标识已用于其他物品发放",
	"item_grant_not_replayable":        "该物品发放仍在处理中或已经失败",
	"game_server_unavailable":          "游戏服务暂时不可用",
	"game_server_setting_out_of_range": "游戏设置数值超出允许范围。",
	"navigation_disabled":              "该角色没有导航传送权限。",
	"navigation_cross_map_disabled":    "当前不允许跨地图传送。",
	"navigation_cooldown":              "传送正在冷却，请稍后重试。",
	"navigation_invalid_map":           "目标地图不存在或未加载。",
	"navigation_npc_unavailable":       "该 NPC 对目标角色不可见或尚未开放。",
	"navigation_map_forbidden":         "角色或地图规则禁止此次传送。",
	"navigation_no_cell":               "NPC 附近没有可用落点。",
	"navigation_invalid_coordinate":    "坐标超出地图范围。",
	"navigation_dialogue_active":       "请先结束当前 NPC 对话，再尝试传送。",
	"navigation_no_safe_cell":          "目标附近没有安全落点，请稍后重试。",
	"navigation_map_not_ready":         "地图尚未加载完成，请稍后重试。",
	"navigation_failed":                "角色传送失败。",
	"navigation_same_map_required":     "只能在目标角色当前地图内寻路。",
}

func Message(code, fallback string) string {
	if message, ok := messages[code]; ok {
		return message
	}
	if fallback != "" {
		return fallback
	}
	return "游戏服务暂时不可用。"
}
