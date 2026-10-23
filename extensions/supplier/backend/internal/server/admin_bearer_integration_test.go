//go:build integration

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/finance"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
)

func TestMainAdminBearerMutationsWithRealPostgres(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated database required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var database string
	if err = db.QueryRow(`SELECT current_database()`).Scan(&database); err != nil || database != "supplier_repository_test" {
		t.Fatal("wrong fixture")
	}
	if _, err = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	instance := "30000000-0000-0000-0000-000000000001"
	store := repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad")
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	vault, _ := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	role := "admin"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/proxies") {
			if r.Header.Get("x-api-key") != "synthetic-machine" {
				t.Error("resource adapter credential missing")
			}
			if strings.HasSuffix(r.URL.Path, "/test") {
				fmt.Fprint(w, `{"code":0,"data":{"success":true}}`)
			} else {
				fmt.Fprint(w, `{"code":0,"data":{"id":701,"protocol":"http","host":"8.8.8.8","port":1111,"username":"","password":"","status":"active","fallback_mode":"none"}}`)
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-main-admin" || r.Header.Get("x-api-key") != "" {
			t.Error("caller identity lost")
		}
		if r.URL.Path == "/api/v1/user/profile" {
			fmt.Fprintf(w, `{"code":0,"data":{"id":42,"email":"fixture@example.invalid","role":%q,"status":"active"}}`, role)
		} else {
			fmt.Fprint(w, `{"code":0,"data":{"required":false}}`)
		}
	}))
	defer up.Close()
	client, _ := sub2api.New(up.URL, nil)
	resourceClient, _ := sub2api.NewAccountClient(up.URL, "synthetic-machine")
	h, err := New(&auth.Service{Local: store}, Config{Origin: origin, AdminOrigin: "https://main.example.invalid", Suppliers: &suppliers.Service{Store: store, Vault: vault}, Admin: &auth.Service{Administrator: true, Upstream: client}, Finance: &finance.Service{Store: store, Vault: vault}, Accounts: &accounts.Service{Store: store, Upstream: resourceClient, Vault: vault, KeyID: "test", IdentityKey: bytes.Repeat([]byte{4}, 32), IdentityKeyID: "test-identity", QuarantineID: 1, ProxyCapacity: 1}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, key string, version int, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", "https://main.example.invalid")
		r.Header.Set("Authorization", "Bearer synthetic-main-admin")
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if version > 0 {
			r.Header.Set("If-Match", fmt.Sprint(version))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SyntheticPassword") {
			t.Fatal("password disclosed")
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Data
	}
	base := "/api/supplier/admin/v1/suppliers"
	create := `{"name":"Main vendor","login_account":"Main_vendor","initial_password":"SyntheticPassword2026"}`
	key := "40000000-0000-0000-0000-000000000001"
	row := call("POST", base, create, key, 0, 201)
	id := row["id"].(string)
	if replay := call("POST", base, create, key, 0, 200); replay["id"] != id {
		t.Fatal("duplicate creation")
	}
	if result := call("GET", base+"?search=Main_vendor&page=1&page_size=20", "", "", 0, 200); result["total"] != float64(1) {
		t.Fatal("search/pagination")
	}
	call("GET", base+"/"+id, "", "", 0, 200)
	call("GET", base+"/"+id+"/accounts", "", "", 0, 200)
	call("PUT", base+"/"+id+"/status", `{"enabled":false}`, "40000000-0000-0000-0000-000000000002", 1, 200)
	call("PUT", base+"/"+id+"/status", `{"enabled":true}`, "40000000-0000-0000-0000-000000000003", 1, 409)
	call("POST", base+"/"+id+"/credential-reset", `{"new_password":"SyntheticPassword2027"}`, "40000000-0000-0000-0000-000000000004", 2, 200)
	var audits, actors, sessions int
	if err = db.QueryRow(`SELECT count(*), count(*) FILTER(WHERE actor_user_id=42 AND actor_kind='admin' AND actor_id='42') FROM supplier.supplier_audit_events`).Scan(&audits, &actors); err != nil || audits != 3 || actors != 3 {
		t.Fatal("actual actor audit", audits, actors, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM supplier.supplier_sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("Bearer created a second session")
	}

	call("PUT", base+"/"+id+"/status", `{"enabled":true}`, "40000000-0000-0000-0000-000000000020", 3, 200)
	proxy := call("POST", base+"/"+id+"/proxies", `{"name":"Synthetic proxy","address":"http://8.8.8.8:1111","region":"US"}`, "40000000-0000-0000-0000-000000000021", 0, 201)
	proxyID := proxy["id"].(string)
	call("GET", base+"/"+id+"/proxies", "", "", 0, 200)
	call("POST", base+"/"+id+"/proxies/"+proxyID+"/test", `{}`, "", 0, 200)
	call("DELETE", base+"/"+id+"/proxies/"+proxyID, "", "", 0, 200)
	var proxyAudits int
	if err = db.QueryRow(`SELECT count(*) FROM supplier.supplier_audit_events WHERE actor_kind='admin' AND actor_user_id=42 AND action IN ('proxy.create','proxy.test','proxy.delete')`).Scan(&proxyAudits); err != nil || proxyAudits != 3 {
		t.Fatal("resource operator impersonated", proxyAudits, err)
	}
	// Synthetic funds exist only in this disposable test database.
	if _, err = db.Exec(`UPDATE supplier.supplier_balances SET unsettled_cents=2000 WHERE supplier_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	payout := `{"amount_usd":"1.23","payment_method":"usd_transfer","reference":"fixture-only","remark":""}`
	payoutKey := "40000000-0000-0000-0000-000000000010"
	settled := call("POST", base+"/"+id+"/settlements", payout, payoutKey, 0, 201)
	replayed := call("POST", base+"/"+id+"/settlements", payout, payoutKey, 0, 200)
	if settled["id"] != replayed["id"] {
		t.Fatal("settlement replay")
	}
	call("GET", "/api/supplier/admin/v1/finance/ledger?supplier_id="+id, "", "", 0, 200)
	call("GET", "/api/supplier/admin/v1/finance/settlements/"+settled["id"].(string), "", "", 0, 200)
	call("POST", base+"/"+id+"/settlements", `{"amount_usd":"0.001","payment_method":"other"}`, "40000000-0000-0000-0000-000000000011", 0, 400)
	call("GET", base+"/00000000-0000-0000-0000-000000000000/usage", "", "", 0, 404)
	call("GET", base+"/00000000-0000-0000-0000-000000000000/proxies", "", "", 0, 404)
	role = "user"
	call("GET", "/api/supplier/admin/v1/finance/ledger", "", "", 0, 403)
	call("POST", base+"/"+id+"/settlements", payout, payoutKey, 0, 403)
	call("GET", base+"/"+id+"/proxies", "", "", 0, 403)

	call("GET", base, "", "", 0, 403)
	call("POST", base, create, "40000000-0000-0000-0000-000000000005", 0, 403)
}
