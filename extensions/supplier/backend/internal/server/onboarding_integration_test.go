//go:build integration

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestR2LocalOnboarding(t *testing.T) {
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
	if _, e = db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`); e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS supplier CASCADE`)
	instance := "30000000-0000-0000-0000-000000000001"
	store := repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad")
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(4)
	vault, _ := auth.NewVault(bytes.Repeat([]byte{3}, 32))
	management := &suppliers.Service{Store: store, Vault: vault}
	key := func() string {
		x, e := auth.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	first, _, e := management.Mutate(ctx, 42, "supplier.create", "", key(), "Fixture One", "vendor_one", "FixturePassword!2026", "", 0)
	if e != nil {
		t.Fatal(e)
	}
	second, _, e := management.Mutate(ctx, 42, "supplier.create", "", key(), "Fixture Two", "vendor_two", "FixturePassword!2026", "", 0)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	proxies := map[int64]ports.RemoteProxy{}
	remoteAccounts := map[int64]ports.RemoteAccount{}
	generateCalls, exchangeCalls, createCalls := 0, 0, 0
	failCreate, failEnable := false, false
	failDelete, failProxyTest := false, false
	groupDeleted := false
	nextProxyID := int64(100)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("x-api-key") != "fixture-key" {
			t.Error("missing machine key")
		}
		w.Header().Set("Content-Type", "application/json")
		var data any
		switch {
		case r.URL.Path == "/api/v1/admin/accounts/runtime/batch":
			var input struct {
				IDs []int64 `json:"account_ids"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			values := map[int64]any{}
			for _, id := range input.IDs {
				if _, ok := remoteAccounts[id]; !ok {
					t.Error("unknown/foreign runtime ID", id)
				}
				values[id] = map[string]any{"rpm": 7, "concurrency": 2, "tokens": "13", "uncached_tokens": "5"}
			}
			data = map[string]any{"sampled_at": time.Now().UTC(), "rpm_window": "current_minute", "token_window_seconds": 60, "accounts": values}
		case r.URL.Path == "/api/v1/admin/accounts/usage/batch":
			var input struct {
				IDs   []int64 `json:"account_ids"`
				Force bool    `json:"force"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			if input.Force {
				t.Error("active quota request")
			}
			values := map[int64]any{}
			for _, id := range input.IDs {
				values[id] = map[string]any{"source": "passive", "updated_at": time.Now().UTC(), "five_hour": map[string]any{"utilization": 95, "resets_at": time.Now().Add(time.Hour)}}
			}
			data = map[string]any{"usage": values}
		case r.URL.Path == "/api/v1/admin/accounts/creation-capabilities":
			data = map[string]bool{"schedulable_on_create": true}
		case r.URL.Path == "/api/v1/admin/groups/1":
			data = map[string]any{"id": 1, "name": "Quarantine", "platform": "anthropic", "status": "inactive", "is_exclusive": true}
		case r.URL.Path == "/api/v1/admin/groups/1/api-keys":
			data = map[string]any{"items": []any{}, "total": 0}
		case r.URL.Path == "/api/v1/admin/groups/2":
			if groupDeleted {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			data = ports.AccountGroup{ID: 2, Name: "Configured fixture group", Platform: "anthropic", Status: "active"}
		case r.URL.Path == "/api/v1/admin/groups/6":
			w.WriteHeader(http.StatusNotFound)
			return
		case r.URL.Path == "/api/v1/admin/proxies" && r.Method == "POST":
			var in struct {
				ports.ProxyConfig
				Name     string
				Fallback string `json:"fallback_mode"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				t.Error("proxy input")
			}
			id := nextProxyID
			nextProxyID++
			p := ports.RemoteProxy{ProxyConfig: in.ProxyConfig, ID: id, Status: "active", Fallback: in.Fallback}
			proxies[id] = p
			data = p
		case strings.HasPrefix(r.URL.Path, "/api/v1/admin/proxies/"):
			var id int64
			fmt.Sscanf(r.URL.Path, "/api/v1/admin/proxies/%d", &id)
			p, ok := proxies[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			if r.Method == "DELETE" {
				delete(proxies, id)
				if failDelete {
					w.WriteHeader(503)
					return
				} // Lost response after remote deletion.
				data = map[string]bool{"deleted": true}
				break
			}
			if strings.HasSuffix(r.URL.Path, "/test") {
				data = map[string]bool{"success": !failProxyTest}
			} else {
				data = p
			}
		case r.URL.Path == "/api/v1/admin/accounts/generate-auth-url":
			var body map[string]int64
			json.NewDecoder(r.Body).Decode(&body)
			if body["proxy_id"] <= 0 {
				t.Error("generation without bound proxy")
			}
			generateCalls++
			data = ports.RemoteOAuth{URL: "https://claude.com/cai/oauth/authorize?state=fixture-state", SessionID: "fixture-session"}
		case r.URL.Path == "/api/v1/admin/accounts/exchange-code":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["proxy_id"].(float64) <= 0 {
				t.Error("exchange without proxy")
			}
			exchangeCalls++
			uuid := "11111111-1111-4111-8111-111111111111"
			if strings.Contains(body["code"].(string), "enablefail") {
				uuid = "33333333-3333-4333-8333-333333333333"
			}
			if strings.Contains(body["code"].(string), "unknown") {
				uuid = "22222222-2222-4222-8222-222222222222"
			}
			data = ports.ClaudeTokens{Access: "fixture-access-secret", Refresh: "fixture-refresh-secret", AccountUUID: uuid, Email: "fixture@example.invalid", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		case r.URL.Path == "/api/v1/admin/accounts" && r.Method == "GET":
			items := []ports.RemoteAccount{}
			for _, a := range remoteAccounts {
				items = append(items, a)
			}
			data = map[string]any{"items": items, "total": len(items)}
		case r.URL.Path == "/api/v1/admin/accounts" && r.Method == "POST":
			createCalls++
			var in struct {
				Schedulable *bool `json:"schedulable"`
				Name        string
				ProxyID     int64   `json:"proxy_id"`
				GroupIDs    []int64 `json:"group_ids"`
				Credentials map[string]any
				Concurrency int
				Extra       map[string]any
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.ProxyID <= 0 || len(in.GroupIDs) != 1 || in.GroupIDs[0] != 2 || in.Schedulable == nil || *in.Schedulable {
				t.Error("unsafe create")
			}
			if in.Name == "Real fixture account" && (in.Concurrency != 120 || in.Extra["max_sessions"] != float64(150) || in.Extra["base_rpm"] != float64(23) || in.Extra["session_idle_timeout_minutes"] != float64(8) || in.Extra["window_cost_limit"] != 12.34 || in.Extra["window_cost_sticky_reserve"] != float64(0)) {
				t.Errorf("supplier settings were not forwarded unchanged: concurrency=%d extra=%v", in.Concurrency, in.Extra)
			}
			if in.Name == "Unknown" && (in.Extra["max_sessions"] != nil || in.Extra["base_rpm"] != nil) {
				t.Error("disabled limits were re-enabled", in.Extra)
			}
			id := int64(200 + len(remoteAccounts))
			a := ports.RemoteAccount{ID: id, Name: in.Name, Platform: "anthropic", Type: "oauth", Status: "active", Schedulable: false, ProxyID: &in.ProxyID, Credentials: in.Credentials}
			a.Groups = append(a.Groups, struct {
				ID int64 `json:"id"`
			}{2})
			remoteAccounts[id] = a
			if failCreate {
				w.WriteHeader(502)
				return
			}
			data = a
		case r.URL.Path == "/api/v1/admin/accounts/bulk-update":
			var in struct {
				IDs    []int64 `json:"account_ids"`
				Groups []int64 `json:"group_ids"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			a := remoteAccounts[in.IDs[0]]
			a.Schedulable = false
			a.Groups = nil
			for _, g := range in.Groups {
				a.Groups = append(a.Groups, struct {
					ID int64 `json:"id"`
				}{g})
			}
			remoteAccounts[a.ID] = a
			data = map[string]int{"success": 1, "failed": 0}
		case strings.HasPrefix(r.URL.Path, "/api/v1/admin/accounts/"):
			var id int64
			fmt.Sscanf(r.URL.Path, "/api/v1/admin/accounts/%d", &id)
			a, ok := remoteAccounts[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/schedulable") {
				var in struct{ Schedulable bool }
				json.NewDecoder(r.Body).Decode(&in)
				a.Schedulable = in.Schedulable
				remoteAccounts[id] = a
				if in.Schedulable && failEnable {
					w.WriteHeader(502)
					return
				}
				data = map[string]bool{"ok": true}
			} else {
				data = struct {
					ports.RemoteAccount
					Concurrency int `json:"concurrency"`
					Priority    int `json:"priority"`
				}{a, 120, 0}
			}
		default:
			t.Error("unexpected fixture path", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer remote.Close()
	accountUp, e := sub2api.NewAccountClient(remote.URL, "fixture-key")
	if e != nil {
		t.Fatal(e)
	}
	upstream := testutil.NewUpstream()
	local := &auth.Service{Store: store, Local: store, Upstream: upstream, Vault: vault, InstanceID: instance, KeyID: "fixture"}
	admin := &auth.Service{Store: store, Upstream: upstream, Vault: vault, InstanceID: instance, KeyID: "fixture", Administrator: true}
	business := &accounts.Service{Store: store, Upstream: accountUp, Vault: vault, KeyID: "fixture", IdentityKey: bytes.Repeat([]byte{9}, 32), IdentityKeyID: "identity-fixture", QuarantineID: 1, ApprovedGroups: []int64{2}, ProxyCapacity: 1, GroupPolicies: store}
	if _, e = store.SetGroupAccess(ctx, ports.GroupAccess{GroupID: 2, Enabled: true}, 42); e != nil {
		t.Fatal(e)
	}
	h, e := New(local, Config{Origin: origin, Suppliers: management, Admin: admin, Accounts: business})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body any, cookie *http.Cookie, csrf, idempotency string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Origin", origin)
		r.Header.Set("User-Agent", "fixture-agent")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if idempotency != "" {
			r.Header.Set("Idempotency-Key", idempotency)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	want := func(w *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
		t.Helper()
		if w.Code != status {
			t.Fatalf("want %d got %d %s", status, w.Code, w.Body.String())
		}
		var body struct{ Data map[string]json.RawMessage }
		if json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal("decode")
		}
		return body.Data
	}
	login := func(name string) (*http.Cookie, string) {
		t.Helper()
		w := call("POST", "/api/supplier/v1/auth/login", map[string]string{"login_account": name, "password": "FixturePassword!2026"}, nil, "", "")
		want(w, 200)
		var cookie *http.Cookie
		for _, candidate := range w.Result().Cookies() {
			if candidate.Name == "__Host-supplier_session" {
				cookie = candidate
			}
		}
		if cookie == nil {
			t.Fatal("missing supplier cookie")
		}
		w = call("GET", "/api/supplier/v1/me", nil, cookie, "", "")
		d := want(w, 200)
		var csrf string
		json.Unmarshal(d["csrf_token"], &csrf)
		return cookie, csrf
	}
	c1, csrf1 := login("vendor_one")
	c2, csrf2 := login("vendor_two")
	want(call("POST", "/api/supplier/v1/oauth/sessions", map[string]any{}, nil, "", key()), 401)
	want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "No CSRF", Address: "8.8.8.8:8080"}, c1, "", key()), 403)
	want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Metadata", Address: "169.254.169.254:80"}, c1, csrf1, key()), 400)
	pkey := key()
	input := ports.ProxyInput{Name: "Owned fixture", Address: "http://user:proxy-secret@8.8.8.8:8080", Region: "SG"}
	w := call("POST", "/api/supplier/v1/proxies", input, c1, csrf1, pkey)
	data := want(w, 201)
	var proxyID string
	json.Unmarshal(data["id"], &proxyID)
	if strings.Contains(w.Body.String(), "proxy-secret") || strings.Contains(w.Body.String(), "username") {
		t.Fatal("proxy credential leaked")
	}
	want(call("POST", "/api/supplier/v1/proxies", input, c1, csrf1, pkey), 201)
	if len(proxies) != 1 {
		t.Fatal("proxy created twice")
	}
	want(call("POST", "/api/supplier/v1/proxies/"+proxyID+"/test", nil, c2, csrf2, ""), 404)
	want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{Name: "Foreign", ProxyID: proxyID, GroupID: 2}, c2, csrf2, key()), 409)
	if generateCalls != 0 {
		t.Fatal("foreign proxy used")
	}
	// Two requests contend for the last slot. Real PG supplier/proxy locks permit only one.
	record, e := store.LocalSupplier(ctx, first.ID)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan ports.BoundOAuth, 2)
	errorsC := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := key()
			o, _, e := store.ReserveOnboarding(ctx, record, ports.BoundOAuth{OAuthSession: ports.OAuthSession{ID: id, Name: "Capacity", KeyID: "fixture", StateHash: auth.Hash("draft"), ExpiresAt: time.Now().Add(time.Minute)}, Proxy: ports.OwnProxy{ID: proxyID}, GroupID: 2, BeginKey: key(), RequestHash: auth.Hash(id)})
			if e == nil {
				results <- o
			} else {
				errorsC <- e
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errorsC)
	if len(results) != 1 || len(errorsC) != 1 {
		t.Fatal("capacity overbook", len(results), len(errorsC))
	}
	held := <-results
	if e = store.CancelOnboarding(ctx, first.ID, held.ID); e != nil {
		t.Fatal(e)
	}
	// Closing admission changes options, not existing resources or user pricing.
	policy, e := store.GroupAccess(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	policy.Enabled = false
	policy, e = store.SetGroupAccess(ctx, policy, 42)
	if e != nil {
		t.Fatal(e)
	}
	groups, e := business.Groups(ctx)
	if e != nil || len(groups) != 0 {
		t.Fatal(groups, e)
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{ProxyID: proxyID, GroupID: 2}, c1, csrf1, key()), 403)
	policy.Enabled = true
	if _, e = store.SetGroupAccess(ctx, policy, 42); e != nil {
		t.Fatal(e)
	}
	// Begin is idempotent, settings freeze, and invalid state never exchanges.
	beginKey := key()
	beginInput := ports.BeginOnboarding{Name: "Real fixture account", ProxyID: proxyID, GroupID: 2}
	var custom ports.AccountSettings
	json.Unmarshal([]byte(`{"concurrency":120,"max_sessions":150,"session_idle_timeout_minutes":8,"base_rpm":23,"window_cost_limit":12.34,"window_cost_sticky_reserve":0}`), &custom)
	beginInput.Settings = &custom
	data = want(call("POST", "/api/supplier/v1/oauth/sessions", beginInput, c1, csrf1, beginKey), 201)
	var oauthID string
	json.Unmarshal(data["id"], &oauthID)
	settingsBound, e := store.Onboarding(ctx, first.ID, oauthID)
	if e != nil || settingsBound.Settings.Concurrency != 120 || *settingsBound.Settings.MaxSessions != 150 || *settingsBound.Settings.BaseRPM != 23 {
		t.Fatal("settings did not survive database round-trip", settingsBound.Settings, e)
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions", beginInput, c1, csrf1, beginKey), 201)
	changed := beginInput
	other := custom
	other.Concurrency = 121
	changed.Settings = &other
	want(call("POST", "/api/supplier/v1/oauth/sessions", changed, c1, csrf1, beginKey), 409)
	invalid := ports.BeginOnboarding{ProxyID: proxyID, GroupID: 2, Settings: &ports.AccountSettings{Concurrency: -1}}
	want(call("POST", "/api/supplier/v1/oauth/sessions", invalid, c1, csrf1, key()), 400)
	if generateCalls != 1 {
		t.Fatal("authorization generated twice")
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+oauthID+"/complete", map[string]string{"authorization_code": "code#wrong"}, c1, csrf1, key()), 400)
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+oauthID+"/complete", map[string]string{"authorization_code": "code#fixture-state"}, c2, csrf2, key()), 404)
	completeKey := key()
	completeBody := map[string]string{"authorization_code": "code#fixture-state"}
	w = call("POST", "/api/supplier/v1/oauth/sessions/"+oauthID+"/complete", completeBody, c1, csrf1, completeKey)
	data = want(w, 202)
	var state, opID string
	json.Unmarshal(data["state"], &state)
	json.Unmarshal(data["id"], &opID)
	if state != "succeeded" {
		t.Fatalf("failed local onboarding %s", w.Body.String())
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+oauthID+"/complete", completeBody, c1, csrf1, completeKey), 202)
	if exchangeCalls != 1 || createCalls != 1 {
		t.Fatal("non-idempotent exchange/create")
	}
	want(call("GET", "/api/supplier/v1/operations/"+opID, nil, c2, "", ""), 404)
	items, total, e := store.Accounts(ctx, first.ID, 1, 20)
	if e != nil || total != 1 || items[0].Admission != "approved" || items[0].Desired != "running" {
		t.Fatal("incorrect account outcome", items, total, e)
	}
	if !remoteAccounts[items[0].UpstreamID].Schedulable {
		t.Fatal("validated account was not enabled")
	}
	// The same authenticated list optionally projects safe configuration and
	// cached consumption. No metrics query preserves the original contract.
	w = call("GET", "/api/supplier/v1/accounts", nil, c1, "", "")
	want(w, 200)
	if strings.Contains(w.Body.String(), `"metrics"`) {
		t.Fatal("legacy list contract changed")
	}
	w = call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, c1, "", "")
	data = want(w, 200)
	var projected []ports.Account
	if e = json.Unmarshal(data["items"], &projected); e != nil || len(projected) != 1 {
		t.Fatal(e, w.Body.String())
	}
	if projected[0].Metrics == nil || projected[0].Metrics.Configuration == nil || projected[0].Metrics.Configuration.ConcurrencyLimit != 120 || projected[0].Metrics.Consumption == nil || projected[0].Metrics.Consumption.TotalCost != nil {
		t.Fatal("configuration or unobserved consumption projection incorrect", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "credentials") || strings.Contains(w.Body.String(), "upstream_account_id") {
		t.Fatal("projection leaked source fields")
	}
	want(call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, nil, "", ""), 401)
	w = call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, c2, "", "")
	data = want(w, 200)
	if string(data["total"]) != "0" || strings.Contains(w.Body.String(), items[0].ID) {
		t.Fatal("metrics crossed supplier scope")
	}
	// Unknown external create must keep its reservation/account fence and never replay.
	// A main-site administrator repairs an account outside the supplier workflow.
	w = call("GET", "/api/supplier/v1/accounts/overview?account_id=999&supplier_id="+second.ID, nil, c1, "", "")
	want(w, 200)
	var overviewBody struct{ Data ports.PoolOverview }
	if e = json.Unmarshal(w.Body.Bytes(), &overviewBody); e != nil {
		t.Fatal(e)
	}
	if overviewBody.Data.RPM.Total != 1 || overviewBody.Data.RPM.Known != 1 || *overviewBody.Data.RPM.Value != "7" || *overviewBody.Data.TPM.Value != "13" {
		t.Fatal("overview not pinned to current supplier", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "credentials") || strings.Contains(w.Body.String(), "upstream_account_id") {
		t.Fatal("overview leaked fields")
	}
	w = call("GET", "/api/supplier/v1/accounts/overview", nil, c2, "", "")
	want(w, 200)
	json.Unmarshal(w.Body.Bytes(), &overviewBody)
	if overviewBody.Data.RPM.Total != 0 || *overviewBody.Data.RPM.Value != "0" {
		t.Fatal("foreign accounts included", w.Body.String())
	}
	want(call("GET", "/api/supplier/v1/accounts/overview", nil, nil, "", ""), 401)
	want(call("GET", "/api/supplier/admin/v1/suppliers/"+first.ID+"/accounts/overview", nil, c1, "", ""), 401)
	if _, e = db.Exec(`UPDATE supplier.supplier_accounts SET admission_state='needs_attention',desired_state='paused',version=version+1 WHERE id=$1`, items[0].ID); e != nil {
		t.Fatal(e)
	}
	w = call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, c1, "", "")
	data = want(w, 200)
	json.Unmarshal(data["items"], &projected)
	if len(projected) != 1 || projected[0].Admission != "approved" || projected[0].Desired != "running" || projected[0].StateSync != "synced" {
		t.Fatal("main repair not mirrored", w.Body.String())
	}
	mu.Lock()
	remoteRow := remoteAccounts[items[0].UpstreamID]
	remoteRow.Schedulable = false
	remoteAccounts[remoteRow.ID] = remoteRow
	mu.Unlock()
	w = call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, c1, "", "")
	data = want(w, 200)
	json.Unmarshal(data["items"], &projected)
	if projected[0].Admission != "approved" || projected[0].Desired != "paused" {
		t.Fatal("main pause not mirrored", w.Body.String())
	}
	persisted, e := store.Account(ctx, first.ID, items[0].ID)
	if e != nil || persisted.Desired != "paused" {
		t.Fatal("state response not persisted", persisted, e)
	}
	mu.Lock()
	delete(remoteAccounts, remoteRow.ID)
	mu.Unlock()
	w = call("GET", "/api/supplier/v1/accounts?include_metrics=1", nil, c1, "", "")
	data = want(w, 200)
	json.Unmarshal(data["items"], &projected)
	if projected[0].StateSync != "failed" || projected[0].Admission != "approved" || projected[0].Desired != "paused" {
		t.Fatal("read failure hid stale state", w.Body.String())
	}
	mu.Lock()
	remoteAccounts[remoteRow.ID] = remoteRow
	mu.Unlock()
	w = call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Second proxy", Address: "8.8.4.4:8080", Region: "SG"}, c1, csrf1, key())
	data = want(w, 201)
	var secondProxy string
	json.Unmarshal(data["id"], &secondProxy)
	zero := 0
	data = want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{Name: "Unknown", ProxyID: secondProxy, GroupID: 2, Settings: &ports.AccountSettings{Concurrency: 7, MaxSessions: &zero, BaseRPM: &zero}}, c1, csrf1, key()), 201)
	var unknownID string
	json.Unmarshal(data["id"], &unknownID)
	failCreate = true
	unknownKey := key()
	body := map[string]string{"authorization_code": "unknown#fixture-state"}
	data = want(call("POST", "/api/supplier/v1/oauth/sessions/"+unknownID+"/complete", body, c1, csrf1, unknownKey), 202)
	json.Unmarshal(data["state"], &state)
	if state != "needs_attention" {
		t.Fatal("unknown external outcome lost")
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+unknownID+"/complete", body, c1, csrf1, unknownKey), 202)
	if createCalls != 2 {
		t.Fatal("unknown creation replayed")
	}
	p, e := store.Proxy(ctx, first.ID, secondProxy)
	if e != nil || p.Accounts+p.Held != 1 {
		t.Fatal("unknown capacity released", p, e)
	}
	var audit string
	if e = db.QueryRow(`SELECT string_agg(COALESCE(actor_kind,'')||':'||COALESCE(actor_id,'')||':'||safe_summary::text,';') FROM supplier.supplier_audit_events`).Scan(&audit); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(audit, "supplier:"+first.ID) || strings.Contains(audit, "secret") || strings.Contains(audit, "fixture-state") {
		t.Fatal("actor provenance or secret audit")
	}
	_, otherTotal, e := store.Accounts(ctx, second.ID, 1, 20)
	if e != nil || otherTotal != 0 {
		t.Fatal("cross supplier leak")
	}

	// Expiry/cancel/credential reset do not consume a code, and release only unconsumed holds.
	failCreate = false
	data = want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Expiry fixture", Address: "1.1.1.1:8080", Region: "SG"}, c1, csrf1, key()), 201)
	var thirdProxy string
	json.Unmarshal(data["id"], &thirdProxy)
	beginThird := func() string {
		t.Helper()
		data := want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{Name: "Not created", ProxyID: thirdProxy, GroupID: 2}, c1, csrf1, key()), 201)
		var id string
		json.Unmarshal(data["id"], &id)
		return id
	}
	cancelled := beginThird()
	want(call("DELETE", "/api/supplier/v1/oauth/sessions/"+cancelled, nil, c2, csrf2, ""), 409)
	want(call("DELETE", "/api/supplier/v1/oauth/sessions/"+cancelled, nil, c1, csrf1, ""), 200)
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+cancelled+"/complete", completeBody, c1, csrf1, key()), 409)
	expired := beginThird()
	if _, e = db.Exec(`UPDATE supplier.supplier_oauth_sessions SET expires_at=now()-interval '1 minute' WHERE id=$1`, expired); e != nil {
		t.Fatal(e)
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+expired+"/complete", completeBody, c1, csrf1, key()), 409)
	if exchangeCalls != 2 {
		t.Fatal("expired/cancelled code exchanged")
	}
	resetBound := beginThird()
	want(call("DELETE", "/api/supplier/v1/proxies/"+thirdProxy, nil, c1, csrf1, ""), 409)
	want(call("DELETE", "/api/supplier/v1/proxies/"+proxyID, nil, c1, csrf1, ""), 409) // Bound account.
	// Failed creation must compensate both services; the same key cannot recreate it.
	failProxyTest = true
	failedKey := key()
	failedInput := ports.ProxyInput{Name: "Failed fixture", Address: "9.9.9.9:8080", Region: "SG"}
	want(call("POST", "/api/supplier/v1/proxies", failedInput, c1, csrf1, failedKey), 422)
	want(call("POST", "/api/supplier/v1/proxies", failedInput, c1, csrf1, failedKey), 422)
	var visible int
	if e = db.QueryRow(`SELECT count(*) FROM supplier.supplier_proxies WHERE name='Failed fixture' AND deleted_at IS NULL`).Scan(&visible); e != nil || visible != 0 {
		t.Fatal("failed proxy still visible", e)
	}
	failProxyTest = false
	data = want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Delete fixture", Address: "1.0.0.1:8080", Region: "SG"}, c1, csrf1, key()), 201)
	var deleteID string
	json.Unmarshal(data["id"], &deleteID)
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, nil, "", ""), 401)
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, c1, "", ""), 403)
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, c2, csrf2, ""), 404)
	failDelete = true
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, c1, csrf1, ""), 503)
	want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{Name: "Deleting", ProxyID: deleteID, GroupID: 2}, c1, csrf1, key()), 409)
	failDelete = false
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, c1, csrf1, ""), 200) // Main already absent; retry finishes local deletion.
	want(call("DELETE", "/api/supplier/v1/proxies/"+deleteID, nil, c1, csrf1, ""), 404)
	if _, e = store.Proxy(ctx, first.ID, deleteID); e == nil {
		t.Fatal("deleted proxy still listed")
	}
	bound, e := store.Onboarding(ctx, first.ID, resetBound)
	if e != nil {
		t.Fatal(e)
	}
	if bound.Settings.Concurrency != 20 || *bound.Settings.MaxSessions != 100 || *bound.Settings.BaseRPM != 100 {
		t.Fatal("omitted settings did not freeze the new defaults", bound.Settings)
	}
	// Existing explicit snapshots retain their original defaults after upgrade.
	if _, e = db.Exec(`UPDATE supplier.supplier_oauth_sessions SET account_settings='{"concurrency":100,"max_sessions":100,"session_idle_timeout_minutes":5,"base_rpm":15,"rpm_strategy":"tiered"}'::jsonb WHERE id=$1`, resetBound); e != nil {
		t.Fatal(e)
	}
	previous, e := store.Onboarding(ctx, first.ID, resetBound)
	if e != nil || previous.Settings.Concurrency != 100 || *previous.Settings.MaxSessions != 100 || *previous.Settings.BaseRPM != 15 {
		t.Fatal("existing authorization snapshot changed", previous.Settings, e)
	}
	// An authorization created before migration 013 has an empty settings
	// snapshot. Its previous limits must not change after upgrading.
	if _, e = db.Exec(`UPDATE supplier.supplier_oauth_sessions SET account_settings='{}'::jsonb WHERE id=$1`, resetBound); e != nil {
		t.Fatal(e)
	}
	legacy, e := store.Onboarding(ctx, first.ID, resetBound)
	if e != nil || legacy.Settings.Concurrency != 100 || *legacy.Settings.MaxSessions != 100 || *legacy.Settings.BaseRPM != 0 {
		t.Fatal("legacy authorization defaults changed", legacy.Settings, e)
	}
	if _, e = db.Exec(`UPDATE supplier.supplier_oauth_sessions SET credential_version=NULL WHERE id=$1`, resetBound); e == nil {
		t.Fatal("NULL actor generation bypassed constraint")
	}
	if _, _, e = management.Mutate(ctx, 42, "supplier.reset", first.ID, key(), "", "", "ResetPassword!2026", "", first.Version); e != nil {
		t.Fatal(e)
	}
	if e = store.CheckOnboarding(ctx, bound); e == nil {
		t.Fatal("old OAuth survived credential reset")
	}
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+resetBound+"/complete", completeBody, c1, csrf1, key()), 401)
	if exchangeCalls != 2 {
		t.Fatal("reset authorization consumed")
	}
	// An ambiguous enable response must compensate back to paused; no new OAuth/create on retry.
	failEnable = true
	data = want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Enable failure", Address: "4.2.2.2:8080", Region: "US"}, c2, csrf2, key()), 201)
	var failProxy string
	json.Unmarshal(data["id"], &failProxy)
	data = want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{Name: "Enable failure", ProxyID: failProxy, GroupID: 2}, c2, csrf2, key()), 201)
	var failOAuth string
	json.Unmarshal(data["id"], &failOAuth)
	failKey := key()
	data = want(call("POST", "/api/supplier/v1/oauth/sessions/"+failOAuth+"/complete", map[string]string{"authorization_code": "enablefail#fixture-state"}, c2, csrf2, failKey), 202)
	json.Unmarshal(data["state"], &state)
	if state != "needs_attention" {
		t.Fatal("ambiguous activation not recorded", state)
	}
	rows, n, e := store.Accounts(ctx, second.ID, 1, 20)
	if e != nil || n != 1 || rows[0].Desired != "paused" || remoteAccounts[rows[0].UpstreamID].Schedulable {
		t.Fatal("failed activation still schedulable", rows, n, e)
	}
	beforeCreates := createCalls
	want(call("POST", "/api/supplier/v1/oauth/sessions/"+failOAuth+"/complete", map[string]string{"authorization_code": "enablefail#fixture-state"}, c2, csrf2, failKey), 202)
	if createCalls != beforeCreates {
		t.Fatal("activation failure replayed creation")
	}
	// A stale enabled mapping must not break options or hide the supplier's proxies.
	if _, e = store.SetGroupAccess(ctx, ports.GroupAccess{GroupID: 6, Enabled: true, SupplierGroupName: "Deleted fixture"}, 42); e != nil {
		t.Fatal(e)
	}
	data = want(call("GET", "/api/supplier/v1/onboarding/options", nil, c2, "", ""), 200)
	var optionGroups []ports.AccountGroup
	var optionProxies []ports.OwnProxy
	if e = json.Unmarshal(data["groups"], &optionGroups); e != nil || len(optionGroups) != 1 || optionGroups[0].ID != 2 {
		t.Fatal("stale mapping broke options", string(data["groups"]), e)
	}
	if e = json.Unmarshal(data["proxies"], &optionProxies); e != nil || len(optionProxies) == 0 {
		t.Fatal("options lost owned proxies", e)
	}
	// Deletion after issuing an authorization must block exchange/create/enable.
	failEnable = false
	data = want(call("POST", "/api/supplier/v1/proxies", ports.ProxyInput{Name: "Group deletion fixture", Address: "8.8.4.4:8080", Region: "SG"}, c2, csrf2, key()), 201)
	var groupProxy, groupOAuth string
	json.Unmarshal(data["id"], &groupProxy)
	data = want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{ProxyID: groupProxy, GroupID: 2}, c2, csrf2, key()), 201)
	json.Unmarshal(data["id"], &groupOAuth)
	mu.Lock()
	groupDeleted = true
	mu.Unlock()
	data = want(call("GET", "/api/supplier/v1/onboarding/options", nil, c2, "", ""), 200)
	if string(data["groups"]) != "[]" {
		t.Fatal("all deleted groups must return an empty array", string(data["groups"]))
	}
	beforeExchange, beforeCreate, beforeGenerate := exchangeCalls, createCalls, generateCalls
	want(call("POST", "/api/supplier/v1/oauth/sessions", ports.BeginOnboarding{ProxyID: groupProxy, GroupID: 2}, c2, csrf2, key()), 403)
	data = want(call("POST", "/api/supplier/v1/oauth/sessions/"+groupOAuth+"/complete", completeBody, c2, csrf2, key()), 202)
	if string(data["state"]) != `"failed"` || string(data["step"]) != `"group_rejected"` {
		t.Fatal("deleted group authorization not rejected", string(data["state"]), string(data["step"]))
	}
	if exchangeCalls != beforeExchange || createCalls != beforeCreate || generateCalls != beforeGenerate {
		t.Fatal("deleted group allowed an external OAuth/account operation")
	}
	if policy, e := store.GroupAccess(ctx, 6); e != nil || !policy.Enabled {
		t.Fatal("read mutated the historical mapping", policy, e)
	}
	t.Log("R2 onboarding: real PG + local identity + synthetic upstream: proxy/CSRF/ownership/capacity/idempotency/state/atomic paused creation then automatic enabling/uncertain outcome/deleted group options and authorization fencing verified; no live Claude traffic")
}
