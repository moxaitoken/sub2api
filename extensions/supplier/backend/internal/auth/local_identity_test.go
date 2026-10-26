package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
)

type localIdentityStore struct {
	ports.IdentityStore
	rows   *testutil.Store
	record ports.SupplierRecord
}

func (f *localIdentityStore) LocalCredentials(context.Context, string) (ports.SupplierRecord, error) {
	return f.record, nil
}
func (f *localIdentityStore) LocalSupplier(context.Context, string) (ports.SupplierRecord, error) {
	return f.record, nil
}
func (f *localIdentityStore) CreateSession(ctx context.Context, v ports.Session) error {
	return f.rows.CreateSession(ctx, v)
}
func (f *localIdentityStore) WithSession(ctx context.Context, hash string, fn func(*ports.Session) error) error {
	return f.rows.WithSession(ctx, hash, fn)
}

func TestLocalSupplierFixedSevenDaySession(t *testing.T) {
	s, rows, _ := setup(t)
	hash, err := HashPassword("FixturePassword!2026")
	if err != nil {
		t.Fatal(err)
	}
	store := &localIdentityStore{rows: rows, record: ports.SupplierRecord{
		Supplier: ports.Supplier{ID: "10000000-0000-0000-0000-000000000001", PortalStatus: "active"},
		Login:    "fixture", PasswordHash: hash, CredentialVersion: 1, SessionEpoch: 1,
	}}
	s.Local = store
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	now := start
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	login, err := s.LocalLogin(ctx, "fixture", "FixturePassword!2026", info)
	if err != nil {
		t.Fatal(err)
	}
	expires := start.Add(7 * 24 * time.Hour)
	if !login.ExpiresAt.Equal(expires) || !rows.Sessions[Hash(login.Cookie)].ExpiresAt.Equal(expires) {
		t.Fatal("login and persisted expiry must be exactly seven days")
	}
	var csrf string
	read := func(client ports.ClientInfo) error {
		return s.LocalDo(ctx, login.Cookie, "", false, client, func(_ ports.SupplierRecord, token string) error { csrf = token; return nil })
	}
	for _, elapsed := range []time.Duration{31 * time.Minute, 9 * time.Hour, 6 * 24 * time.Hour, 7*24*time.Hour - time.Nanosecond} {
		now = start.Add(elapsed)
		if err := read(info); err != nil {
			t.Fatalf("session rejected before fixed expiry at %s: %v", elapsed, err)
		}
		if !rows.Sessions[Hash(login.Cookie)].ExpiresAt.Equal(expires) {
			t.Fatal("activity extended absolute expiry")
		}
	}
	// Existing security and revocation checks still apply inside seven days.
	for _, tc := range []struct {
		name    string
		change  func()
		restore func()
	}{
		{"disabled", func() { store.record.Supplier.PortalStatus = "disabled" }, func() { store.record.Supplier.PortalStatus = "active" }},
		{"password reset", func() { store.record.CredentialVersion++ }, func() { store.record.CredentialVersion-- }},
		{"session epoch", func() { store.record.SessionEpoch++ }, func() { store.record.SessionEpoch-- }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.change()
			defer tc.restore()
			if err := read(info); !errors.Is(err, ports.ErrUnauthenticated) {
				t.Fatal("revoked identity accepted", err)
			}
		})
	}
	if err := read(ports.ClientInfo{IP: "192.0.2.2", UserAgent: info.UserAgent}); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatal("client binding bypassed", err)
	}
	if err := s.LocalLogout(ctx, login.Cookie, "wrong-csrf", info); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("CSRF bypassed", err)
	}
	now = expires
	if err := read(info); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatal("exact expiry accepted", err)
	}
	now = expires.Add(time.Second)
	if err := read(info); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatal("past expiry accepted", err)
	}
	// The clock moves back only to exercise logout before the fixed boundary.
	now = expires.Add(-time.Second)
	if err := s.LocalLogout(ctx, login.Cookie, csrf, info); err != nil {
		t.Fatal(err)
	}
	if err := read(info); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatal("logout did not revoke", err)
	}
}

func TestAdministratorSessionLifetimeUnchanged(t *testing.T) {
	for _, elapsed := range []time.Duration{30 * time.Minute, 8 * time.Hour} {
		t.Run(elapsed.String(), func(t *testing.T) {
			s, rows, up := setup(t)
			s.Administrator = true
			up.ProfileValue.Role = "admin"
			start := time.Now().UTC()
			now := start
			s.Now = func() time.Time { return now }
			l := login(t, s)
			if !l.ExpiresAt.Equal(start.Add(8 * time.Hour)) {
				t.Fatal("admin absolute lifetime changed")
			}
			if elapsed == 8*time.Hour {
				row := rows.Sessions[Hash(l.Cookie)]
				row.LastSeenAt = start.Add(elapsed - time.Minute)
				rows.Sessions[row.Hash] = row
			}
			now = start.Add(elapsed)
			if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); !errors.Is(err, ports.ErrForbidden) {
				t.Fatal("admin idle/absolute expiration changed", err)
			}
		})
	}
}
