package auth

import (
	"context"
	"errors"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"testing"
)

func TestAdministratorRequiresRoleButNotMembership(t *testing.T) {
	s, store, up := setup(t)
	s.Administrator = true
	store.MissingMember = true
	up.ProfileValue.Role = "admin"
	result := login(t, s)
	called := false
	if err := s.Do(context.Background(), result.Cookie, "", false, info, func(p Principal) error {
		called = true
		if p.Me.Supplier != nil {
			t.Fatal("admin invented supplier")
		}
		return nil
	}); err != nil || !called {
		t.Fatal(err)
	}
	up.ProfileValue.Role = "user"
	if err := s.Do(context.Background(), result.Cookie, "", false, info, func(Principal) error { return nil }); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("revoked admin accepted", err)
	}
	if _, err := s.Login(context.Background(), ports.LoginInput{Email: "fixture@example.invalid", Password: "synthetic"}, info); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("normal user admin login", err)
	}
}
