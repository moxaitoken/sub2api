//go:build integration

package accounts

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
)

func TestAccountsWithRealPostgres(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated database required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	ctx := context.Background()
	var actual string
	if err = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != "supplier_repository_test" {
		t.Fatal("wrong fixture database")
	}
	if _, err = db.ExecContext(ctx, `DROP SCHEMA IF EXISTS supplier CASCADE`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
			t.Error(e)
		}
	}()
	instance := "30000000-0000-0000-0000-000000000001"
	supplier := "10000000-0000-0000-0000-000000000001"
	other := "10000000-0000-0000-0000-000000000002"
	store := repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad")
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO supplier.suppliers(id,name) VALUES($1,'Fixture A'),($2,'Fixture B')`, supplier, other); err != nil {
		t.Fatal(err)
	}
	for i, pair := range []struct {
		id    string
		actor int64
	}{{supplier, 42}, {other, 43}} {
		bid, _ := auth.NewID()
		if _, err = db.ExecContext(ctx, `INSERT INTO supplier.supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES($1,$2,$3,$4,'owner')`, bid, pair.id, instance, pair.actor); err != nil {
			t.Fatal(i, err)
		}
	}
	s, _, remote := fixture(t)
	s.Store = store
	key, _ := auth.NewID()
	o, err := s.Begin(ctx, supplier, 42, "Persistent Claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, e := store.OAuth(ctx, other, 43, o.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal("cross supplier OAuth", e)
	}
	op, err := s.Complete(ctx, supplier, 42, o.ID, "code#fixture-state", key)
	if err != nil || op.State != "succeeded" {
		t.Fatalf("complete: %+v %v", op, err)
	}
	rows, total, err := store.Accounts(ctx, supplier, 1, 20)
	if err != nil || total != 1 || rows[0].Admission != "pending" || rows[0].Desired != "paused" {
		t.Fatal(rows, total, err)
	}
	if _, e := store.Account(ctx, other, rows[0].ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal("cross supplier account", e)
	}
	if _, e := store.AccountOperation(ctx, other, 43, op.ID); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal("cross supplier operation", e)
	}
	if _, total, e := store.Accounts(ctx, other, 1, 20); e != nil || total != 0 {
		t.Fatal("other tenant rows", total, e)
	}
	calls := len(remote.calls)
	again, err := s.Complete(ctx, supplier, 42, o.ID, "code#fixture-state", key)
	if err != nil || again.ID != op.ID || len(remote.calls) != calls {
		t.Fatal("durable replay", err)
	}
	approveKey, _ := auth.NewID()
	admin := ports.Profile{ID: 42, Role: "admin", Status: "active"}
	approved, err := s.Change(ctx, supplier, admin, rows[0].ID, approveKey, rows[0].Version, true, "Fixture approved", func(context.Context) error { return nil })
	if err != nil || approved.State != "succeeded" {
		t.Fatal("approve", approved, err)
	}
	a, err := store.Account(ctx, supplier, rows[0].ID)
	if err != nil || a.Admission != "approved" || a.Desired != "running" {
		t.Fatal("approval persistence", a, err)
	}
	pauseKey, _ := auth.NewID()
	paused, err := s.Change(ctx, supplier, admin, a.ID, pauseKey, a.Version, false, "Fixture pause", nil)
	if err != nil || paused.State != "succeeded" || remote.current.Schedulable {
		t.Fatal("pause", paused, err)
	}
	var audit string
	if err = db.QueryRowContext(ctx, `SELECT string_agg(safe_summary::text,'') FROM supplier.supplier_audit_events`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit, "Fixture approved") {
		t.Fatal("review reason missing from audit")
	}
	for _, secret := range []string{"access-canary", "refresh-canary", "code#fixture-state", "upstream-session-canary"} {
		if strings.Contains(audit, secret) {
			t.Fatal("secret in audit")
		}
	}
	var cipherSize int
	if err = db.QueryRowContext(ctx, `SELECT octet_length(upstream_session_cipher) FROM supplier.supplier_oauth_sessions WHERE id=$1`, o.ID).Scan(&cipherSize); err != nil || cipherSize != 0 {
		t.Fatal("OAuth ciphertext not cleared", cipherSize, err)
	}
	t.Run("concurrent reservation and expiry", func(t *testing.T) {
		second, e := s.Begin(ctx, supplier, 42, "Concurrent fixture")
		if e != nil {
			t.Fatal(e)
		}
		record, e := store.OAuth(ctx, supplier, 42, second.ID)
		if e != nil {
			t.Fatal(e)
		}
		var winners atomic.Int32
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id, _ := auth.NewID()
				_, e := store.ReserveAccount(ctx, id, record, auth.Hash("one-concurrent-identity"), "fixture")
				if e == nil {
					winners.Add(1)
				} else if !errors.Is(e, ports.ErrConflict) {
					t.Error(e)
				}
			}()
		}
		wg.Wait()
		if winners.Load() != 1 {
			t.Fatal("identity reservation winners", winners.Load())
		}
		if _, e = db.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, second.ID); e != nil {
			t.Fatal(e)
		}
		id, _ := auth.NewID()
		key, _ := auth.NewID()
		if _, _, e = store.BeginAccountOperation(ctx, ports.AccountOperation{ID: id, SupplierID: supplier, ActorID: 42, ResourceID: second.ID, Action: "oauth.complete", Key: key, Hash: auth.Hash("expired")}); !errors.Is(e, ports.ErrConflict) {
			t.Fatal("expired OAuth accepted", e)
		}
	})
	t.Run("disable prevents new OAuth persistence", func(t *testing.T) {
		if _, e := db.ExecContext(ctx, `UPDATE supplier.suppliers SET portal_status='disabled' WHERE id=$1`, supplier); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Begin(ctx, supplier, 42, "Forbidden"); !errors.Is(e, ports.ErrForbidden) {
			t.Fatal(e)
		}
	})
}
