package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type memory struct {
	sessions     map[string]ports.OAuthSession
	rows         map[string]ports.Account
	operations   map[string]ports.AccountOperation
	fingerprints map[string]bool
	eligible     bool
}

func (m *memory) Accounts(context.Context, string, int, int) ([]ports.Account, int64, error) {
	out := []ports.Account{}
	for _, a := range m.rows {
		out = append(out, a)
	}
	return out, int64(len(out)), nil
}
func (m *memory) Account(_ context.Context, s, id string) (ports.Account, error) {
	a, ok := m.rows[id]
	if !ok || a.SupplierID != s {
		return a, ports.ErrNotFound
	}
	return a, nil
}
func (m *memory) ReviewAccount(_ context.Context, id string) (ports.Account, error) {
	a, ok := m.rows[id]
	if !ok {
		return a, ports.ErrNotFound
	}
	return a, nil
}
func (m *memory) ReviewAccounts(ctx context.Context, p, n int) ([]ports.Account, int64, error) {
	return m.Accounts(ctx, "", p, n)
}
func (m *memory) CheckAccountScope(context.Context, string, int64, bool) error {
	if !m.eligible {
		return ports.ErrForbidden
	}
	return nil
}
func (m *memory) SaveOAuth(_ context.Context, o ports.OAuthSession) error {
	o.Status = "created"
	m.sessions[o.ID] = o
	return nil
}
func (m *memory) OAuth(_ context.Context, s string, actor int64, id string) (ports.OAuthSession, error) {
	o, ok := m.sessions[id]
	if !ok || o.SupplierID != s || o.ActorID != actor {
		return o, ports.ErrNotFound
	}
	return o, nil
}
func (m *memory) BeginAccountOperation(_ context.Context, o ports.AccountOperation) (ports.AccountOperation, bool, error) {
	for _, old := range m.operations {
		if old.SupplierID == o.SupplierID && old.ActorID == o.ActorID && old.Action == o.Action && old.Key == o.Key {
			if old.Hash != o.Hash {
				return o, false, ports.ErrConflict
			}
			return old, true, nil
		}
	}
	if o.Action == "oauth.complete" {
		s := m.sessions[o.ResourceID]
		if s.Status != "created" || time.Now().After(s.ExpiresAt) {
			return o, false, ports.ErrConflict
		}
		s.Status = "exchanging"
		m.sessions[s.ID] = s
	} else {
		if m.rows[o.ResourceID].Version != o.ExpectedVersion {
			return o, false, ports.ErrConflict
		}
	}
	o.State = "running"
	m.operations[o.ID] = o
	return o, false, nil
}
func (m *memory) CheckpointAccountOperation(_ context.Context, o ports.AccountOperation, state, step string) error {
	o.State = state
	o.Step = step
	m.operations[o.ID] = o
	return nil
}
func (m *memory) ReserveAccount(_ context.Context, id string, o ports.OAuthSession, f, key string) (ports.Account, error) {
	if m.fingerprints[f] {
		return ports.Account{}, ports.ErrConflict
	}
	m.fingerprints[f] = true
	a := ports.Account{ID: id, SupplierID: o.SupplierID, Name: o.Name, Admission: "provisioning", Desired: "paused", Version: 1}
	m.rows[id] = a
	return a, nil
}
func (m *memory) FinishAccount(_ context.Context, o ports.AccountOperation, a ports.Account, state, step string) error {
	if a.ID != "" {
		a.Version++
		m.rows[a.ID] = a
	}
	o.State = state
	o.Step = step
	o.AccountID = a.ID
	m.operations[o.ID] = o
	if o.Action == "oauth.complete" {
		s := m.sessions[o.ResourceID]
		s.Status = "completed"
		s.Cipher = nil
		m.sessions[s.ID] = s
	}
	return nil
}
func (m *memory) AccountOperation(_ context.Context, s string, actor int64, id string) (ports.AccountOperation, error) {
	o, ok := m.operations[id]
	if !ok || o.SupplierID != s || o.ActorID != actor {
		return o, ports.ErrNotFound
	}
	return o, nil
}

