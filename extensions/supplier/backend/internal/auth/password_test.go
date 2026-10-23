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

func TestPasswordSymbolsAndWhitespaceAreSignificant(t *testing.T) {
	for _, p := range []string{"Aa1!@#$%^&*()_+-=[]{};:',.<>/?`~\"\\|", " Aa1!Password ", "密码Aa1！＠＃测试"} {
		if !ValidPassword(p) {
			t.Fatal("valid symbolic password rejected")
		}
		h, e := HashPassword(p)
		if e != nil || !CheckPassword(p, h) || CheckPassword(p+"x", h) {
			t.Fatal("symbolic password hash mismatch", e)
		}
		if strings.TrimSpace(p) != p && CheckPassword(strings.TrimSpace(p), h) {
			t.Fatal("whitespace was stripped")
		}
	}
	for _, p := range []string{"!!!!!!!!!!", "123456789!", "abcdefghij!", "Aa1!", strings.Repeat("A", 64) + "1!"} {
		if ValidPassword(p) {
			t.Fatal("password strength requirement changed")
		}
	}
}
