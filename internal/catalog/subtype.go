package catalog

import "slices"

func slotType(slot string) string {
	switch slot {
	case "Left_Hand":
		return "Shield"
	case "Both_Accessory":
		return "Accessory"
	case "Right_Hand", "Both_Hand":
		return "Weapon"
	}
	return slot
}
func subtype(item Object) any {
	declared := nullableText(item["SubType"])
	if text(item["Type"]) != "Armor" && text(item["Type"]) != "Card" {
		return declared
	}
	slots := []string{}
	for name, value := range object(item["Locations"]) {
		if boolValue(value, false) {
			slots = append(slots, name)
		}
	}
	if len(slots) == 0 {
		return declared
	}
	slices.Sort(slots)
	if len(slots) == 1 {
		return slotType(slots[0])
	}
	for _, group := range []struct {
		slots []string
		name  string
	}{{[]string{"Head_Top", "Head_Mid", "Head_Low"}, "Head"}, {[]string{"Costume_Head_Top", "Costume_Head_Mid", "Costume_Head_Low"}, "Costume_Head"}} {
		all := true
		for _, slot := range slots {
			if !slices.Contains(group.slots, slot) {
				all = false
			}
		}
		if all {
			return group.name
		}
	}
	if len(slots) >= 6 {
		return "Any"
	}
	for _, slot := range []string{"Head_Top", "Head_Mid", "Head_Low", "Armor", "Garment", "Shoes", "Left_Hand", "Both_Accessory", "Right_Accessory", "Left_Accessory", "Costume_Head_Top", "Costume_Head_Mid", "Costume_Head_Low", "Costume_Garment", "Right_Hand", "Both_Hand"} {
		if slices.Contains(slots, slot) {
			return slotType(slot)
		}
	}
	return slots[0]
}
