//go:build integration

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

func TestAccountConsumptionProjection(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated database required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture DB")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	s := New(db, testInstance, testSource)
	ctx := context.Background()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	one, _ := fixtureSupplier(t, s)
	two, _ := fixtureSupplier(t, s)
	a, b, foreign := fixtureID(t), fixtureID(t), fixtureID(t)
	for i, v := range []struct{ id, supplier string }{{a, one.ID}, {b, one.ID}, {foreign, two.ID}} {
		exec(`INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name,admission_state) VALUES($1,$2,$3,$4,'Metrics fixture','pending')`, v.id, v.supplier, testInstance, i+1)
	}
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, usage.Location)
	start, end, _ := usage.Window("today", now)
	initial, err := s.AccountConsumption(ctx, one.ID, []string{a, b, foreign}, now)
	if err != nil || len(initial) != 2 || initial[a].TodayCost != nil || initial[b].TotalCost != nil || initial[a].TotalStatus != "partial" {
		t.Fatal("unobserved or foreign values fabricated", initial, err)
	}
	insert := func(id int, at time.Time, cost string) {
		exec(`INSERT INTO supplier.supplier_usage_records(upstream_instance_id,upstream_usage_id,supplier_id,account_id,occurred_at,account_name_snapshot,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost,account_rate_multiplier) VALUES($1,$2,$3,$4,$5,'Metrics fixture','claude-fixture',1,2,3,4,$6,0.5)`, testInstance, id, one.ID, a, at, cost)
	}
	insert(1, start.Add(-time.Second), "0.2")
	insert(2, start, "0.100000000000000001")
	insert(3, end, "9")
	exec(`UPDATE supplier.supplier_accounts SET usage_history_start=$1::date,usage_discovered_at=$2 WHERE id=ANY($3::uuid[])`, start.AddDate(0, 0, -1).Format("2006-01-02"), now, "{"+a+","+b+"}")
	for _, id := range []string{a, b} {
		for _, day := range []time.Time{start.AddDate(0, 0, -1), start} {
			exec(`INSERT INTO supplier.supplier_sync_cursors(upstream_instance_id,supplier_id,account_id,usage_date,status,last_scan_at,last_reconciled_at) VALUES($1,$2,$3,$4,'reconciled',$5,$5)`, testInstance, one.ID, id, day.Format("2006-01-02"), now)
		}
	}
	values, err := s.AccountConsumption(ctx, one.ID, []string{a, b, foreign}, now)
	if err != nil {
		t.Fatal(err)
	}
	if *values[a].TodayCost != "0.100000000000000001" || *values[a].TotalCost != "0.300000000000000001" || values[a].TotalStatus != "complete" || values[a].TodayStatus != "complete" {
		t.Fatal("precision, Shanghai boundary or official-price basis incorrect", values[a])
	}
	if *values[b].TodayCost != "0" || *values[b].TotalCost != "0" || values[b].TotalStatus != "complete" {
		t.Fatal("observed zero lost", values[b])
	}
	exec(`DELETE FROM supplier.supplier_sync_cursors WHERE account_id=$1 AND usage_date=$2`, a, start.AddDate(0, 0, -1).Format("2006-01-02"))
	values, err = s.AccountConsumption(ctx, one.ID, []string{a}, now)
	if err != nil || values[a].TotalStatus != "partial" || values[a].TodayStatus != "complete" {
		t.Fatal("missing history not marked partial", values, err)
	}
	exec(`UPDATE supplier.supplier_sync_cursors SET last_scan_at=$1 WHERE account_id=$2`, now.Add(-time.Minute*3), a)
	values, err = s.AccountConsumption(ctx, one.ID, []string{a}, now)
	if err != nil || values[a].TodayStatus != "partial" || *values[a].TodayCost != "0.100000000000000001" {
		t.Fatal("stale sync erased data or claimed complete", values, err)
	}
	exec(`UPDATE supplier.supplier_accounts SET deleted_at=$1 WHERE id=$2`, now, a)
	values, err = s.AccountConsumption(ctx, one.ID, []string{a, foreign}, now)
	if err != nil || len(values) != 0 {
		t.Fatal("deleted or foreign account exposed", values, err)
	}
}
