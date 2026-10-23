//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

func TestBackgroundUsageBackfill(t *testing.T) {
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
	if _, err = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	ctx := context.Background()
	s := New(db, testInstance, testSource)
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	one, _ := fixtureSupplier(t, s)
	two, _ := fixtureSupplier(t, s)
	newAccount := func(supplier string, upstream int64) string {
		id := fixtureID(t)
		_, e := db.Exec(`INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name,admission_state) VALUES($1,$2,$3,$4,$5,'pending')`, id, supplier, testInstance, upstream, fmt.Sprint("Account ", upstream))
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	now := time.Now().In(usage.Location)
	today, _, _ := usage.Window("today", now)
	a := newAccount(one.ID, 101)
	foreign := newAccount(two.ID, 201)
	// More than the old 20-account foreground ceiling.
	for i := 102; i <= 125; i++ {
		newAccount(one.ID, int64(i))
	}
	mode := "normal"
	failedOnce := false
	resumePage := 0
	appended := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		id, _ := strconv.Atoi(q.Get("account_id"))
		page, _ := strconv.Atoi(q.Get("page"))
		if id < 101 || (id > 125 && id != 201) || q.Get("user_id") != "" || q.Get("api_key_id") != "" || r.Header.Get("x-api-key") != "fixture-only" {
			t.Error("unscoped usage request")
		}
		first := today
		if id == 101 {
			first = today.AddDate(0, 0, -40)
		}
		if q.Get("start_date") == "" {
			if q.Get("sort_by") != "created_at" || q.Get("sort_order") != "asc" {
				t.Error("history discovery must find earliest timestamp")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"total": 1, "items": []map[string]any{{"account_id": id, "created_at": first}}}})
			return
		}
		if q.Get("start_date") != q.Get("end_date") || q.Get("sort_by") != "id" || q.Get("sort_order") != "asc" {
			t.Error("daily stable pagination missing")
		}
		day := q.Get("start_date")
		total := 0
		if day == today.Format("2006-01-02") {
			total = 1
			if id == 101 {
				total = 3001
			}
		}
		if id == 101 && day == first.Format("2006-01-02") {
			total = 1
		}
		if mode == "missing" && id == 101 && day == today.Format("2006-01-02") {
			total = 3000
		}
		if mode == "appending" && id == 101 && day == today.Format("2006-01-02") {
			if page > 1 {
				appended = true
			}
			if appended {
				total = 3101
			}
		}
		if id == 101 && page == 12 && !failedOnce && total > 100 {
			failedOnce = true
			w.WriteHeader(503)
			return
		}
		if id == 101 && page > 12 && total > 100 {
			resumePage = page
		}
		rows := []map[string]any{}
		for n := (page - 1) * 100; n < total && n < page*100; n++ {
			cost := "0.000000000000000001"
			if mode == "revised" && id == 101 && n == 0 && total > 100 {
				cost = "0.100000000000000001"
			}
			date, _ := time.ParseInLocation("2006-01-02", day, usage.Location)
			recordID := int64(id)*100000 + int64(n) + 1
			if date.Before(today) {
				recordID += 50000
			}
			rows = append(rows, map[string]any{"id": recordID, "account_id": id, "created_at": date.Add(time.Minute), "model": "claude-fixture", "input_tokens": 1, "output_tokens": 2, "cache_read_tokens": 3, "cache_creation_tokens": 4, "total_cost": json.Number(cost), "user_id": n%5 + 1, "api_key_id": n%7 + 1})
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"total": total, "items": rows}})
	}))
	defer up.Close()
	source, err := sub2api.NewAccountClient(up.URL, "fixture-only")
	if err != nil {
		t.Fatal(err)
	}
	svc := &usage.Service{Store: s, Source: source, Now: func() time.Time { return now }}
	run := func(pages int) {
		t.Helper()
		if e := svc.Tick(ctx, "worker-fixture", pages); e != nil {
			t.Fatal(e)
		}
	}
	// No browser request triggers synchronization.
	for i := 0; i < 20 && !failedOnce; i++ {
		run(20)
	}
	if !failedOnce {
		t.Fatal("large account did not reach retry page")
	}
	var checkpoint, retries int
	if err = db.QueryRow(`SELECT page,retry_count FROM supplier.supplier_sync_cursors WHERE account_id=$1 AND usage_date=$2`, a, today.Format("2006-01-02")).Scan(&checkpoint, &retries); err != nil || checkpoint != 12 || retries != 1 {
		t.Fatal("retry lost committed progress", checkpoint, retries, err)
	}
	q := ports.UsageQuery{Period: "today", Page: 1, Size: 100}
	before, e := svc.Read(ctx, one.ID, q)
	if e != nil || before.Status != "partial" || before.Summary.Records == "0" {
		t.Fatal("partial data hidden or falsely complete", e)
	}
	// Simulate process restart: a new service/owner resumes persisted page 12.
	svc = &usage.Service{Store: New(db, testInstance, testSource), Source: source, Now: func() time.Time { return now }}
	if _, err = db.Exec(`UPDATE supplier.supplier_sync_cursors SET next_retry_at=now() WHERE status='retry_wait'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 15; i++ {
		run(50)
	}
	v, err := svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "complete" || v.Total != 25 || v.Summary.Records != "3025" || v.Summary.Tokens != "9075" || v.Summary.Cache != "21175" || v.Summary.Cost != "0.000000000000003025" || resumePage < 31 {
		t.Fatalf("full supplier totals not reconciled: %+v resume=%d err=%v", v, resumePage, err)
	}
	q.Period = "all"
	v, err = svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "complete" || len(v.Daily) != 41 || v.Summary.Records != "3026" {
		t.Fatalf("historical backfill missing %+v %v", v, err)
	}
	q.AccountID = foreign
	if _, err = svc.Read(ctx, one.ID, q); err != ports.ErrNotFound {
		t.Fatal("foreign history visible", err)
	}
	q.AccountID = a
	q.Period = "today"
	force := func() {
		t.Helper()
		_, e := db.Exec(`UPDATE supplier.supplier_sync_cursors SET next_retry_at=now() WHERE account_id=$1 AND usage_date=$2`, a, today.Format("2006-01-02"))
		if e != nil {
			t.Fatal(e)
		}
	}
	mode = "revised"
	force()
	run(70)
	v, err = svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "complete" || v.Summary.Cost != "0.100000000000003001" || v.Summary.Records != "3001" {
		t.Fatal("revised consumption duplicated or not reconciled", v.Summary, err)
	}
	mode = "appending"
	force()
	run(64)
	v, err = svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "partial" || v.Summary.Records != "3001" {
		t.Fatal("concurrent append starved progress or falsely reconciled", v.Summary, err)
	}
	force()
	run(70)
	v, err = svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "complete" || v.Summary.Records != "3101" {
		t.Fatal("appended tail not eventually reconciled", v.Summary, err)
	}
	mode = "missing"
	force()
	run(70)
	v, err = svc.Read(ctx, one.ID, q)
	if err != nil || v.Status != "partial" || v.Summary.Records != "3101" {
		t.Fatal("source deletion erased history or passed reconciliation", v.Summary, err)
	}
	// Fencing: a crashed owner's result cannot overwrite a replacement worker.
	force()
	j, found, err := s.ClaimUsagePage(ctx, "crashed-owner")
	if err != nil || !found {
		t.Fatal("lease not claimed", err)
	}
	if _, err = db.Exec(`UPDATE supplier.supplier_sync_cursors SET lease_until=now()-interval '1 second' WHERE account_id=$1 AND usage_date=$2`, j.Account.ID, j.Day.Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	k, found, err := s.ClaimUsagePage(ctx, "replacement-owner")
	if err != nil || !found {
		t.Fatal("expired lease not recovered", err)
	}
	if err = s.ApplyUsagePage(ctx, j, ports.UsageSourcePage{}); err == nil {
		t.Fatal("stale owner committed")
	}
	if err = s.RetryUsagePage(ctx, k); err != nil {
		t.Fatal(err)
	}
	t.Log("background only: 25 owned accounts, 3001 records/day, mixed customers, exact costs/tokens, page retry/restart, 41-day history, source revisions/gaps, tenant isolation and stale-lease fencing verified")
}
