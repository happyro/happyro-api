package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRequiresExplicitAbsoluteBattleConfigurationPath(t *testing.T) {
	for _, path := range []string{"", "  ", "battle_conf.txt", "../server/conf/import/battle_conf.txt"} {
		cfg := Config{BattleConfigPath: path}
		if err := cfg.ValidateServer(); err == nil || !strings.Contains(err.Error(), "GAME_SERVER_BATTLE_CONFIG_PATH") {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	cfg := Config{BattleConfigPath: filepath.Join(t.TempDir(), "battle_conf.txt")}
	if err := cfg.ValidateServer(); err != nil {
		t.Fatal(err)
	}
}