type remote struct {
	current       ports.RemoteAccount
	calls         []string
	fail          string
	token         ports.ClaudeTokens
	afterExchange func()
}

func (r *remote) CheckQuarantine(context.Context, int64) error {
	r.calls = append(r.calls, "quarantine")
	if r.fail == "quarantine" {
		return ports.ErrForbidden
	}
	return nil
}
func (r *remote) GenerateClaudeOAuth(context.Context) (ports.RemoteOAuth, error) {
	return ports.RemoteOAuth{URL: "https://claude.com/cai/oauth/authorize?state=fixture-state", SessionID: "upstream-session-canary"}, nil
}
func (r *remote) ExchangeClaudeOAuth(context.Context, string, string) (ports.ClaudeTokens, error) {
	r.calls = append(r.calls, "exchange")
	if r.afterExchange != nil {
		r.afterExchange()
	}
	if r.fail == "exchange" {
		return ports.ClaudeTokens{}, ports.ErrUnavailable
	}
	return r.token, nil
}
func (r *remote) CheckClaudeIdentity(context.Context, string) error {
	if r.fail == "identity" {
		return ports.ErrConflict
	}
	return nil
}
func (r *remote) CreateClaudeAccount(_ context.Context, name, key string, t ports.ClaudeTokens, q int64) (ports.RemoteAccount, error) {
	r.calls = append(r.calls, "create")
	r.current = ports.RemoteAccount{ID: 10, Name: name, Platform: "anthropic", Type: "oauth", Status: "active", Schedulable: true}
	r.current.Groups = append(r.current.Groups, struct {
		ID int64 `json:"id"`
	}{q})
	if r.fail == "create" {
		return ports.RemoteAccount{}, ports.ErrUnavailable
	}
	return r.current, nil
}
func (r *remote) ReadAccount(context.Context, int64) (ports.RemoteAccount, error) {
	r.calls = append(r.calls, "read")
	if r.fail == "read" {
		return ports.RemoteAccount{}, ports.ErrUnavailable
	}
	return r.current, nil
}
func (r *remote) SetAccountSchedulable(_ context.Context, _ int64, value bool) error {
	call := "disable"
	if value {
		call = "enable"
	}
	r.calls = append(r.calls, call)
	if r.fail == call {
		return ports.ErrUnavailable
	}
	r.current.Schedulable = value
	return nil
}
func (r *remote) SetAccountGroups(_ context.Context, _ int64, groups []int64) error {
	r.calls = append(r.calls, "groups")
	r.current.Groups = nil
	for _, id := range groups {
		r.current.Groups = append(r.current.Groups, struct {
			ID int64 `json:"id"`
		}{id})
	}
	return nil
}

