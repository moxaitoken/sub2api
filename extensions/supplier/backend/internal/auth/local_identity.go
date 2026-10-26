package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"regexp"
	"strings"
	"time"
)

var loginName = regexp.MustCompile(`^[A-Za-z0-9_.@-]{4,64}$`)

// Supplier portal sessions have a fixed lifetime; activity never renews it.
// Administrator and upstream OAuth token lifetimes are independent.
const localSupplierSessionLifetime = 7 * 24 * time.Hour

func NormalizeLogin(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return strings.ToLower(value), loginName.MatchString(value)
}
func (s *Service) LocalLogin(ctx context.Context, login, password string, info ports.ClientInfo) (Login, error) {
	normalized, valid := NormalizeLogin(login)
	if len(password) > 256 {
		return Login{}, ports.ErrUnauthenticated
	}
	record, err := s.Local.LocalCredentials(ctx, normalized)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return Login{}, err
	}
	match := CheckPassword(password, record.PasswordHash)
	if !valid || err != nil || !match || record.Supplier.PortalStatus != "active" {
		return Login{}, ports.ErrUnauthenticated
	}
	id, err := NewID()
	if err != nil {
		return Login{}, err
	}
	cookie, err := RandomToken()
	if err != nil {
		return Login{}, err
	}
	csrf, err := RandomToken()
	if err != nil {
		return Login{}, err
	}
	row := ports.Session{ID: id, Hash: Hash(cookie), InstanceID: s.InstanceID, Kind: "local_supplier", SupplierID: record.Supplier.ID,
		KeyID: s.KeyID, CSRFHash: Hash(csrf), ClientHash: binding(info), ExpiresAt: s.now().Add(localSupplierSessionLifetime), LastSeenAt: s.now(), Version: 1,
		CredentialVersion: record.CredentialVersion, SessionEpoch: record.SessionEpoch}
	row.Cipher, err = s.Vault.seal(row, secrets{CSRF: csrf})
	if err != nil {
		return Login{}, err
	}
	if err = s.Local.CreateSession(ctx, row); err != nil {
		return Login{}, err
	}
	return Login{Cookie: cookie, ExpiresAt: row.ExpiresAt}, nil
}
func (s *Service) LocalDo(ctx context.Context, cookie, csrf string, write bool, info ports.ClientInfo, fn func(ports.SupplierRecord, string) error) error {
	if cookie == "" {
		return ports.ErrUnauthenticated
	}
	var row ports.Session
	var value secrets
	var outcome error
	err := s.Local.WithSession(ctx, Hash(cookie), func(x *ports.Session) error {
		if x.Kind != "local_supplier" || x.InstanceID != s.InstanceID || x.Revoked || x.KeyID != s.KeyID || x.ClientHash != binding(info) || !s.now().Before(x.ExpiresAt) {
			return ports.ErrUnauthenticated
		}
		var err error
		value, err = s.Vault.open(*x)
		if err != nil {
			return ports.ErrUnauthenticated
		}
		if write && subtle.ConstantTimeCompare([]byte(x.CSRFHash), []byte(Hash(csrf))) != 1 {
			return ports.ErrForbidden
		}
		x.LastSeenAt = s.now()
		row = *x
		return nil
	})
	if errors.Is(err, ports.ErrNotFound) {
		return ports.ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	record, err := s.Local.LocalSupplier(ctx, row.SupplierID)
	if err == nil && (record.Supplier.PortalStatus != "active" || record.CredentialVersion != row.CredentialVersion || record.SessionEpoch != row.SessionEpoch) {
		outcome = ports.ErrUnauthenticated
	}
	if errors.Is(err, ports.ErrNotFound) {
		outcome = ports.ErrUnauthenticated
	} else if err != nil {
		return err
	}
	if outcome != nil {
		return outcome
	}
	return fn(record, value.CSRF)
}
func (s *Service) LocalLogout(ctx context.Context, cookie, csrf string, info ports.ClientInfo) error {
	return s.LocalDo(ctx, cookie, csrf, true, info, func(_ ports.SupplierRecord, _ string) error {
		return s.Local.WithSession(ctx, Hash(cookie), func(x *ports.Session) error { x.Revoked = true; return nil })
	})
}
