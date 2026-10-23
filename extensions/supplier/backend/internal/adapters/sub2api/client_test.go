package sub2api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func reply(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
		t.Error(err)
	}
}

func TestFixedOriginAndBoundedClient(t *testing.T) {
	invalid := []string{
		"", "localhost:8080", "ftp://example.invalid", "http://", "http://user:password@example.invalid",
		"http://example.invalid/api", "http://example.invalid/?key=value", "http://example.invalid?",
		"http://example.invalid/#fragment", "http://example.invalid#", "http://example.invalid:0",
		"http://example.invalid:65536", "http://example.invalid:", "http://example.invalid\n",
	}
	for _, origin := range invalid {
		t.Run(origin, func(t *testing.T) {
			_, err := New(origin, nil)
			if err == nil || strings.Contains(err.Error(), "password") {
				t.Fatal("invalid origin was accepted or exposed")
			}
		})
	}
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	supplied := &http.Client{Timeout: time.Minute, Jar: jar, Transport: transport}
	c, err := New("http://127.0.0.1:8080/", supplied)
	if err != nil {
		t.Fatal(err)
	}
	if c.http == supplied || c.http.Timeout != 10*time.Second || c.http.Jar != nil || c.http.Transport == transport {
		t.Fatal("client isolation and bounds not applied")
	}
	if c.http.Transport.(*http.Transport).Proxy != nil || supplied.Jar != jar || supplied.Timeout != time.Minute || transport.Proxy == nil {
		t.Fatal("proxy inherited or caller client changed")
	}
	c, err = New("https://example.invalid", &http.Client{Timeout: time.Second})
	if err != nil || c.http.Timeout != time.Second {
		t.Fatal("shorter timeout must be preserved")
	}
	if c.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("default transport must not read proxy environment")
	}
}

func TestHTTPAuthContracts(t *testing.T) {
	var calls []string
	info := ports.ClientInfo{IP: "203.0.113.42", UserAgent: "supplier-test-browser", Language: "zh-CN"}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("X-Real-IP") != info.IP || r.Header.Get("X-Forwarded-For") != info.IP ||
			r.Header.Get("User-Agent") != info.UserAgent || r.Header.Get("Accept-Language") != info.Language {
			t.Error("trusted client context was not forwarded")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-API-Key") != "" || r.URL.RawQuery != "" {
			t.Error("unexpected credential or query forwarding")
		}
		var input map[string]any
		if r.Method == http.MethodPost {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("JSON content type missing")
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
		}
		switch r.URL.Path {
		case "/api/v1/auth/login":
			if input["email"] != "member@example.invalid" || input["password"] != "fixture-password" ||
				input["turnstile_token"] != "fixture-turnstile" || input["tencent_captcha_ticket"] != "fixture-ticket" ||
				input["tencent_captcha_randstr"] != "fixture-randstr" {
				t.Error("login/captcha request contract changed")
			}
		case "/api/v1/auth/login/2fa":
			if input["temp_token"] != "fixture-challenge" || input["totp_code"] != "123456" || len(input) != 2 {
				t.Error("2FA request contract changed")
			}
		case "/api/v1/auth/refresh", "/api/v1/auth/logout":
			if input["refresh_token"] != "fixture-refresh" || len(input) != 1 {
				t.Error("refresh/logout request contract changed")
			}
		case "/api/v1/user/profile":
			if r.Header.Get("Authorization") != "Bearer fixture-access" {
				t.Error("bearer token was not sent")
			}
			reply(t, w, map[string]any{"id": int64(9007199254740993), "email": "member@example.invalid", "role": "user", "status": "active", "notes": "private-ignored", "balance": 500})
			return
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer authorization leaked into unauthenticated endpoint")
		}
		reply(t, w, map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "expires_in": 60, "token_type": "Bearer"})
	})
	ctx := context.Background()
	before := time.Now()
	login, err := client.Login(ctx, ports.LoginInput{Email: "member@example.invalid", Password: "fixture-password", TurnstileToken: "fixture-turnstile", TencentCaptchaTicket: "fixture-ticket", TencentCaptchaRandstr: "fixture-randstr"}, info)
	if err != nil || login.Challenge != "" || login.Tokens.AccessToken != "fixture-access" ||
		login.Tokens.ExpiresAt.Before(before.Add(59*time.Second)) || login.Tokens.ExpiresAt.After(time.Now().Add(60*time.Second)) {
		t.Fatalf("invalid login result: error=%v", err)
	}
	if _, err := client.Verify2FA(ctx, "fixture-challenge", "123456", info); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(ctx, "fixture-refresh", info); err != nil {
		t.Fatal(err)
	}
	if err := client.Logout(ctx, "fixture-refresh", info); err != nil {
		t.Fatal(err)
	}
	for _, fetch := range []func() (ports.Profile, error){
		func() (ports.Profile, error) { return client.Profile(ctx, "fixture-access", info) },
	} {
		profile, err := fetch()
		if err != nil || profile.ID != 9007199254740993 || profile.Status != "active" || profile.Role != "user" {
			t.Fatalf("numeric int64 profile decode failed: %v", err)
		}
		serialized, _ := json.Marshal(profile)
		if strings.Contains(string(serialized), "private-ignored") || strings.Contains(string(serialized), "balance") {
			t.Fatal("unneeded upstream fields escaped the adapter")
		}
	}
	if len(calls) != 5 {
		t.Fatalf("want five requests, got %d", len(calls))
	}
}

func TestTwoFactorChallengeAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		data any
		ok   bool
	}{
		{"challenge", map[string]any{"requires_2fa": true, "temp_token": "fixture-challenge"}, true},
		{"missing challenge", map[string]any{"requires_2fa": true}, false},
		{"tokens with challenge", map[string]any{"requires_2fa": true, "temp_token": "fixture-challenge", "access_token": "fixture-access"}, false},
		{"missing access", map[string]any{"expires_in": 60}, false},
		{"unknown expiry", map[string]any{"access_token": "fixture-access"}, false},
		{"zero expiry", map[string]any{"access_token": "fixture-access", "expires_in": 0}, false},
		{"negative expiry", map[string]any{"access_token": "fixture-access", "expires_in": -1}, false},
		{"overflow expiry", map[string]any{"access_token": "fixture-access", "expires_in": int64(9223372036854775807)}, false},
		{"string expiry", map[string]any{"access_token": "fixture-access", "expires_in": "60"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { reply(t, w, tc.data) })
			result, err := client.Login(context.Background(), ports.LoginInput{}, ports.ClientInfo{})
			if tc.ok {
				if err != nil || result.Challenge != "fixture-challenge" || result.Tokens.AccessToken != "" {
					t.Fatalf("challenge contract failed: %v", err)
				}
			} else if err == nil {
				t.Fatal("malformed auth response accepted")
			}
		})
	}
}

func TestJWTExpiryIsOnlyUntrustedBound(t *testing.T) {
	now := time.Unix(1900000000, 0)
	jwt := func(exp any) string {
		payload, _ := json.Marshal(map[string]any{"exp": exp, "role": "admin"})
		return "unverified." + base64.RawURLEncoding.EncodeToString(payload) + ".not-a-signature"
	}
	seconds := int64(60)
	for _, tc := range []struct {
		name    string
		data    authData
		want    time.Time
		wantErr bool
	}{
		{"fallback", authData{AccessToken: jwt(now.Unix() + 30)}, now.Add(30 * time.Second), false},
		{"shorter jwt", authData{AccessToken: jwt(now.Unix() + 30), ExpiresIn: &seconds}, now.Add(30 * time.Second), false},
		{"shorter response", authData{AccessToken: jwt(now.Unix() + 120), ExpiresIn: &seconds}, now.Add(60 * time.Second), false},
		{"past", authData{AccessToken: jwt(now.Unix() - 1), ExpiresIn: &seconds}, time.Time{}, true},
		{"unknown", authData{AccessToken: "unverified.invalid.signature"}, time.Time{}, true},
		{"wrong claim type", authData{AccessToken: jwt("1900000030")}, time.Time{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokenExpiry(tc.data, now)
			if tc.wantErr {
				if err == nil {
					t.Fatal("unknown or expired token accepted")
				}
			} else if err != nil || !got.Equal(tc.want) {
				t.Fatalf("wrong expiry bound: %v, %v", got, err)
			}
		})
	}
}

