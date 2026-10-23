package suppliers

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type passwordStore struct {
	ports.IdentityStore
	last ports.SupplierMutation
}

func (s *passwordStore) MutateSupplier(_ context.Context, in ports.SupplierMutation) (ports.ManagedSupplier, bool, error) {
	s.last = in
	return ports.ManagedSupplier{}, false, nil
}

func TestSupplierInputFieldDiagnosis(t *testing.T) {
	for _, tc := range []struct{ action, name, login, password, reason string }{
		{"supplier.create", "", "vendor_01", "FixturePassword!2026", "INVALID_SUPPLIER_NAME"},
		{"supplier.create", "Fixture", "vendor!", "FixturePassword!2026", "INVALID_SUPPLIER_LOGIN"},
		{"supplier.create", "Fixture", "vendor_01", "short1!", "INVALID_SUPPLIER_PASSWORD"},
		{"supplier.reset", "", "", "abcdefghij!", "INVALID_SUPPLIER_PASSWORD"},
	} {
		t.Run(tc.reason+tc.action, func(t *testing.T) {
			_, _, e := (&Service{}).Mutate(context.Background(), 42, tc.action, "", "fixture", tc.name, tc.login, tc.password, "", 1)
			var failure interface {
				StatusCode() int
				ReasonCode() string
			}
			if !errors.As(e, &failure) || failure.StatusCode() != 400 || failure.ReasonCode() != tc.reason {
				t.Fatalf("field diagnosis: got %v, want %s", e, tc.reason)
			}
		})
	}
}

func TestSpecialPasswordsPreservedOnCreateAndReset(t *testing.T) {
	vault, e := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	if e != nil {
		t.Fatal(e)
	}
	for _, password := range []string{"Aa1!@#$%^&*()_+-=[]{};:',.<>/?`~\"\\|", " Aa1!Password "} {
		for _, action := range []string{"supplier.create", "supplier.reset"} {
			store := &passwordStore{}
			s := &Service{Store: store, Vault: vault}
			if _, _, e := s.Mutate(context.Background(), 42, action, "fixture", "fixture", "Fixture", "vendor_01", password, "", 1); e != nil {
				t.Fatal(action, e)
			}
			if !auth.CheckPassword(password, store.last.PasswordHash) || auth.CheckPassword(strings.TrimSpace(password)+"changed", store.last.PasswordHash) {
				t.Fatal("password was transformed or hash accepted another input")
			}
			if password != strings.TrimSpace(password) && auth.CheckPassword(strings.TrimSpace(password), store.last.PasswordHash) {
				t.Fatal("password whitespace lost")
			}
		}
	}
}
