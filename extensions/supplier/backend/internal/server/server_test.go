package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
)

const origin = "https://supplier.example.invalid"

func setup(t *testing.T) (http.Handler, *testutil.Store, *testutil.Upstream) {
	t.Helper()
	store := testutil.NewStore()
	up := testutil.NewUpstream()
	vault, err := auth.NewVault(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{Store: store, Upstream: up, Vault: vault, InstanceID: "30000000-0000-0000-0000-000000000001", KeyID: "test-v1"}
	h, err := New(svc, Config{LegacyTestMode: true, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return h, store, up
}
func request(h http.Handler, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
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
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func session(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	w := request(h, "POST", "/api/supplier/v1/auth/login", `{"email":"fixture@example.invalid","password":"synthetic-fixture"}`, nil, "")
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body.String())
	}
	var c *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "__Host-supplier_session" {
			c = cookie
		}
	}
	if c == nil {
		t.Fatal("session cookie missing")
	}
	w = request(h, "GET", "/api/supplier/v1/me", "", c, "")
	if w.Code != 200 {
		t.Fatalf("me %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data auth.Me `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return c, envelope.Data.CSRFToken
}

func TestLoginCookieAndResponseContainNoUpstreamTokens(t *testing.T) {
	h, _, _ := setup(t)
	c, csrf := session(t, h)
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
		t.Fatal("insecure cookie settings")
	}
	w := request(h, "GET", "/api/supplier/v1/me", "", c, "")
	for _, secret := range []string{"fixture-access-canary", "fixture-refresh-canary", "synthetic-fixture"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("credential in public response")
		}
	}
	if csrf == "" {
		t.Fatal("CSRF missing")
	}
}
func TestUnauthorizedAndScopeGuards(t *testing.T) {
	h, _, _ := setup(t)
	if w := request(h, "GET", "/api/supplier/v1/me", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	c, csrf := session(t, h)
	for _, path := range []string{"/api/supplier/v1/members", "/api/supplier/v1/ops/suppliers", "/api/supplier/v1/ops/suppliers/10000000-0000-0000-0000-000000000001", "/api/supplier/v1/ops/suppliers/10000000-0000-0000-0000-000000000001/members", "/api/supplier/v1/ops/suppliers/10000000-0000-0000-0000-000000000001/members/20000000-0000-0000-0000-000000000001"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if w := request(h, method, path, `{}`, c, csrf); w.Code != 404 {
				t.Fatalf("removed route %s %s: %d", method, path, w.Code)
			}
		}
	}
}
func TestWriteOriginCSRFAndStrictFields(t *testing.T) {
	h, _, _ := setup(t)
	c, _ := session(t, h)
	if w := request(h, "POST", "/api/supplier/v1/auth/logout", `{}`, c, ""); w.Code != 403 {
		t.Fatal("logout without CSRF accepted")
	}
	for _, body := range []string{`{"email":"fixture@example.invalid","password":"synthetic-fixture","group_ids":[1]}`, `{"email":"` + strings.Repeat("x", 66000) + `"}`} {
		if w := request(h, "POST", "/api/supplier/v1/auth/login", body, nil, ""); w.Code != 400 {
			t.Fatalf("invalid login accepted: %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/supplier/v1/auth/login", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin login accepted")
	}
}
func TestMeContainsOnlyCurrentSupplierIdentity(t *testing.T) {
	h, store, up := setup(t)
	up.ProfileValue.Role = "admin" // A bound upstream admin is an ordinary supplier here.
	c, _ := session(t, h)
	w := request(h, "GET", "/api/supplier/v1/me?supplier_id=another", "", c, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 3 || envelope.Data["user"] == nil || envelope.Data["supplier"] == nil || envelope.Data["csrf_token"] == nil {
		t.Fatalf("unexpected /me contract: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "another") || !strings.Contains(w.Body.String(), store.MembershipValue.Supplier.ID) {
		t.Fatal("client controlled supplier isolation")
	}
	for _, field := range []string{`"member"`, `"is_ops"`, `"capabilities"`} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatalf("removed field %s", field)
		}
	}
}
func TestUnboundUsersIncludingAdminCannotLogin(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		t.Run(role, func(t *testing.T) {
			h, store, up := setup(t)
			up.ProfileValue.Role = role
			store.MissingMember = true
			if w := request(h, "POST", "/api/supplier/v1/auth/login", `{"email":"fixture@example.invalid","password":"synthetic-fixture"}`, nil, ""); w.Code != 403 {
				t.Fatalf("unbound login: %d", w.Code)
			}
			if len(store.Sessions) != 0 {
				t.Fatal("unbound user received a session")
			}
		})
	}
}
func TestTwoFAChallengeDoesNotLeakTempToken(t *testing.T) {
	h, _, up := setup(t)
	up.Challenge = true
	w := request(h, "POST", "/api/supplier/v1/auth/login", `{"email":"fixture@example.invalid","password":"synthetic-fixture"}`, nil, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-2fa-canary") {
		t.Fatal("challenge leak")
	}
	var body struct {
		Data struct {
			ID string `json:"challenge_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	var c *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "__Host-supplier_challenge" {
			c = cookie
		}
	}
	if c == nil {
		t.Fatal("missing binding cookie")
	}
	w = request(h, "POST", "/api/supplier/v1/auth/login/2fa", `{"challenge_id":"`+body.Data.ID+`","totp_code":"123456"}`, c, "")
	if w.Code != 200 {
		t.Fatalf("2fa%d %s", w.Code, w.Body.String())
	}
}
func TestLogoutWithCSRFRevokesLocally(t *testing.T) {
	h, _, _ := setup(t)
	c, csrf := session(t, h)
	if w := request(h, "POST", "/api/supplier/v1/auth/logout", `{}`, c, csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/supplier/v1/me", "", c, ""); w.Code == 200 {
		t.Fatal("revoked session accepted")
	}
}
func TestInsecureFlagCannotWeakenPublicOrigin(t *testing.T) {
	if _, err := New(nil, Config{LegacyTestMode: true, Origin: origin, InsecureLocalhost: true}); err == nil {
		t.Fatal("public cookie security disabled")
	}
}
func TestSpoofedForwardHeadersIgnored(t *testing.T) {
	h, _, _ := setup(t)
	c, _ := session(t, h)
	r := httptest.NewRequest("GET", "/api/supplier/v1/me", nil)
	r.Header.Set("User-Agent", "fixture-agent")
	r.Header.Set("X-Real-IP", "169.254.169.254")
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("untrusted forwarded headers changed binding")
	}
}
func TestNotEnabledRoutesCannotBeCalled(t *testing.T) {
	h, _, _ := setup(t)
	for _, path := range []string{"/api/supplier/v1/usage/records", "/api/supplier/v1/proxies"} {
		if w := request(h, "POST", path, `{}`, nil, ""); w.Code != 404 {
			t.Fatalf("not-yet-verified route enabled: %s=%d", path, w.Code)
		}
	}
}

func TestAccountRoutesRequireIdentityAndExplicitConfiguration(t *testing.T) {
	h, _, _ := setup(t)
	if w := request(h, "GET", "/api/supplier/v1/accounts", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	c, csrf := session(t, h)
	if w := request(h, "GET", "/api/supplier/v1/accounts", "", c, ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", "/api/supplier/v1/oauth/sessions", `{"name":"fixture"}`, c, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", "/api/supplier/v1/oauth/sessions", `{"name":"fixture"}`, c, csrf); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/supplier/v1/review/accounts", "", c, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

// Keep the public API usable through an actual loopback HTTP listener, not only recorder tests.
func TestLoopbackHealth(t *testing.T) {
	h, _, _ := setup(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL+"/health", nil)
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !bytes.Contains(b, []byte(`"status":"ok"`)) {
		t.Fatalf("health%d %s", r.StatusCode, b)
	}
}
