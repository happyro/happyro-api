package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPreservesOperatorConfigurationAndMergesManagedValues(t *testing.T) {
	prefix := "// operator comment\nbase_exp_rate: 100\nimport: conf/custom.txt\n"
	first, err := Render(prefix, map[string]int64{"base_exp_rate": 200, "job_exp_rate": 300})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(first, map[string]int64{"base_exp_rate": 400})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(second, prefix) || !strings.Contains(second, "base_exp_rate: 400\njob_exp_rate: 300") || strings.Count(second, blockStart) != 1 {
		t.Fatal(second)
	}
	same, err := Render(second, map[string]int64{"base_exp_rate": 400})
	if err != nil || same != second {
		t.Fatal("non-deterministic render")
	}
}
func TestRenderRejectsAmbiguousOrInvalidManagedConfiguration(t *testing.T) {
	for _, content := range []string{blockStart, blockEnd, blockEnd + blockStart, blockStart + blockStart + blockEnd, blockStart + "\nunknown: 1\n" + blockEnd, blockStart + "\nbase_exp_rate: 1\nbase_exp_rate: 2\n" + blockEnd, blockStart + "\n" + blockEnd + "\nbase_exp_rate: 100"} {
		if _, err := Render(content, map[string]int64{"base_exp_rate": 200}); err == nil {
			t.Fatalf("accepted %q", content)
		}
	}
}
func TestFilePublishesCompleteConfigPreservingModeAndRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "battle_conf.txt")
	file := File{Path: path}
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := file.Publish("new\n"); err != nil {
		t.Fatal(err)
	}
	raw, err := file.Read()
	if err != nil || raw != "new\n" {
		t.Fatalf("read %q %v", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("permissions changed")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := (File{Path: link}).Publish("unsafe"); err == nil {
		t.Fatal("followed symlink")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("temporary file leaked")
	}
}
