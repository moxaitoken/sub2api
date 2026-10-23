package suppliers

import (
	"context"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"strings"
	"unicode/utf8"
)

type Service struct {
	Store ports.IdentityStore
	Vault *auth.Vault
}
type fault struct{ reason string }

func (f *fault) Error() string { return f.ReasonCode() }
func (*fault) StatusCode() int { return 400 }
func (f *fault) ReasonCode() string {
	if f.reason == "" {
		return "INVALID_SUPPLIER_INPUT"
	}
	return f.reason
}
func (s *Service) Mutate(ctx context.Context, actor int64, action, id, key, name, login, password, status string, version int64) (ports.ManagedSupplier, bool, error) {
	if actor <= 0 {
		return ports.ManagedSupplier{}, false, ports.ErrForbidden
	}
	name = strings.TrimSpace(name)
	login = strings.TrimSpace(login)
	normalized, valid := auth.NormalizeLogin(login)
	if action == "supplier.create" {
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
			return ports.ManagedSupplier{}, false, &fault{reason: "INVALID_SUPPLIER_NAME"}
		}
		if !valid {
			return ports.ManagedSupplier{}, false, &fault{reason: "INVALID_SUPPLIER_LOGIN"}
		}
	}
	if (action == "supplier.create" || action == "supplier.reset") && !auth.ValidPassword(password) {
		return ports.ManagedSupplier{}, false, &fault{reason: "INVALID_SUPPLIER_PASSWORD"}
	}
	if action == "supplier.status" && status != "active" && status != "disabled" {
		return ports.ManagedSupplier{}, false, &fault{}
	}
	if action != "supplier.create" && action != "supplier.reset" && action != "supplier.status" {
		return ports.ManagedSupplier{}, false, ports.ErrForbidden
	}
	payload, _ := json.Marshal([]any{action, id, name, normalized, password, status, version})
	hash := s.Vault.Fingerprint("supplier-mutation", payload)
	passwordHash := ""
	var err error
	if action == "supplier.create" || action == "supplier.reset" {
		passwordHash, err = auth.HashPassword(password)
		if err != nil {
			return ports.ManagedSupplier{}, false, err
		}
	}
	if action == "supplier.create" {
		id, err = auth.NewID()
		if err != nil {
			return ports.ManagedSupplier{}, false, err
		}
	}
	return s.Store.MutateSupplier(ctx, ports.SupplierMutation{ID: id, Action: action, Key: key, Hash: hash, Name: name, Login: login, PasswordHash: passwordHash, Status: status, ActorID: actor, Version: version})
}
