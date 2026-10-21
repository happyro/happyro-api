package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogExportUsesSharedRulesWithoutDatabaseConfiguration(t *testing.T) {
	t.Setenv("SESSION_LIFETIME", "invalid")
	dir := t.TempDir()
	source := filepath.Join(dir, "source.json")
	output := filepath.Join(dir, "output.json")
	raw := `{"schema":"item-catalog/v2","mode":"renewal","englishSource":{"revision":"test"},"items":{"1":{"names":{"zh-CN":"盾","en-US":"Shield"},"Type":"Armor","Locations":{"Left_Hand":true}},"2":{"names":{"zh-CN":"头饰","en-US":"Head"},"Type":"Card","Locations":{"Head_Top":true,"Head_Mid":true}},"3":{"names":{"zh-CN":"剑","en-US":"Sword"},"Type":"Weapon","SubType":"Sword","Jobs":{"Swordman":true}},"4":{"names":{"zh-CN":"药水","en-US":"Potion"},"Type":"Healing"}}}`
	if err := os.WriteFile(source, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(check bool) error {
		cmd := NewRoot()
		args := []string{"catalog", "export-subtypes", "--server-items", source, "--output", output, "--no-color"}
		if check {
			args = append(args, "--check")
		}
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	if err := run(false); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Sources   map[string]string
		Items     map[string]string
		Equipment map[string]struct{ Jobs map[string]bool }
	}
	if err = json.Unmarshal(generated, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 || result.Items["1"] != "Shield" || result.Items["2"] != "Head" || result.Items["3"] != "Sword" || len(result.Equipment) != 2 || !result.Equipment["1"].Jobs["All"] || !result.Equipment["3"].Jobs["Swordman"] {
		t.Fatalf("wrong classification: %+v", result)
	}
	if len(result.Sources) != 2 || len(result.Sources["subtype_rules"]) != 64 || len(result.Sources["renewal_snapshot"]) != 64 {
		t.Fatal("missing provenance")
	}
	if err = run(true); err != nil {
		t.Fatal(err)
	}
	if err = run(false); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(output)
	if !bytes.Equal(generated, second) {
		t.Fatal("nondeterministic export")
	}
	if err = os.WriteFile(output, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run(true); err == nil {
		t.Fatal("accepted stale artifact")
	}
	after, _ := os.ReadFile(output)
	if string(after) != "stale" {
		t.Fatal("check mutated artifact")
	}
	if err = os.WriteFile(source, []byte(strings.Replace(raw, "renewal", "client-only", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run(false); err == nil {
		t.Fatal("accepted wrong snapshot")
	}
	after, _ = os.ReadFile(output)
	if string(after) != "stale" {
		t.Fatal("failed generation destroyed artifact")
	}
}
