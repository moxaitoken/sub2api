//go:build integration

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestR2IdentityWithRealPostgres(t *testing.T) {
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
	var database string
	if e = db.QueryRow(`SELECT current_database()`).Scan(&database); e != nil || database != "supplier_repository_test" {
		t.Fatal("wrong fixture")
	}
	if _, e = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
			t.Error(e)
		}
	}()
	instance := "30000000-0000-0000-0000-000000000001"
	store := repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad")
	if e = store.Migrate(ctx); e != nil {
		t.Fatal("migrate", e)
	}
	db.SetMaxOpenConns(1)
	vault, e := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	if e != nil {
		t.Fatal(e)
	}
	up := testutil.NewUpstream()
	up.ProfileValue.Role = "admin"
	local := &auth.Service{Store: store, Local: store, Upstream: up, Vault: vault, InstanceID: instance, KeyID: "fixture"}
	admin := &auth.Service{Store: store, Upstream: up, Vault: vault, InstanceID: instance, KeyID: "fixture", Administrator: true}
	managed := &suppliers.Service{Store: store, Vault: vault}
	usageSource, e := sub2api.NewAccountClient("https://usage.example.invalid", "fixture-only")
	if e != nil {
		t.Fatal(e)
	}
	h, e := New(local, Config{Usage: &usage.Service{Store: store, Source: usageSource}, Origin: origin, Admin: admin, Suppliers: managed, Accounts: &accounts.Service{Store: store}})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, body string, cookie *http.Cookie, csrf, key, version string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("User-Agent", "fixture-agent")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if version != "" {
			r.Header.Set("If-Match", version)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	data := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var env struct {
			Data map[string]any `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &env); e != nil {
			t.Fatal(e)
		}
		return env.Data
	}
	want := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("HTTP %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	cookie := func(w *httptest.ResponseRecorder, name string) *http.Cookie {
		for _, c := range w.Result().Cookies() {
			if c.Name == name {
				return c
			}
		}
		t.Fatal("cookie missing", name)
		return nil
	}
	key := func() string {
		k, e := auth.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return k
	}
	// An actual admin is accepted without a supplier membership.
	w := call("POST", "/api/supplier/admin/v1/auth/login", `{"email":"fixture@example.invalid","password":"synthetic"}`, nil, "", "", "")
	want(w, 200)
	ac := cookie(w, "__Host-supplier_admin")
	w = call("GET", "/api/supplier/admin/v1/me", "", ac, "", "", "")
	want(w, 200)
	adminData := data(w)
	acsrf := adminData["csrf_token"].(string)
	if _, ok := adminData["supplier"]; ok {
		t.Fatal("admin fabricated supplier")
	}
	// Includes JSON-sensitive quotes/backslashes, symbols and significant spaces.
	encode := func(v map[string]string) string {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	initialPassword := "  FixturePassword1!@#$%^&*()[]{};:'\"\\|<>/?`~  "
	resetPassword := "  ResetPassword2!@#$%\"\\  "
	create := encode(map[string]string{"name": "Fixture vendor", "login_account": "Vendor_01", "initial_password": initialPassword})
	for _, tc := range []struct{ name, login, password, reason string }{
		{"", "vendor_01", initialPassword, "INVALID_SUPPLIER_NAME"},
		{"Fixture", "vendor!", initialPassword, "INVALID_SUPPLIER_LOGIN"},
		{"Fixture", "vendor_01", "short1!", "INVALID_SUPPLIER_PASSWORD"},
	} {
		bad := call("POST", "/api/supplier/admin/v1/suppliers", encode(map[string]string{"name": tc.name, "login_account": tc.login, "initial_password": tc.password}), ac, acsrf, key(), "")
		want(bad, 400)
		var failure struct {
			Reason string `json:"reason"`
		}
		if e := json.Unmarshal(bad.Body.Bytes(), &failure); e != nil || failure.Reason != tc.reason {
			t.Fatal("unsafe or missing field diagnosis", e)
		}
		if strings.Contains(bad.Body.String(), tc.password) {
			t.Fatal("error echoed password")
		}
	}
	createKey := key()
	want(call("POST", "/api/supplier/admin/v1/suppliers", create, ac, "", createKey, ""), 403)
	w = call("POST", "/api/supplier/admin/v1/suppliers", create, ac, acsrf, createKey, "")
	want(w, 201)
	created := data(w)
	id := created["id"].(string)
	for _, f := range []string{"unsettled_usd", "settled_usd", "total_consumption_usd"} {
		if created[f] != "0.00" {
			t.Fatal("nonzero opening", f)
		}
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("password returned")
	}
	w = call("POST", "/api/supplier/admin/v1/suppliers", create, ac, acsrf, createKey, "")
	want(w, 200)
	if data(w)["id"] != id {
		t.Fatal("replay duplicate")
	}
	want(call("POST", "/api/supplier/admin/v1/suppliers", strings.ReplaceAll(create, "Fixture vendor", "Changed"), ac, acsrf, createKey, ""), 409)
	want(call("POST", "/api/supplier/admin/v1/suppliers", create, ac, acsrf, key(), ""), 409)
	w = call("GET", "/api/supplier/admin/v1/suppliers?search=Vendor_01", "", ac, "", "", "")
	want(w, 200)
	if data(w)["total"] != float64(1) {
		t.Fatal("search")
	}
	w = call("GET", "/api/supplier/admin/v1/suppliers?search=FixturePassword", "", ac, "", "", "")
	want(w, 200)
	if data(w)["total"] != float64(0) {
		t.Fatal("hidden secret searched")
	}
	// No upstream authentication is needed for local supplier credentials.
	up.LoginError = ports.ErrUnavailable
	login := encode(map[string]string{"login_account": " vendor_01 ", "password": initialPassword})
	w = call("POST", "/api/supplier/v1/auth/login", login, nil, "", "", "")
	want(w, 200)
	want(call("POST", "/api/supplier/v1/auth/login", encode(map[string]string{"login_account": "vendor_01", "password": strings.TrimSpace(initialPassword)}), nil, "", "", ""), 401)
	sc := cookie(w, "__Host-supplier_session")
	if !sc.Secure || !sc.HttpOnly || sc.SameSite != http.SameSiteLaxMode || sc.Path != "/" || time.Until(sc.Expires) < 7*24*time.Hour-5*time.Second || time.Until(sc.Expires) > 7*24*time.Hour {
		t.Fatal("supplier cookie must retain protections and expire in seven days")
	}
	w = call("GET", "/api/supplier/v1/me", "", sc, "", "", "")
	want(w, 200)
	supplierData := data(w)
	scsrf := supplierData["csrf_token"].(string)
	if supplierData["principal"].(map[string]any)["kind"] != "supplier" {
		t.Fatal("role spoof")
	}
	want(call("GET", "/api/supplier/admin/v1/suppliers", "", sc, "", "", ""), 401)
	want(call("GET", "/api/supplier/v1/me", "", ac, "", "", ""), 401)
	w = call("GET", "/api/supplier/v1/accounts", "", sc, "", "", "")
	want(w, 200)
	if data(w)["total"] != float64(0) {
		t.Fatal("seed accounts")
	}

	// Usage stays under local identity, never a browser-supplied supplier scope.
	want(call("GET", "/api/supplier/v1/usage", "", nil, "", "", ""), 401)
	want(call("GET", "/api/supplier/v1/usage", "", ac, "", "", ""), 401)
	w = call("GET", "/api/supplier/v1/usage?period=7d", "", sc, "", "", "")
	want(w, 200)
	usageData := data(w)
	if usageData["status"] != "complete" || usageData["request_attempts"] != nil || len(usageData["daily"].([]any)) != 7 {
		t.Fatal("usage zero scope", usageData)
	}
	for _, query := range []string{"period=year", "supplier_id=" + id, "period=today&period=7d", "page=-1"} {
		want(call("GET", "/api/supplier/v1/usage?"+query, "", sc, "", "", ""), 400)
	}
	want(call("GET", "/api/supplier/v1/usage?account_id=10000000-0000-4000-8000-000000000099", "", sc, "", "", ""), 404)
	want(call("GET", "/api/supplier/v1/review/accounts", "", sc, "", "", ""), 404)
	want(call("GET", "/api/supplier/admin/v1/suppliers/10000000-0000-0000-0000-000000000099/accounts", "", ac, "", "", ""), 404)
	// Disable revokes, leaves account/history/zero balance intact, and admin still accesses.
	want(call("PUT", "/api/supplier/admin/v1/suppliers/"+id+"/status", `{"enabled":false}`, ac, acsrf, key(), "99"), 409)
	w = call("PUT", "/api/supplier/admin/v1/suppliers/"+id+"/status", `{"enabled":false}`, ac, acsrf, key(), "1")
	want(w, 200)
	want(call("GET", "/api/supplier/v1/me", "", sc, "", "", ""), 401)
	want(call("POST", "/api/supplier/v1/auth/login", login, nil, "", "", ""), 401)
	want(call("GET", "/api/supplier/admin/v1/suppliers/"+id+"/accounts", "", ac, "", "", ""), 200)
	want(call("PUT", "/api/supplier/admin/v1/suppliers/"+id+"/status", `{"enabled":true}`, ac, acsrf, key(), "2"), 200)
	want(call("GET", "/api/supplier/v1/me", "", sc, "", "", ""), 401)
	w = call("POST", "/api/supplier/v1/auth/login", login, nil, "", "", "")
	want(w, 200)
	sc = cookie(w, "__Host-supplier_session")
	want(call("POST", "/api/supplier/admin/v1/suppliers/"+id+"/credential-reset", encode(map[string]string{"new_password": resetPassword}), ac, acsrf, key(), "3"), 200)
	want(call("GET", "/api/supplier/v1/me", "", sc, "", "", ""), 401)
	want(call("POST", "/api/supplier/v1/auth/login", login, nil, "", "", ""), 401)
	w = call("POST", "/api/supplier/v1/auth/login", encode(map[string]string{"login_account": "vendor_01", "password": resetPassword}), nil, "", "", "")
	want(w, 200)
	sc = cookie(w, "__Host-supplier_session")
	w = call("GET", "/api/supplier/v1/me", "", sc, "", "", "")
	want(w, 200)
	scsrf = data(w)["csrf_token"].(string)
	want(call("POST", "/api/supplier/v1/auth/logout", `{}`, sc, scsrf, "", ""), 200)
	want(call("GET", "/api/supplier/v1/me", "", sc, "", "", ""), 401)
	var audits string
	if e = db.QueryRow(`SELECT string_agg(safe_summary::text,'') FROM supplier.supplier_audit_events`).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(audits, "Password") {
		t.Fatal("password in audit")
	}
	// Concurrent case-insensitive uniqueness is enforced in the real database.
	db.SetMaxOpenConns(4)
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, name := range []string{"Concurrent_01", "concurrent_01"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, _, e := managed.Mutate(ctx, 42, "supplier.create", "", key(), "Concurrent", name, "FixturePassword!2026", "", 0)
			outcomes <- e
		}(name)
	}
	wg.Wait()
	close(outcomes)
	wins := 0
	for e := range outcomes {
		if e == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatal("duplicate concurrent accounts", wins)
	}
	// Fixed seven-day lifetime also survives an idle period through the real
	// session repository and HTTP route. Existing sessions are not extended.
	t.Run("supplier fixed lifetime through PostgreSQL and HTTP", func(t *testing.T) {
		start := time.Now().UTC().Truncate(time.Second)
		now := start
		local.Now = func() time.Time { return now }
		defer func() { local.Now = nil }()
		w := call("POST", "/api/supplier/v1/auth/login", encode(map[string]string{"login_account": "vendor_01", "password": resetPassword}), nil, "", "", "")
		want(w, 200)
		c := cookie(w, "__Host-supplier_session")
		var expiry time.Time
		if err := db.QueryRow(`SELECT expires_at FROM supplier.supplier_sessions WHERE session_hash=decode($1,'hex')`, auth.Hash(c.Value)).Scan(&expiry); err != nil || !expiry.Equal(start.Add(7*24*time.Hour)) || !c.Expires.Equal(expiry) {
			t.Fatal("cookie/persisted expiry differ", err)
		}
		for _, elapsed := range []time.Duration{31 * time.Minute, 6 * 24 * time.Hour, 7*24*time.Hour - time.Second} {
			now = start.Add(elapsed)
			want(call("GET", "/api/supplier/v1/me", "", c, "", "", ""), 200)
		}
		now = start.Add(7 * 24 * time.Hour)
		want(call("GET", "/api/supplier/v1/me", "", c, "", "", ""), 401)
		if err := db.QueryRow(`SELECT expires_at FROM supplier.supplier_sessions WHERE session_hash=decode($1,'hex')`, auth.Hash(c.Value)).Scan(&expiry); err != nil || !expiry.Equal(start.Add(7*24*time.Hour)) {
			t.Fatal("activity must not slide expiry", err)
		}
	})
	// Revoking the real admin role invalidates the management session.
	up.ProfileValue.Role = "user"
	want(call("GET", "/api/supplier/admin/v1/suppliers", "", ac, "", "", ""), 403)
	t.Log("R2 D1: unbound admin -> create zero supplier -> local non-email login -> disable/revoke -> enable -> reset/revoke -> logout; real PostgreSQL, synthetic main auth")
}
