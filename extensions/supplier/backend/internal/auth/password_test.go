package auth

import (
	"strings"
	"testing"
)

func TestLocalPasswordHashAndPolicy(t *testing.T) {
	for _, p := range []string{"short1", "abcdefghijk", "1234567890", strings.Repeat("a", 65) + "1"} {
		if ValidPassword(p) {
			t.Fatal("weak password accepted")
		}
	}
	password := "FixturePassword!2026"
	if !ValidPassword(password) {
		t.Fatal("valid rejected")
	}
	a, e := HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	b, e := HashPassword(password)
	if e != nil || a == b {
		t.Fatal("salt reused", e)
	}
	if strings.Contains(a, password) || !CheckPassword(password, a) || CheckPassword("wrong", a) || CheckPassword(password, "") {
		t.Fatal("hash check")
	}
}
