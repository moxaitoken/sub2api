//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

func TestRealUsageProjection(t *testing.T) {
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
	ctx := context.Background()
	db.SetMaxOpenConns(8)
	if _, e = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	s := New(db, testInstance, testSource)
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	one, _ := fixtureSupplier(t, s)
	two, _ := fixtureSupplier(t, s)
	newAccount := func(supplier string, upstream int64) string {
		id := fixtureID(t)
		_, e = db.Exec(`INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name,admission_state) VALUES($1,$2,$3,$4,'Usage fixture','pending')`, id, supplier, testInstance, upstream)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	a := newAccount(one.ID, 101)
	b := newAccount(one.ID, 102)
	foreign := newAccount(two.ID, 201)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, usage.Location)
	start, end, _ := usage.Window("today", now)
	mode := "normal"
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/admin/usage" || q.Get("timezone") != "Asia/Shanghai" || q.Get("start_date") != "2026-10-22" || q.Get("end_date") != "2026-10-22" || q.Get("exact_total") != "true" || r.Header.Get("x-api-key") != "fixture-only" {
			t.Error("unscoped source query")
		}
		if mode == "fail" {
			w.WriteHeader(503)
			return
		}
		id := q.Get("account_id")
		if id != "101" && id != "102" {
			t.Error("foreign account fetched", id)
		}
		rows := []map[string]any{}
		if id == "101" && mode != "missing" {
			cost := "0.100000000000000001"
			if mode == "revised" {
				cost = "0.200000000000000001"
			}
			rows = append(rows, map[string]any{"id": 11, "account_id": 101, "created_at": start.Add(time.Minute), "model": "claude-fixture", "input_tokens": 10, "output_tokens": 20, "cache_read_tokens": 30, "cache_creation_tokens": 40, "total_cost": json.Number(cost), "user": map[string]any{"email": "never-leak@example.invalid"}, "ip_address": "never-leak"})
		}
		if id == "102" {
			rows = append(rows, map[string]any{"id": 12, "account_id": 102, "created_at": end.Add(-time.Second), "model": "claude-fixture", "input_tokens": 1, "output_tokens": 2, "cache_read_tokens": 3, "cache_creation_tokens": 4, "total_cost": json.Number("0.2")})
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"total": len(rows), "items": rows}})
	}))
	defer up.Close()
	source, e := sub2api.NewAccountClient(up.URL, "fixture-only")
	if e != nil {
		t.Fatal(e)
	}
	svc := &usage.Service{Store: s, Source: source, Now: func() time.Time { return now }}
	q := ports.UsageQuery{Period: "today", Page: 1, Size: 1}
	read := func() ports.UsageReport {
		t.Helper()
		v, e := svc.Read(ctx, one.ID, q)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	assertCost := func(v ports.UsageReport, want string) {
		t.Helper()
		x, ok := new(big.Rat).SetString(v.Summary.Cost)
		y, _ := new(big.Rat).SetString(want)
		if !ok || x.Cmp(y) != 0 {
			t.Fatalf("cost %s want %s", v.Summary.Cost, want)
		}
	}
	v := read()
	assertCost(v, "0.300000000000000001")
	if v.Status != "complete" || v.Summary.Records != "2" || v.Summary.Tokens != "33" || v.Summary.Cache != "77" || v.Total != 2 || v.Pages != 2 || len(v.Accounts) != 1 || v.Attempts != nil {
		t.Fatalf("incorrect report %+v", v)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "never-leak") || strings.Contains(string(encoded), "upstream_account_id") {
		t.Fatal("sensitive source fields leaked")
	}
	v = read()
	assertCost(v, "0.300000000000000001")
	if v.Summary.Records != "2" {
		t.Fatal("duplicate sync counted twice")
	}
	mode = "revised"
	v = read()
	assertCost(v, "0.400000000000000001")
	q.AccountID = a
	v = read()
	assertCost(v, "0.200000000000000001")
	if v.Total != 1 || v.Summary.Records != "1" {
		t.Fatal("single account scope")
	}
	q.AccountID = foreign
	before := calls
	if _, e = svc.Read(ctx, one.ID, q); !errors.Is(e, ports.ErrNotFound) || calls != before {
		t.Fatal("foreign supplier read not rejected before source")
	}
	q.AccountID = b
	mode = "fail"
	v = read()
	if v.Status != "partial" || v.Summary.Records != "1" {
		t.Fatal("failed sync erased cached data")
	}
	q.AccountID = a
	mode = "missing"
	v = read()
	if v.Status != "partial" || v.Summary.Records != "1" {
		t.Fatal("source retention gap erased history")
	}
	var status string
	if e = db.QueryRow(`SELECT status FROM supplier.supplier_sync_cursors WHERE account_id=$1`, a).Scan(&status); e != nil || status != "gap" {
		t.Fatal("gap not persisted", e, status)
	}
	// Serialized account refreshes cannot overlap or partially commit.
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('supplier-usage'),hashtext($1))`, a); e != nil {
		t.Fatal(e)
	}
	owned, e := s.UsageAccounts(ctx, one.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, account := range owned {
		if account.ID == a {
			e = s.SyncUsageWindow(ctx, one.ID, account, start, end, func(context.Context) ([]ports.UsageRecord, error) {
				t.Fatal("concurrent fetch executed")
				return nil, nil
			})
			if !errors.Is(e, ports.ErrUnavailable) {
				t.Fatal("lock not enforced", e)
			}
		}
	}
	tx.Rollback()

	// A malicious/reused source ID cannot reassign previously observed ownership.
	collision := ports.UsageAccount{ID: foreign, Name: "Foreign fixture", UpstreamID: 201}
	e = s.SyncUsageWindow(ctx, two.ID, collision, start, end, func(context.Context) ([]ports.UsageRecord, error) {
		return []ports.UsageRecord{{ID: 11, AccountID: 201, OccurredAt: start, Model: "claude-fixture", Cost: "9"}}, nil
	})
	if !errors.Is(e, ports.ErrConflict) {
		t.Fatal("source identity reassigned", e)
	}
	foreignReport, e := s.UsageReport(ctx, two.ID, ports.UsageQuery{Period: "today", Page: 1, Size: 20}, start, end)
	if e != nil || foreignReport.Summary.Records != "0" {
		t.Fatal("conflicting source write leaked", e)
	}
	// Archived accounts keep their history and remain filterable.
	if _, e = db.Exec(`UPDATE supplier.supplier_accounts SET admission_state='deleted',deleted_at=now() WHERE id=$1`, b); e != nil {
		t.Fatal(e)
	}
	mode = "normal"
	q.AccountID = b
	v = read()
	if !v.Accounts[0].Archived || v.Summary.Records != "1" {
		t.Fatal("archived history hidden")
	}
	sevenStart, sevenEnd, _ := usage.Window("7d", now)
	v, e = s.UsageReport(ctx, one.ID, ports.UsageQuery{Period: "7d", Page: 1, Size: 20}, sevenStart, sevenEnd)
	if e != nil || len(v.Daily) != 7 || v.Status != "partial" || v.Summary.Records != "2" {
		t.Fatal("unsynced days disguised as complete", e)
	}
	t.Log("real adapter -> isolated PostgreSQL: exact decimal, two accounts, Shanghai boundary, pagination, idempotency, revisions, tenant isolation, cached failure, retention gap, concurrent lock verified")
}
