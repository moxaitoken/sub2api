// Package testutil contains synthetic, in-memory test doubles only.
package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type Store struct {
	Mu              sync.Mutex
	Sessions        map[string]ports.Session
	MembershipValue ports.Membership
	MissingMember   bool
}

func NewStore() *Store {
	return &Store{Sessions: make(map[string]ports.Session), MembershipValue: ports.Membership{Member: ports.Member{ID: "20000000-0000-0000-0000-000000000001", SupplierID: "10000000-0000-0000-0000-000000000001", UpstreamUserID: 42, Role: "owner", Status: "active", Version: 1}, Supplier: ports.Supplier{ID: "10000000-0000-0000-0000-000000000001", Name: "Fixture A", PortalStatus: "active", Version: 1}}}
}
func (s *Store) CreateSession(_ context.Context, v ports.Session) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Sessions[v.Hash] = v
	return nil
}
func (s *Store) WithSession(_ context.Context, hash string, fn func(*ports.Session) error) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	v, ok := s.Sessions[hash]
	if !ok {
		return ports.ErrNotFound
	}
	v.Checkpoint = func() error { saved := v; saved.Checkpoint = nil; s.Sessions[hash] = saved; return nil }
	if err := fn(&v); err != nil {
		return err
	}
	v.Checkpoint = nil
	s.Sessions[hash] = v
	return nil
}

func (s *Store) Membership(_ context.Context, _ string, _ int64) (ports.Membership, error) {
	if s.MissingMember {
		return ports.Membership{}, ports.ErrNotFound
	}
	return s.MembershipValue, nil
}

type Upstream struct {
	ProfileValue                                        ports.Profile
	LoginError, ProfileError, RefreshError, LogoutError error
	Challenge                                           bool
	Mu                                                  sync.Mutex
	RefreshCount, TwoFACount                            int
	TTL                                                 time.Duration
}

func NewUpstream() *Upstream {
	return &Upstream{ProfileValue: ports.Profile{ID: 42, Email: "fixture@example.invalid", Role: "user", Status: "active"}, TTL: time.Hour}
}
func (u *Upstream) tokens() ports.Tokens {
	return ports.Tokens{AccessToken: "fixture-access-canary", RefreshToken: "fixture-refresh-canary", ExpiresAt: time.Now().Add(u.TTL)}
}
func (u *Upstream) Login(context.Context, ports.LoginInput, ports.ClientInfo) (ports.LoginResult, error) {
	if u.LoginError != nil {
		return ports.LoginResult{}, u.LoginError
	}
	if u.Challenge {
		return ports.LoginResult{Challenge: "fixture-2fa-canary"}, nil
	}
	return ports.LoginResult{Tokens: u.tokens()}, nil
}
func (u *Upstream) Verify2FA(context.Context, string, string, ports.ClientInfo) (ports.Tokens, error) {
	u.Mu.Lock()
	u.TwoFACount++
	u.Mu.Unlock()
	return u.tokens(), nil
}
func (u *Upstream) Profile(context.Context, string, ports.ClientInfo) (ports.Profile, error) {
	return u.ProfileValue, u.ProfileError
}
func (u *Upstream) Refresh(context.Context, string, ports.ClientInfo) (ports.Tokens, error) {
	u.Mu.Lock()
	u.RefreshCount++
	u.Mu.Unlock()
	return u.tokens(), u.RefreshError
}
func (u *Upstream) Logout(context.Context, string, ports.ClientInfo) error { return u.LogoutError }
