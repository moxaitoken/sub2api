package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
)

func TestMainAdminBearerTransactionProbe(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-main-admin" || r.Header.Get("x-api-key") != "" {
			t.Error("caller identity not preserved")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user/profile":
			fmt.Fprint(w, `{"code":0,"data":{"id":42,"email":"fixture@example.invalid","role":"admin","status":"active"}}`)
		case "/api/v1/admin/compliance":
			fmt.Fprint(w, `{"code":0,"data":{"required":false}}`)
		default:
			t.Error("unexpected upstream call", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	client, err := sub2api.New(up.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := repository.New(nil, "", "")
	h, err := New(&auth.Service{Local: store}, Config{Origin: origin, Suppliers: &suppliers.Service{}, Admin: &auth.Service{Administrator: true, Upstream: client}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/supplier/admin/v1/me", nil)
	r.Header.Set("Authorization", "Bearer synthetic-main-admin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	t.Logf("main_admin_bearer_status=%d body=%s", w.Code, w.Body.String())
	if w.Code != 200 {
		t.Fatalf("main admin must enter without a second login: %d", w.Code)
	}
	if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "synthetic-main-admin") {
		t.Fatal("bearer login must not create a session or disclose token")
	}
}

func TestMainAdminAuthorizationBoundaries(t *testing.T) {
	role, status, required, failure := "admin", "active", false, false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			t.Error("machine key must not authorize caller")
		}
		if failure {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-main-admin" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/user/profile" {
			fmt.Fprintf(w, `{"code":0,"data":{"id":42,"email":"fixture@example.invalid","role":%q,"status":%q}}`, role, status)
		} else if r.URL.Path == "/api/v1/admin/compliance" {
			fmt.Fprintf(w, `{"code":0,"data":{"required":%t}}`, required)
		} else {
			t.Error("unexpected upstream route")
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	client, _ := sub2api.New(up.URL, nil)
	store := repository.New(nil, "", "")
	h, err := New(&auth.Service{Local: store}, Config{Origin: origin, AdminOrigin: "https://main.example.invalid", Suppliers: &suppliers.Service{}, Admin: &auth.Service{Administrator: true, Upstream: client}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, from string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		if token != "" {
			r.Header.Set("Authorization", token)
		}
		r.Header.Set("Origin", from)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
	}
	me := "/api/supplier/admin/v1/me"
	call("GET", me, "", "", 401)
	call("GET", me, "Bearer synthetic-main-admin", "", 200)
	role = "user"
	call("GET", me, "Bearer synthetic-main-admin", "", 403)
	role = "admin"
	status = "disabled"
	call("GET", me, "Bearer synthetic-main-admin", "", 403)
	status = "active"
	required = true
	call("GET", me, "Bearer synthetic-main-admin", "", 423)
	required = false
	call("GET", me, "Bearer expired", "", 401)
	failure = true
	call("GET", me, "Bearer synthetic-main-admin", "", 503)
	failure = false
	for _, bad := range []string{"Basic synthetic-main-admin", "Bearer ", "Bearer a b"} {
		call("GET", me, bad, "", 401)
	}
	// Authenticated write reaches the existing input validator (no key => 400),
	// while a foreign origin or missing Bearer cannot reach any mutation.
	call("POST", "/api/supplier/admin/v1/suppliers", "Bearer synthetic-main-admin", "https://main.example.invalid", 400)
	call("POST", "/api/supplier/admin/v1/suppliers", "Bearer synthetic-main-admin", "https://evil.example.invalid", 403)
	call("POST", "/api/supplier/admin/v1/suppliers", "", "https://main.example.invalid", 403)
	call("POST", "/api/supplier/v1/auth/logout", "Bearer synthetic-main-admin", "https://main.example.invalid", 403)
	call("GET", "/api/supplier/v1/me", "Bearer synthetic-main-admin", "", 401)
}
