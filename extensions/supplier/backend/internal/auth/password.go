package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const passwordRounds = 600000

func ValidPassword(password string) bool {
	if !utf8.ValidString(password) {
		return false
	}
	n := utf8.RuneCountInString(password)
	if n < 10 || n > 64 {
		return false
	}
	letter, number := false, false
	for _, r := range password {
		letter = letter || unicode.IsLetter(r)
		number = number || unicode.IsDigit(r)
	}
	return letter && number
}
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$600000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func CheckPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	salt := make([]byte, 16)
	expected := make([]byte, 32)
	valid := false
	if len(parts) == 4 && parts[0] == "pbkdf2-sha256" && parts[1] == "600000" {
		s, e1 := base64.RawStdEncoding.DecodeString(parts[2])
		k, e2 := base64.RawStdEncoding.DecodeString(parts[3])
		if e1 == nil && e2 == nil && len(s) == 16 && len(k) == 32 {
			salt = s
			expected = k
			valid = true
		}
	}
	// Unknown/disabled/malformed credentials still perform the same KDF work.
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1 && valid
}
