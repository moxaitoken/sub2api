package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type secrets struct {
	Tokens    ports.Tokens `json:"tokens"`
	CSRF      string       `json:"csrf"`
	Challenge string       `json:"challenge,omitempty"`
}

type Vault struct {
	aead           cipher.AEAD
	fingerprintKey []byte
}

// SealBlob binds a business secret to an explicit domain and resource identity.
// OAuth data must not be interchangeable with authentication session ciphertext.
func (v *Vault) SealBlob(domain string, value []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, value, []byte("supplier-business:"+domain)), nil
}

func (v *Vault) OpenBlob(domain string, value []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(value) < n {
		return nil, ports.ErrForbidden
	}
	plain, err := v.aead.Open(nil, value[:n], value[n:], []byte("supplier-business:"+domain))
	if err != nil {
		return nil, ports.ErrForbidden
	}
	return plain, nil
}

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("session key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("supplier-private-request-fingerprint-v1"))
	return &Vault{aead: aead, fingerprintKey: h.Sum(nil)}, nil
}

func (v *Vault) seal(s ports.Session, value secrets) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, b, []byte(s.ID+":"+s.Kind)), nil
}

func (v *Vault) open(s ports.Session) (secrets, error) {
	var out secrets
	n := v.aead.NonceSize()
	if len(s.Cipher) < n {
		return out, ports.ErrForbidden
	}
	b, err := v.aead.Open(nil, s.Cipher[:n], s.Cipher[n:], []byte(s.ID+":"+s.Kind))
	if err != nil {
		return out, ports.ErrForbidden
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func RandomToken() (string, error) {
	b := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// Fingerprint protects low-entropy password-bearing idempotency payloads against offline guessing.
func (v *Vault) Fingerprint(domain string, value []byte) string {
	h := hmac.New(sha256.New, v.fingerprintKey)
	h.Write([]byte(domain + "\x00"))
	h.Write(value)
	return hex.EncodeToString(h.Sum(nil))
}
