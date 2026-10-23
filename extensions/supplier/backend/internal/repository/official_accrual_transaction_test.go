//go:build integration

package repository

import (
	"context"
	"database/sql"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestOfficialAccrualTransaction(t *testing.T) {
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
	s := New(db, testInstance, testSource)
	ctx := context.Background()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	owner, _ := fixtureSupplier(t, s)
	account := fixtureID(t)
	if _, e = db.Exec(`INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name,admission_state) VALUES($1,$2,$3,101,'Official price fixture','pending')`, account, owner.ID, testInstance); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	a := ports.UsageAccount{ID: account, SupplierID: owner.ID, UpstreamID: 101, Name: "Official price fixture"}
	records := []ports.UsageRecord{{ID: 1, AccountID: 101, OccurredAt: day.Add(time.Hour), Model: "claude-fixture", Input: 1000000, Cost: "20.000000000000000000"}}
	e = s.SyncUsageWindow(ctx, owner.ID, a, day, day.AddDate(0, 0, 1), func(context.Context) ([]ports.UsageRecord, error) { return records, nil })
	if e != nil {
		t.Fatal(e)
	}
	balance, e := s.ManagedSupplier(ctx, owner.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("official_usage=20.00 unsettled=%s settled=%s total=%s", balance.Unsettled, balance.Settled, balance.Total)
	if balance.Unsettled != "20.00" || balance.Settled != "0.00" || balance.Total != "20.00" {
		t.Fatal("official usage must accrue 1:1")
	}
}
