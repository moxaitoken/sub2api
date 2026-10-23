//go:build integration

package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/finance"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/url"
	"os"
	"sync"
	"testing"
)

func TestFinanceAtomicIdempotentAndConcurrent(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated database required")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture DB")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	ctx := context.Background()
	store := New(db, testInstance, testSource)
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	one, _ := fixtureSupplier(t, store)
	two, _ := fixtureSupplier(t, store)
	if _, e = db.Exec(`INSERT INTO supplier.supplier_balances(supplier_id,unsettled_cents) VALUES($1,10000),($2,5000)`, one.ID, two.ID); e != nil {
		t.Fatal(e)
	}
	vault, _ := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	svc := finance.Service{Store: store, Vault: vault}
	input := ports.SettlementInput{Amount: "30.01", Method: "usdt_transfer", Reference: "fixture-reference", Remark: "Synthetic test only"}
	key := fixtureID(t)
	out, replay, e := svc.Settle(ctx, 42, one.ID, key, input)
	if e != nil || replay || out.Before != "100.00" || out.After != "69.99" || out.Settled != "30.01" {
		t.Fatalf("settle %+v %t %v", out, replay, e)
	}
	again, replay, e := svc.Settle(ctx, 42, one.ID, key, input)
	if e != nil || !replay || again.ID != out.ID || again.LedgerID != out.LedgerID {
		t.Fatal("duplicate debit", again, replay, e)
	}
	input.Amount = "30.02"
	if _, _, e = svc.Settle(ctx, 42, one.ID, key, input); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("key reuse accepted", e)
	}
	input.Amount = "1.00"
	if _, _, e = svc.Settle(ctx, 42, two.ID, key, input); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("target reuse accepted", e)
	}
	// Failure after inserting the settlement must undo every financial state change.
	if _, e = db.Exec(`CREATE FUNCTION supplier.reject_test_ledger() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic ledger failure'; END $$; CREATE TRIGGER reject_test_ledger BEFORE INSERT ON supplier.supplier_finance_ledger FOR EACH ROW EXECUTE FUNCTION supplier.reject_test_ledger()`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.Settle(ctx, 42, one.ID, fixtureID(t), input); e == nil {
		t.Fatal("ledger failure ignored")
	}
	if _, e = db.Exec(`DROP TRIGGER reject_test_ledger ON supplier.supplier_finance_ledger`); e != nil {
		t.Fatal(e)
	}
	var balance, count, ledgers, audits int64
	db.QueryRow(`SELECT unsettled_cents FROM supplier.supplier_balances WHERE supplier_id=$1`, one.ID).Scan(&balance)
	db.QueryRow(`SELECT count(*) FROM supplier.supplier_settlements`).Scan(&count)
	if balance != 6999 || count != 1 {
		t.Fatal("partial transaction", balance, count)
	}
	// Two competing payouts cannot both spend the same current balance.
	input.Amount = "50.00"
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		k := fixtureID(t)
		go func() { defer wg.Done(); _, _, err := svc.Settle(ctx, 42, one.ID, k, input); errs <- err }()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ports.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("overspend", success, conflict)
	}
	// Disabled supplier identities can still receive a recorded offline settlement.
	if _, e = db.Exec(`UPDATE supplier.suppliers SET portal_status='disabled' WHERE id=$1`, one.ID); e != nil {
		t.Fatal(e)
	}
	input.Amount = "19.99"
	if _, _, e = svc.Settle(ctx, 42, one.ID, fixtureID(t), input); e != nil {
		t.Fatal("disabled settlement", e)
	}
	if _, _, e = svc.Settle(ctx, 42, one.ID, fixtureID(t), input); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("zero overspend", e)
	}
	report, e := store.Finance(ctx, ports.FinanceQuery{Page: 1, Size: 20})
	if e != nil || report.Total != 3 || report.Overview.Unsettled != "50.00" || report.Overview.Settled != "100.00" {
		t.Fatal("overview", report, e)
	}
	filtered, e := store.Finance(ctx, ports.FinanceQuery{SupplierID: one.ID, Search: "no-match", Page: 1, Size: 20})
	if e != nil || filtered.Total != 0 || filtered.Overview.Count != 3 || filtered.Overview.Settled != "100.00" {
		t.Fatal("keyword changed cards", filtered, e)
	}
	detail, e := store.Settlement(ctx, out.ID)
	if e != nil || detail.Reference != "fixture-reference" || detail.ActorID != 42 {
		t.Fatal("detail", detail, e)
	}
	db.QueryRow(`SELECT count(*) FROM supplier.supplier_finance_ledger`).Scan(&ledgers)
	db.QueryRow(`SELECT count(*) FROM supplier.supplier_audit_events WHERE actor_user_id=42 AND actor_kind='admin' AND action='supplier.settle'`).Scan(&audits)
	if ledgers != 3 || audits != 3 {
		t.Fatal("missing atomic ledger/audit", ledgers, audits)
	}
	t.Log("synthetic 100.00 => 69.99 => 19.99 => 0.00; duplicate=one debit; failure=full rollback; concurrent=one success/one conflict; records=ledger=audit=3")
}
