package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoArgumentsOnlyPrintsHelpWithoutLoadingConfiguration(t *testing.T) {
	t.Setenv("SESSION_LIFETIME", "invalid")
	for _, args := range [][]string{nil, {"--no-color"}} {
		command := NewRoot()
		command.SetArgs(args)
		var output bytes.Buffer
		command.SetOut(&output)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		text := output.String()
		if !strings.HasPrefix(text, "\n") || !strings.HasSuffix(text, "\n\n") {
			t.Fatalf("help needs leading and trailing blank lines: %q", text)
		}
		if len(args) > 0 && strings.Contains(text, "\033[") {
			t.Fatal("--no-color printed ANSI")
		}
		if len(args) == 0 && !strings.Contains(text, "\033[1;36m") {
			t.Fatal("default help missing color")
		}
	}
}
