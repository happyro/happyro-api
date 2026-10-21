package identity

import "testing"

func TestArgon2PasswordVerification(t *testing.T) {
	hasher := Argon2id{}
	hash, err := hasher.Hash("安全密码123")
	if err != nil {
		t.Fatal(err)
	}
	if !hasher.Verify("安全密码123", hash) {
		t.Fatal("correct password rejected")
	}
	if hasher.Verify("wrong", hash) {
		t.Fatal("wrong password accepted")
	}
	for _, corrupt := range []string{"", "$argon2id$v=19$m=4294967295,t=3,p=1$AAAA$AAAA", "$argon2id$v=19$m=65536,t=3,p=1$AAAA$AAAA"} {
		if hasher.Verify("password", corrupt) {
			t.Fatal("malformed hash accepted")
		}
	}
}
