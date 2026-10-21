package catalog

import (
	"regexp"
	"strings"
)

var colorCode = regexp.MustCompile(`\^[0-9a-fA-F]{6}`)

func description(value any) any {
	lines, ok := value.([]any)
	if !ok {
		return nil
	}
	clean := []string{}
	for _, line := range lines {
		plain := strings.TrimSpace(colorCode.ReplaceAllString(text(line), ""))
		if plain != "" && plain != "_" {
			clean = append(clean, plain)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}
func nullableText(value any) any {
	if value, ok := value.(string); ok {
		return value
	}
	return nil
}

func itemView(id int64, client, server Object, clientVersion, serverVersion string) Object {
	payload := Object{}
	for key, value := range server {
		payload[key] = value
	}
	for key, value := range client {
		payload[key] = value
	}
	delete(payload, "identifiedDescriptionName")
	delete(payload, "identifiedResourceName")
	payload["Id"] = id
	primary := client
	if primary == nil {
		primary = server
	}
	payload["names"] = primary["names"]
	payload["AegisName"], payload["Type"], payload["SubType"] = nullableText(server["AegisName"]), canonicalItemType(server["Type"]), subtype(server)
	payload["resourceName"] = nullableText(client["identifiedResourceName"])
	payload["description"] = description(client["identifiedDescriptionName"])
	sources := Object{"names": "server"}
	if client != nil {
		sources["names"] = "client"
	}
	for _, field := range []string{"Buy", "Sell", "Weight", "Attack", "Defense", "Slots"} {
		payload[field] = nil
		if n, ok := integer(server[field]); ok && n >= 0 {
			payload[field] = n
		}
		if _, exists := server[field]; exists {
			sources[field] = "server"
		}
	}
	payload["Script"] = nullableText(server["Script"])
	if _, exists := server["Script"]; exists {
		sources["Script"] = "server"
	}
	for _, field := range []string{"AegisName", "Type", "SubType"} {
		if text(payload[field]) != "" {
			sources[field] = "server"
		}
	}
	if payload["description"] != nil {
		sources["description"] = "client"
	}
	if text(payload["resourceName"]) != "" {
		sources["resourceName"] = "client"
	}
	payload["fieldSources"] = sources
	payload["clientResourceVersion"], payload["serverVersion"] = nil, nil
	if client != nil {
		payload["clientResourceVersion"] = clientVersion
	}
	if server != nil {
		payload["serverVersion"] = serverVersion
	}
	switch {
	case client != nil && server != nil:
		payload["source"] = "both"
	case client != nil:
		payload["source"] = "client"
	default:
		payload["source"] = "server"
	}
	return payload
}
func monsterView(id int64, source Object, version string) Object {
	payload := Object{}
	for key, value := range source {
		payload[key] = value
	}
	payload["Id"], payload["serverVersion"] = id, version
	for field, value := range map[string]string{"Size": "Small", "Race": "Formless", "Element": "Neutral", "AegisName": ""} {
		if _, present := payload[field]; !present {
			payload[field] = value
		}
	}
	for _, field := range []string{"Level", "Hp", "ElementLevel"} {
		payload[field] = number(source[field], 1)
	}
	kind := "normal"
	if drops, ok := source["MvpDrops"].([]any); ok && len(drops) > 0 {
		kind = "mvp"
	} else if text(source["Class"]) == "Boss" {
		kind = "mini"
	}
	payload["kind"] = kind
	return payload
}
func npcView(source Object) Object {
	row := Object{}
	for key, value := range source {
		row[key] = value
	}
	for _, key := range []string{"name", "source_name", "display_name"} {
		row[key] = text(source[key])
	}
	if _, ok := row["type"]; !ok {
		row["type"] = "script"
	}
	for _, key := range []string{"x", "y", "catalog_order"} {
		row[key] = number(source[key], 0)
	}
	for _, key := range []string{"sprite_id", "display_sprite_id"} {
		row[key] = nil
		if n, ok := integer(source[key]); ok {
			row[key] = n
		}
	}
	for _, key := range []string{"image_available", "game_visible"} {
		row[key] = boolValue(source[key], false)
	}
	row["enabled"] = boolValue(source["enabled"], true)
	row["map_name_zh_cn"] = nullableText(source["map_name_zh_cn"])
	row["name_zh_cn"] = nil
	if row["display_name"] != row["source_name"] {
		row["name_zh_cn"] = row["display_name"]
	}
	return row
}

// rAthena accepts item type names without regard to case. Publish one spelling
// so filtering, presentation and reward restrictions share the same enum.
func canonicalItemType(value any) any {
	for _, kind := range []string{"Healing", "Usable", "Etc", "Armor", "Weapon", "Card", "PetEgg", "PetArmor", "Ammo", "DelayConsume", "ShadowGear", "Cash", "None"} {
		if strings.EqualFold(text(value), kind) {
			return kind
		}
	}
	return nullableText(value)
}
