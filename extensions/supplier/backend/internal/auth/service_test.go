package auth

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
)

func setup(t *testing.T) (*Service, *testutil.Store, *testutil.Upstream) {
	t.Helper()
	v, err := NewVault(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := testutil.NewStore()
	up := testutil.NewUpstream()
	return &Service{Store: store, Upstream: up, Vault: v, InstanceID: "30000000-0000-0000-0000-000000000001", KeyID: "fixture-v1"}, store, up
}

var info = ports.ClientInfo{IP: "192.0.2.1", UserAgent: "fixture-agent"}

func login(t *testing.T, s *Service) Login {
	t.Helper()
	v, err := s.Login(context.Background(), ports.LoginInput{Email: "fixture@example.invalid", Password: "synthetic-test-only"}, info)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLoginRejectsMissingOrDisabledMembership(t *testing.T) {
	for _, kind := range []string{"missing", "member", "supplier"} {
		t.Run(kind, func(t *testing.T) {
			s, store, _ := setup(t)
			switch kind {
			case "missing":
				store.MissingMember = true
			case "member":
				store.MembershipValue.Member.Status = "disabled"
			case "supplier":
				store.MembershipValue.Supplier.PortalStatus = "disabled"
			}
			if _, err := s.Login(context.Background(), ports.LoginInput{}, info); err == nil {
				t.Fatal("inactive membership accepted")
			}
			if len(store.Sessions) != 0 {
				t.Fatal("created unauthorized session")
			}
		})
	}
}
func TestCiphertextAndFreshIdentityChecks(t *testing.T) {
	s, store, up := setup(t)
	l := login(t, s)
	session := store.Sessions[Hash(l.Cookie)]
	if bytes.Contains(session.Cipher, []byte("fixture-access-canary")) {
		t.Fatal("plaintext credentials stored")
	}
	var csrf string
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(p Principal) error { csrf = p.Me.CSRFToken; return nil }); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := s.Do(context.Background(), l.Cookie, "wrong", true, info, func(Principal) error { called = true; return nil }); err == nil || called {
		t.Fatal("CSRF not enforced")
	}
	if err := s.Do(context.Background(), l.Cookie, csrf, true, info, func(Principal) error { return nil }); err != nil {
		t.Fatal(err)
	}
	up.ProfileValue.Status = "disabled"
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { t.Fatal("inactive user reached handler"); return nil }); err == nil {
		t.Fatal("inactive upstream accepted")
	}
	if !store.Sessions[Hash(l.Cookie)].Revoked {
		t.Fatal("inactive session not revoked")
	}
}
func TestPortalDisableIsCheckedOnEveryRequest(t *testing.T) {
	s, store, _ := setup(t)
	l := login(t, s)
	store.MembershipValue.Supplier.PortalStatus = "disabled"
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { t.Fatal("disabled supplier reached handler"); return nil }); err == nil {
		t.Fatal("disable not enforced")
	}
}
func TestTwoFAIsBrowserBoundAndSingleUse(t *testing.T) {
	s, _, up := setup(t)
	up.Challenge = true
	l := login(t, s)
	if !l.Requires2FA {
		t.Fatal("2FA challenge absent")
	}
	other := info
	other.UserAgent = "different"
	if _, err := s.Complete2FA(context.Background(), l.Cookie, l.ChallengeID, "123456", other); err == nil {
		t.Fatal("challenge not browser bound")
	}
	if _, err := s.Complete2FA(context.Background(), l.Cookie, l.ChallengeID, "123456", info); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete2FA(context.Background(), l.Cookie, l.ChallengeID, "123456", info); err == nil {
		t.Fatal("challenge replay accepted")
	}
	if up.TwoFACount != 1 {
		t.Fatal("upstream challenge consumed more than once")
	}
}
func TestConcurrentRefreshHappensOnlyOnce(t *testing.T) {
	s, _, up := setup(t)
	up.TTL = 10 * time.Second
	l := login(t, s)
	up.TTL = time.Hour
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if up.RefreshCount != 1 {
		t.Fatalf("expected one refresh, got %d", up.RefreshCount)
	}
}
func TestRefreshFailureRevokesAndDoesNotRetry(t *testing.T) {
	s, store, up := setup(t)
	up.TTL = 10 * time.Second
	l := login(t, s)
	up.RefreshError = ports.ErrUnavailable
	for i := 0; i < 2; i++ {
		if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err == nil {
			t.Fatal("refresh failure accepted")
		}
	}
	if !store.Sessions[Hash(l.Cookie)].Revoked || up.RefreshCount != 1 {
		t.Fatal("unknown refresh result retried")
	}
}

