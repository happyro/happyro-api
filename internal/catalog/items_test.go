package catalog

import (
	"strings"
	"testing"
)

func TestItemViewCanonicalTypes(t *testing.T) {
	for _, kind := range []string{"PetEgg", "PetArmor", "ShadowGear", "DelayConsume", "Weapon", "Armor", "Card", "Healing", "Usable", "Etc", "Cash", "Ammo", "None"} {
		for _, input := range []string{kind, strings.ToLower(kind)} {
			row := itemView(9001, nil, Object{"Type": input}, "", "server")
			if row["Type"] != kind {
				t.Errorf("type %q: got %v, want %s", input, row["Type"], kind)
			}
		}
	}
}
