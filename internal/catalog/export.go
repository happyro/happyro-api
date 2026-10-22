package catalog

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
)

//go:embed subtype.go
var subtypeRules []byte

// ExportSubtypes uses exactly the same classification as the imported API view.
// It does not require configuration or a database connection.
func ExportSubtypes(path string) ([]byte, error) {
	snapshot, err := readSnapshot(path, "server")
	if err != nil {
		return nil, err
	}
	items := map[int64]any{}
	equipment := map[int64]any{}
	for id, item := range snapshot.Entries {
		kind := text(item["Type"])
		if kind != "Weapon" && kind != "Armor" && kind != "Card" {
			continue
		}
		if kind == "Weapon" || kind == "Armor" {
			jobs := object(item["Jobs"])
			if jobs == nil {
				jobs = Object{"All": true}
			}
			equipment[id] = Object{"jobs": jobs}
		}
		if value := subtype(item); value != nil {
			items[id] = value
		}
	}
	digest := sha256.Sum256(subtypeRules)
	raw, err := json.Marshal(struct {
		Sources   map[string]string `json:"sources"`
		Items     map[int64]any     `json:"items"`
		Equipment map[int64]any     `json:"equipment"`
	}{map[string]string{"renewal_snapshot": snapshot.Hash, "subtype_rules": hex.EncodeToString(digest[:])}, items, equipment})
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