func TestKnownRefreshResultSurvivesTemporaryProfileFailure(t *testing.T) {
	s, _, up := setup(t)
	up.TTL = 10 * time.Second
	l := login(t, s)
	up.TTL = time.Hour
	up.ProfileError = ports.ErrUnavailable
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err == nil {
		t.Fatal("temporary profile failure accepted")
	}
	up.ProfileError = nil
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if up.RefreshCount != 1 {
		t.Fatal("known rotated refresh token was lost")
	}
}

func TestExternalPendingAfterCrashNeverReplaysRefresh(t *testing.T) {
	s, store, up := setup(t)
	l := login(t, s)
	v := store.Sessions[Hash(l.Cookie)]
	v.ExternalPending = true
	store.Sessions[Hash(l.Cookie)] = v
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err == nil {
		t.Fatal("unknown external consumption accepted")
	}
	if up.RefreshCount != 0 || !store.Sessions[Hash(l.Cookie)].Revoked {
		t.Fatal("unknown refresh was retried")
	}
}
func TestLogoutRevokesDespiteUpstreamFailure(t *testing.T) {
	s, store, up := setup(t)
	l := login(t, s)
	var csrf string
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(p Principal) error { csrf = p.Me.CSRFToken; return nil }); err != nil {
		t.Fatal(err)
	}
	up.LogoutError = ports.ErrUnavailable
	if err := s.Logout(context.Background(), l.Cookie, csrf, info); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatal("upstream failure should be visible")
	}
	if !store.Sessions[Hash(l.Cookie)].Revoked {
		t.Fatal("logout kept local session")
	}
}
func TestIdleAndAbsoluteExpiry(t *testing.T) {
	for _, elapsed := range []time.Duration{31 * time.Minute, 9 * time.Hour} {
		s, _, _ := setup(t)
		l := login(t, s)
		now := time.Now().Add(elapsed)
		s.Now = func() time.Time { return now }
		if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { return nil }); err == nil {
			t.Fatal("expired session accepted")
		}
	}
}
func TestAuthenticatedMutationRunsOutsideSessionLock(t *testing.T) {
	s, store, _ := setup(t)
	l := login(t, s)
	done := make(chan error, 1)
	go func() {
		done <- s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error {
			return store.WithSession(context.Background(), Hash(l.Cookie), func(v *ports.Session) error { v.Revoked = true; return nil })
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("business callback deadlocked session transaction")
	}
}

func TestSingleSupplierBindingAndNoAdminBypass(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		t.Run(role, func(t *testing.T) {
			s, store, up := setup(t)
			up.ProfileValue.Role = role
			store.MissingMember = true
			if _, err := s.Login(context.Background(), ports.LoginInput{}, info); !errors.Is(err, ports.ErrForbidden) {
				t.Fatal("unbound identity accepted", err)
			}
			store.MissingMember = false
			l := login(t, s)
			if store.Sessions[Hash(l.Cookie)].Kind != "supplier" {
				t.Fatal("admin received special session")
			}
			store.MembershipValue.Supplier.ID = "10000000-0000-0000-0000-000000000002"
			store.MembershipValue.Member.SupplierID = store.MembershipValue.Supplier.ID
			if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { t.Fatal("changed tenant reached handler"); return nil }); !errors.Is(err, ports.ErrForbidden) {
				t.Fatal("changed tenant accepted", err)
			}
			if !store.Sessions[Hash(l.Cookie)].Revoked {
				t.Fatal("changed binding did not revoke session")
			}
		})
	}
}
func TestInvalidBindingMetadataRejected(t *testing.T) {
	for _, kind := range []string{"role", "supplier_mismatch", "user_mismatch", "missing_id"} {
		t.Run(kind, func(t *testing.T) {
			s, store, _ := setup(t)
			switch kind {
			case "role":
				store.MembershipValue.Member.Role = "viewer"
			case "supplier_mismatch":
				store.MembershipValue.Member.SupplierID = "another"
			case "user_mismatch":
				store.MembershipValue.Member.UpstreamUserID++
			case "missing_id":
				store.MembershipValue.Member.ID = ""
			}
			if _, err := s.Login(context.Background(), ports.LoginInput{}, info); !errors.Is(err, ports.ErrForbidden) {
				t.Fatal("invalid binding accepted", err)
			}
		})
	}
}
func TestHistoricalOpsSessionCannotAuthorize(t *testing.T) {
	s, store, _ := setup(t)
	l := login(t, s)
	v := store.Sessions[Hash(l.Cookie)]
	v.Kind = "ops"
	store.Sessions[Hash(l.Cookie)] = v
	if err := s.Do(context.Background(), l.Cookie, "", false, info, func(Principal) error { t.Fatal("retired ops reached handler"); return nil }); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal(err)
	}
}