func fixture(t *testing.T) (*Service, *memory, *remote) {
	t.Helper()
	v, e := auth.NewVault(bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	m := &memory{sessions: map[string]ports.OAuthSession{}, rows: map[string]ports.Account{}, operations: map[string]ports.AccountOperation{}, fingerprints: map[string]bool{}, eligible: true}
	r := &remote{token: ports.ClaudeTokens{Access: "access-canary", Refresh: "refresh-canary", AccountUUID: "40000000-0000-4000-8000-000000000001", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	return &Service{Store: m, Upstream: r, Vault: v, KeyID: "fixture", IdentityKey: bytes.Repeat([]byte{2}, 32), IdentityKeyID: "identity-fixture", QuarantineID: 1, ApprovedGroups: []int64{2}}, m, r
}
func complete(t *testing.T, s *Service) (ports.AccountOperation, error) {
	t.Helper()
	o, err := s.Begin(context.Background(), "supplier-a", 42, "Fixture account")
	if err != nil {
		t.Fatal(err)
	}
	return s.Complete(context.Background(), "supplier-a", 42, o.ID, "code#fixture-state", "complete-key")
}

func TestAccountPendingApprovePauseAndReplay(t *testing.T) {
	s, m, r := fixture(t)
	op, err := complete(t, s)
	if err != nil || op.State != "succeeded" {
		t.Fatal(op, err)
	}
	a := m.rows[op.AccountID]
	if a.Admission != "pending" || a.Desired != "paused" || r.current.Schedulable {
		t.Fatal("pending account not isolated")
	}
	var checks int
	verify := func(context.Context) error { checks++; return nil }
	admin := ports.Profile{ID: 42, Role: "admin", Status: "active"}
	op, err = s.Change(context.Background(), "supplier-a", admin, a.ID, "approve-key", a.Version, true, "fixture reason", verify)
	if err != nil || op.State != "succeeded" || !r.current.Schedulable || checks != 2 {
		t.Fatal(op, err, checks)
	}
	calls := len(r.calls)
	replay, err := s.Change(context.Background(), "supplier-a", admin, a.ID, "approve-key", a.Version, true, "fixture reason", verify)
	if err != nil || replay.ID != op.ID || len(r.calls) != calls {
		t.Fatal("approval replay touched upstream", err)
	}
	a = m.rows[a.ID]
	op, err = s.Change(context.Background(), "supplier-a", admin, a.ID, "pause-key", a.Version, false, "", nil)
	if err != nil || op.State != "succeeded" || r.current.Schedulable || m.rows[a.ID].Desired != "paused" {
		t.Fatal(op, err)
	}
	data, _ := json.Marshal(m.rows[a.ID])
	for _, secret := range []string{"access-canary", "refresh-canary", "upstream-session-canary"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("public DTO leaks secret")
		}
	}
}
func TestOAuthStateActorTenantAndSingleUse(t *testing.T) {
	s, _, r := fixture(t)
	o, err := s.Begin(context.Background(), "supplier-a", 42, "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		s     string
		actor int64
		code  string
	}{{"supplier-b", 42, "code#fixture-state"}, {"supplier-a", 43, "code#fixture-state"}, {"supplier-a", 42, "code#wrong-state"}} {
		if _, e := s.Complete(context.Background(), tc.s, tc.actor, o.ID, tc.code, "key"); e == nil {
			t.Fatal("invalid OAuth scope accepted")
		}
	}
	op, err := s.Complete(context.Background(), "supplier-a", 42, o.ID, "code#fixture-state", "key")
	if err != nil {
		t.Fatal(err)
	}
	calls := len(r.calls)
	replay, err := s.Complete(context.Background(), "supplier-a", 42, o.ID, "code#fixture-state", "key")
	if err != nil || replay.ID != op.ID || len(r.calls) != calls {
		t.Fatal("replayed exchange", err)
	}
	if _, err = s.Complete(context.Background(), "supplier-a", 42, o.ID, "code#fixture-state", "another-key"); err == nil {
		t.Fatal("consumed OAuth session reused")
	}
}
func TestUnknownExchangeAndCreateNeverAutomaticallyReplay(t *testing.T) {
	for _, stage := range []string{"exchange", "create", "read", "disable"} {
		t.Run(stage, func(t *testing.T) {
			s, m, r := fixture(t)
			r.fail = stage
			op, err := complete(t, s)
			if err == nil || op.State != "needs_attention" {
				t.Fatal(op, err)
			}
			calls := len(r.calls)
			replay, e := s.Complete(context.Background(), "supplier-a", 42, op.ResourceID, "code#fixture-state", "complete-key")
			if e != nil || replay.State != "needs_attention" || len(r.calls) != calls {
				t.Fatal("unsafe external replay", e)
			}
			for _, a := range m.rows {
				if a.Admission == "approved" {
					t.Fatal("failed creation approved")
				}
			}
		})
	}
}
func TestMissingIdentityAndRevocationCannotCreate(t *testing.T) {
	for _, kind := range []string{"identity", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			s, m, r := fixture(t)
			if kind == "identity" {
				r.token.AccountUUID = ""
			} else {
				r.afterExchange = func() { m.eligible = false }
			}
			_, err := complete(t, s)
			if err == nil {
				t.Fatal("invalid identity accepted")
			}
			for _, call := range r.calls {
				if call == "create" {
					t.Fatal("unauthorized account created")
				}
			}
		})
	}
}
func TestSupplierCannotReviewOrPauseAnotherTenant(t *testing.T) {
	s, m, r := fixture(t)
	op, err := complete(t, s)
	if err != nil {
		t.Fatal(err)
	}
	a := m.rows[op.AccountID]
	calls := len(r.calls)
	user := ports.Profile{ID: 42, Role: "user"}
	if _, e := s.Change(context.Background(), "supplier-a", user, a.ID, "review-key", a.Version, true, "fixture reason", func(context.Context) error { return nil }); !errors.Is(e, ports.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Change(context.Background(), "supplier-b", user, a.ID, "pause-key", a.Version, false, "", nil); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
	if len(r.calls) != calls {
		t.Fatal("cross-tenant operation touched upstream")
	}
}
func TestBusinessCiphertextCannotBeMovedAcrossResources(t *testing.T) {
	s, _, _ := fixture(t)
	cipher, e := s.Vault.SealBlob("oauth:supplier-a:id-a", []byte("secret"))
	if e != nil {
		t.Fatal(e)
	}
	for _, domain := range []string{"oauth:supplier-b:id-a", "oauth:supplier-a:id-b"} {
		if _, e = s.Vault.OpenBlob(domain, cipher); e == nil {
			t.Fatal("ciphertext reassigned")
		}
	}
}

func TestRejectedMutationDoesNotReturnAcceptedOperation(t *testing.T) {
	s, m, _ := fixture(t)
	op, err := complete(t, s)
	if err != nil {
		t.Fatal(err)
	}
	account := m.rows[op.AccountID]
	admin := ports.Profile{ID: 42, Role: "admin", Status: "active"}
	verify := func(context.Context) error { return nil }
	rejected, err := s.Change(context.Background(), "supplier-a", admin, account.ID, "stale-key", account.Version+1, true, "stale", verify)
	if !errors.Is(err, ports.ErrConflict) || rejected.ID != "" {
		t.Fatal("stale version reported as accepted", rejected, err)
	}
	_, err = s.Change(context.Background(), "supplier-a", admin, account.ID, "approve-key", account.Version, true, "first", verify)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err = s.Change(context.Background(), "supplier-a", admin, account.ID, "approve-key", account.Version, true, "changed", verify)
	if !errors.Is(err, ports.ErrConflict) || rejected.ID != "" {
		t.Fatal("changed idempotency payload reported as accepted", rejected, err)
	}
	rejected, err = s.Complete(context.Background(), "supplier-a", 42, op.ResourceID, "different#fixture-state", "complete-key")
	if !errors.Is(err, ports.ErrConflict) || rejected.ID != "" {
		t.Fatal("rejected OAuth replay reported as accepted", rejected, err)
	}
}

func TestInvalidIdentityAndAdminRevocationStayDisabled(t *testing.T) {
	t.Run("invalid identity", func(t *testing.T) {
		s, _, r := fixture(t)
		r.token.AccountUUID = "not-a-uuid"
		_, err := complete(t, s)
		if err == nil || strings.Contains(strings.Join(r.calls, ","), "create") {
			t.Fatal("invalid identity reached creation", err)
		}
	})
	t.Run("admin revoked before enable", func(t *testing.T) {
		s, m, r := fixture(t)
		op, err := complete(t, s)
		if err != nil {
			t.Fatal(err)
		}
		a := m.rows[op.AccountID]
		checks := 0
		verify := func(context.Context) error {
			checks++
			if checks == 2 {
				return ports.ErrForbidden
			}
			return nil
		}
		op, err = s.Change(context.Background(), "supplier-a", ports.Profile{ID: 42, Role: "admin"}, a.ID, "approval", a.Version, true, "fixture", verify)
		if err == nil || op.State != "needs_attention" || r.current.Schedulable {
			t.Fatal("revoked reviewer enabled account", op, err)
		}
		for _, call := range r.calls {
			if call == "enable" {
				t.Fatal("enable called after revocation")
			}
		}
	})
}
