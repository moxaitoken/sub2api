//go:build integration

package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/finance"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func accrualFixture(t *testing.T, legacy bool) (*Store, ports.Supplier, ports.UsageAccount, time.Time) {
	t.Helper()
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
	t.Cleanup(func() { db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); db.Close() })
	if _, e = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
		t.Fatal(e)
	}
	s := New(db, testInstance, testSource)
	if legacy {
		files, e := migrationFiles()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`CREATE SCHEMA supplier; CREATE TABLE supplier.schema_migrations(version text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
			t.Fatal(e)
		}
		for _, f := range files {
			if f.name >= "010" {
				continue
			}
			if _, e = db.Exec(f.text); e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec(`INSERT INTO supplier.schema_migrations(version,checksum) VALUES($1,$2)`, f.name, f.checksum); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = db.Exec(`INSERT INTO supplier.upstream_instances(id,code,source_sha) VALUES($1,'fixture',$2)`, testInstance, testSource); e != nil {
			t.Fatal(e)
		}
	} else if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	owner, _ := fixtureSupplier(t, s)
	a := accrualAccount(t, s, owner.ID, 101)
	return s, owner, a, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
}
func accrualAccount(t *testing.T, s *Store, owner string, upstream int64) ports.UsageAccount {
	t.Helper()
	a := ports.UsageAccount{ID: fixtureID(t), SupplierID: owner, UpstreamID: upstream, Name: "Accrual fixture"}
	if _, e := s.db.Exec(`INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name,admission_state) VALUES($1,$2,$3,$4,$5,'pending')`, a.ID, owner, testInstance, upstream, a.Name); e != nil {
		t.Fatal(e)
	}
	return a
}
func TestOfficialAccrualPrecisionCorrectionsAndSettlement(t *testing.T) {
	s, owner, a, day := accrualFixture(t, false)
	ctx := context.Background()
	record := func(id int64, cost string) ports.UsageRecord {
		return ports.UsageRecord{ID: id, AccountID: a.UpstreamID, OccurredAt: day.Add(time.Hour), Model: "claude-fixture", Input: 1000000, Cost: cost}
	}
	rows := []ports.UsageRecord{record(1, "20")}
	syncRows := func() error {
		return s.SyncUsageWindow(ctx, owner.ID, a, day, day.AddDate(0, 0, 1), func(context.Context) ([]ports.UsageRecord, error) { return rows, nil })
	}
	check := func(unsettled, settled, remainder string, events int) {
		t.Helper()
		m, e := s.ManagedSupplier(ctx, owner.ID)
		if e != nil {
			t.Fatal(e)
		}
		var carry string
		var count int
		if e = s.db.QueryRow(`SELECT accrual_remainder::text FROM supplier.supplier_balances WHERE supplier_id=$1`, owner.ID).Scan(&carry); e != nil {
			t.Fatal(e)
		}
		if e = s.db.QueryRow(`SELECT count(*) FROM supplier.supplier_accrual_events WHERE supplier_id=$1`, owner.ID).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if m.Unsettled != unsettled || m.Settled != settled || carry != remainder || count != events {
			t.Fatalf("balance=%s/%s remainder=%s events=%d; want %s/%s %s %d", m.Unsettled, m.Settled, carry, count, unsettled, settled, remainder, events)
		}
	}
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("20.00", "0.00", "0.000000000000000000", 1)
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("20.00", "0.00", "0.000000000000000000", 1)
	vault, _ := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	svc := finance.Service{Store: s, Vault: vault}
	if _, _, e := svc.Settle(ctx, 42, owner.ID, fixtureID(t), ports.SettlementInput{Amount: "5", Method: "usd_transfer"}); e != nil {
		t.Fatal(e)
	}
	rows[0].Cost = "21"
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("16.00", "5.00", "0.000000000000000000", 2)
	rows[0].Cost = "20"
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("15.00", "5.00", "0.000000000000000000", 3)
	rows = append(rows, record(2, "0.004999999999999999"))
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("15.00", "5.00", "0.004999999999999999", 4)
	rows = append(rows, record(3, "0.004999999999999999"))
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("15.00", "5.00", "0.009999999999999998", 5)
	rows = append(rows, record(4, "0.000000000000000002"))
	if e := syncRows(); e != nil {
		t.Fatal(e)
	}
	check("15.01", "5.00", "0.000000000000000000", 6)
	rows[0].Cost = "1"
	if e := syncRows(); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("must reject correction below already settled amount", e)
	}
	var original string
	if e := s.db.QueryRow(`SELECT total_cost::text FROM supplier.supplier_usage_records WHERE upstream_instance_id=$1 AND upstream_usage_id=1`, testInstance).Scan(&original); e != nil || original != "20.000000000000000000" {
		t.Fatal("failed correction changed projection", original, e)
	}
	rows[0].Cost = "20"
	check("15.01", "5.00", "0.000000000000000000", 6)
	// A ledger failure must undo the source row, remainder and balance together.
	if _, e := s.db.Exec(`CREATE FUNCTION supplier.reject_accrual_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture event failure'; END $$; CREATE TRIGGER reject_accrual_fixture BEFORE INSERT ON supplier.supplier_accrual_events FOR EACH ROW EXECUTE FUNCTION supplier.reject_accrual_fixture()`); e != nil {
		t.Fatal(e)
	}
	rows = append(rows, record(5, "1"))
	if e := syncRows(); e == nil {
		t.Fatal("event failure ignored")
	}
	rows = rows[:4]
	if _, e := s.db.Exec(`DROP TRIGGER reject_accrual_fixture ON supplier.supplier_accrual_events`); e != nil {
		t.Fatal(e)
	}
	var exists bool
	if e := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM supplier.supplier_usage_records WHERE upstream_usage_id=5)`).Scan(&exists); e != nil || exists {
		t.Fatal("failed accrual persisted source", e)
	}
	check("15.01", "5.00", "0.000000000000000000", 6)
	rows = append(rows, record(6, "99999999999999999999"))
	if e := syncRows(); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("overflow accepted", e)
	}
	rows = rows[:4]
	check("15.01", "5.00", "0.000000000000000000", 6)
	// Different accounts and a settlement contend for the same balance lock.
	b := accrualAccount(t, s, owner.ID, 102)
	c := accrualAccount(t, s, owner.ID, 103)
	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for i, target := range []ports.UsageAccount{b, c} {
		wg.Add(1)
		go func(id int, a ports.UsageAccount) {
			defer wg.Done()
			r := record(int64(11+id), "1.11")
			r.AccountID = a.UpstreamID
			failures <- s.SyncUsageWindow(ctx, owner.ID, a, day, day.AddDate(0, 0, 1), func(context.Context) ([]ports.UsageRecord, error) { return []ports.UsageRecord{r}, nil })
		}(i, target)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, e := svc.Settle(ctx, 42, owner.ID, fixtureID(t), ports.SettlementInput{Amount: "10", Method: "usd_transfer"})
		failures <- e
	}()
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	check("7.23", "15.00", "0.000000000000000000", 8)
	other, _ := fixtureSupplier(t, s)
	foreign := accrualAccount(t, s, other.ID, 201)
	r := record(100, "2")
	r.AccountID = 201
	if e := s.SyncUsageWindow(ctx, other.ID, foreign, day, day.AddDate(0, 0, 1), func(context.Context) ([]ports.UsageRecord, error) { return []ports.UsageRecord{r}, nil }); e != nil {
		t.Fatal(e)
	}
	m, e := s.ManagedSupplier(ctx, other.ID)
	if e != nil || m.Unsettled != "2.00" {
		t.Fatal("foreign balance", m, e)
	}
	check("7.23", "15.00", "0.000000000000000000", 8)
	r.ID = 1
	if e = s.SyncUsageWindow(ctx, other.ID, foreign, day, day.AddDate(0, 0, 1), func(context.Context) ([]ports.UsageRecord, error) { return []ports.UsageRecord{r}, nil }); !errors.Is(e, ports.ErrConflict) {
		t.Fatal("source reassigned", e)
	}
	t.Log("20 standard => 20 payable; 5 settled => 15 unsettled; sub-cent carry exact; replay unchanged; corrections bounded; ledger failure atomic; parallel credits/settlement => 7.23 + 15.00")
}

func TestOfficialAccrualMigrationBackfill(t *testing.T) {
	s, owner, a, day := accrualFixture(t, true)
	ctx := context.Background()
	if _, e := s.db.Exec(`INSERT INTO supplier.supplier_balances(supplier_id,unsettled_cents,settled_cents) VALUES($1,1000,500)`, owner.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`INSERT INTO supplier.supplier_usage_records(upstream_instance_id,upstream_usage_id,supplier_id,account_id,occurred_at,account_name_snapshot,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost) VALUES($1,1,$2,$3,$4,'Fixture','claude-fixture',1000000,0,0,0,20.009999999999999999)`, testInstance, owner.ID, a.ID, day); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	m, e := s.ManagedSupplier(ctx, owner.ID)
	if e != nil || m.Unsettled != "30.00" || m.Settled != "5.00" {
		t.Fatal("backfill must preserve opening balance and settled", m, e)
	}
	var count int
	var carry, origin string
	if e = s.db.QueryRow(`SELECT count(*),min(origin) FROM supplier.supplier_accrual_events`).Scan(&count, &origin); e != nil || count != 1 || origin != "backfill" {
		t.Fatal("backfill duplicated", count, origin, e)
	}
	if e = s.db.QueryRow(`SELECT accrual_remainder::text FROM supplier.supplier_balances WHERE supplier_id=$1`, owner.ID).Scan(&carry); e != nil || carry != "0.009999999999999999" {
		t.Fatal("backfill precision", carry, e)
	}
	t.Log("009->010: retained 10.00 opening + 5.00 settled; historical 20.009999999999999999 booked once; payable=30.00; remainder=0.009999999999999999")
}

func TestOfficialAccrualMigrationFailureIsAtomic(t *testing.T) {
	s, owner, a, day := accrualFixture(t, true)
	ctx := context.Background()
	if _, e := s.db.Exec(`INSERT INTO supplier.supplier_balances(supplier_id,unsettled_cents,settled_cents) VALUES($1,1000,500)`, owner.ID); e != nil {
		t.Fatal(e)
	}
	// The first row credits successfully inside the migration; the second
	// exceeds bigint cents. Both the first credit and new DDL must roll back.
	if _, e := s.db.Exec(`INSERT INTO supplier.supplier_usage_records(upstream_instance_id,upstream_usage_id,supplier_id,account_id,occurred_at,account_name_snapshot,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost) SELECT $1,n,$2,$3,$4,'Fixture','claude-fixture',0,0,0,0,CASE WHEN n=1 THEN 20 ELSE 99999999999999999999 END FROM generate_series(1,2) n`, testInstance, owner.ID, a.ID, day); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e == nil {
		t.Fatal("overflowing backfill succeeded")
	}
	var migrations int
	var unsettled, settled int64
	var remainder string
	var noEvents bool
	if e := s.db.QueryRow(`SELECT count(*) FROM supplier.schema_migrations`).Scan(&migrations); e != nil || migrations != 9 {
		t.Fatal("failed migration was recorded", migrations, e)
	}
	if e := s.db.QueryRow(`SELECT unsettled_cents,settled_cents,accrual_remainder::text FROM supplier.supplier_balances WHERE supplier_id=$1`, owner.ID).Scan(&unsettled, &settled, &remainder); e != nil || unsettled != 1000 || settled != 500 || remainder != "0.000000000000" {
		t.Fatal("failed migration changed opening balance/precision", unsettled, settled, remainder, e)
	}
	if e := s.db.QueryRow(`SELECT to_regclass('supplier.supplier_accrual_events') IS NULL`).Scan(&noEvents); e != nil || !noEvents {
		t.Fatal("failed migration left new DDL", noEvents, e)
	}
	t.Log("overflow in historical backfill => migration count 9, original 10.00/5.00 balances and 12-place remainder retained, no accrual table")
}