func TestErrorsAreSanitizedAndCategorized(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 423, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"fixture-secret-password-token", "reason":"fixture-secret-reason"}`))
			})
			_, err := client.Profile(context.Background(), "fixture-access", ports.ClientInfo{})
			var upstream *Error
			if !errors.As(err, &upstream) || upstream.Status != status || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("raw response leaked or status lost: %v", err)
			}
			want := ports.ErrUnavailable
			if status == 401 || status == 403 {
				want = ports.ErrForbidden
			} else if status == 404 {
				want = ports.ErrNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("status %d not categorized as %v", status, want)
			}
		})
	}
}

func TestRejectsMalformedOrOversizedEnvelopes(t *testing.T) {
	responses := []string{
		`not-json`, `{}`, `{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":{}}`,
		`{"code":200,"data":{"id":1}}`, `{"code":0,"data":{"id":"1","email":"a","role":"user","status":"active"}}`,
		`{"code":0,"data":{"id":1,"email":"a","role":"user","status":"active"}} {}`,
		`{"code":0,"data":{"id":1,"email":"a","role":"user","status":"active","notes":"` + strings.Repeat("x", maxResponseBytes) + `"}}`,
	}
	for index, body := range responses {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := client.Profile(context.Background(), "fixture-access", ports.ClientInfo{})
			var upstream *Error
			if !errors.As(err, &upstream) || upstream.Reason != "UPSTREAM_BAD_RESPONSE" {
				t.Fatalf("malformed response accepted: %v", err)
			}
		})
	}
}

func TestRedirectsCannotTransmitCredentials(t *testing.T) {
	var secondCalls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		reply(t, w, map[string]any{})
	}))
	defer other.Close()
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/capture", http.StatusTemporaryRedirect)
	})
	_, err := client.Login(context.Background(), ports.LoginInput{Password: "fixture-password"}, ports.ClientInfo{})
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Status != 307 || secondCalls.Load() != 0 {
		t.Fatal("redirect was followed or raw error exposed")
	}
}

func TestProfileValidationAndBlockedStatus(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Real-IP") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Accept-Language") != "" {
			t.Error("untrusted IP/headers forwarded")
		}
		reply(t, w, map[string]any{"id": 7, "email": "a@example.invalid", "role": "user", "status": "banned"})
	})
	ctx := context.Background()
	if _, err := client.Profile(ctx, "", ports.ClientInfo{}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("empty access token accepted")
	}
	if _, err := client.Profile(ctx, "fixture\r\nInjected: header", ports.ClientInfo{}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("header injection accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid inputs triggered a request")
	}
	profile, err := client.Profile(ctx, "fixture-access", ports.ClientInfo{IP: "127.0.0.1, 203.0.113.42", Language: "zh\r\nInjected: header"})
	if err != nil || profile.Status != "banned" {
		t.Fatal("upstream inactive status not preserved for service enforcement")
	}

}

func TestTransportCancellationDoesNotLeakDestination(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { reply(t, w, map[string]any{}) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Profile(ctx, "fixture-access", ports.ClientInfo{})
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Reason != "UPSTREAM_TRANSPORT_ERROR" || strings.Contains(err.Error(), client.baseURL) {
		t.Fatalf("transport error leaked destination or credentials: %v", err)
	}
}

func TestProvidedCookieJarIsNotForwarded(t *testing.T) {
	var cookieSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieSeen = r.Header.Get("Cookie") != ""
		reply(t, w, map[string]any{"id": 1, "email": "a@example.invalid", "role": "user", "status": "active"})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(u, []*http.Cookie{{Name: "upstream_session", Value: "fixture-private-cookie"}})
	client, err := New(server.URL, &http.Client{Jar: jar})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Profile(context.Background(), "fixture-access", ports.ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if cookieSeen {
		t.Fatal("caller cookie jar leaked to upstream")
	}
}
