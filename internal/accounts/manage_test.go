package accounts

import (
	"errors"
	"strings"
	"testing"
)

func TestGameCredentialsUseWireBytes(t *testing.T) {
	for _, tc := range []struct {
		value string
		max   int
		code  string
	}{
		{"short", 21, "account_too_short"}, {strings.Repeat("a", 22), 21, "account_too_long"}, {strings.Repeat("中", 8), 21, "account_too_long"}, {strings.Repeat("中", 8), 23, "account_too_long"}, {"abc\x00def", 21, "account_invalid"},
	} {
		err := credential(tc.value, "账号", "account", tc.max)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Code != tc.code {
			t.Fatalf("%q => %v", tc.value, err)
		}
	}
	for _, value := range []string{"abcdef", "中文", strings.Repeat("a", 21)} {
		if err := credential(value, "账号", "account", 21); err != nil {
			t.Fatal(err)
		}
	}
	if err := credential(strings.Repeat("a", 23), "密码", "password", 23); err != nil {
		t.Fatal(err)
	}
}
