package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash format: %s", h)
	}
	if ok, err := VerifyPassword(h, "correct horse battery"); !ok || err != nil {
		t.Errorf("right password: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword(h, "correct horse batterY"); ok {
		t.Error("wrong password verified")
	}
	if h2, _ := HashPassword("correct horse battery"); h2 == h {
		t.Error("two hashes of the same password are equal; salt is not random")
	}
	if _, err := VerifyPassword("$2a$10$bcrypt", "x"); err == nil {
		t.Error("non-argon2 hash accepted")
	}
}

func TestToken(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 || !bytes.Equal(hash, HashToken(tok)) {
		t.Errorf("token %q (len %d) hash mismatch", tok, len(tok))
	}
	if tok2, _, _ := NewToken(); tok2 == tok {
		t.Error("tokens repeat")
	}
}
