package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type policyFixture struct {
	ports.AccountStore
	value  ports.GroupAccess
	writes int
}

func (p *policyFixture) GroupAccess(_ context.Context, id int64) (ports.GroupAccess, error) {
	v := p.value
	v.GroupID = id
	return v, nil
}
func (p *policyFixture) EnabledGroupIDs(context.Context) ([]int64, error) { return nil, nil }
func (p *policyFixture) SetGroupAccess(_ context.Context, x ports.GroupAccess, actor int64) (ports.GroupAccess, error) {
	if x.Version != p.value.Version {
		return x, ports.ErrConflict
	}
	if actor != 42 {
		panic("incorrect audit actor")
	}
	x.Version++
	p.value = x
	p.writes++
	return x, nil
}

func TestGroupAccessAdminBoundaries(t *testing.T) {
	role, platform, status := "admin", "anthropic", "active"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user/profile":
			fmt.Fprintf(w, `{"code":0,"data":{"id":42,"email":"fixture@example.invalid","role":%q,"status":"active"}}`, role)
		case "/api/v1/admin/compliance":
			fmt.Fprint(w, `{"code":0,"data":{"required":false}}`)
		case "/api/v1/admin/groups/2":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": ports.AccountGroup{ID: 2, Platform: platform, Status: status}})
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	client, _ := sub2api.New(up.URL, nil)
	remote, _ := sub2api.NewAccountClient(up.URL, "fixture-machine")
	store := &policyFixture{}
	h, e := New(&auth.Service{Local: repository.New(nil, "", "")}, Config{Origin: origin, Suppliers: &suppliers.Service{}, Admin: &auth.Service{Administrator: true, Upstream: client}, Accounts: &accounts.Service{Store: store, Upstream: remote}})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, token, from, version, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, "/api/supplier/admin/v1/groups/2/access", strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", token)
		}
		r.Header.Set("Origin", from)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", version)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s got %d want %d: %s", method, w.Code, want, w.Body.String())
		}
	}
	call("GET", "", origin, "", "", 401)
	call("GET", "Bearer fixture", origin, "", "", 200)
	role = "user"
	call("PUT", "Bearer fixture", origin, "0", `{"enabled":true}`, 403)
	role = "admin"
	call("PUT", "Bearer fixture", "https://evil.invalid", "0", `{"enabled":true}`, 403)
	call("PUT", "Bearer fixture", origin, "", `{"enabled":true}`, 400)
	platform = "openai"
	call("PUT", "Bearer fixture", origin, "0", `{"enabled":true}`, 400)
	platform = "anthropic"
	status = "inactive"
	call("PUT", "Bearer fixture", origin, "0", `{"enabled":true}`, 400)
	status = "active"
	call("PUT", "Bearer fixture", origin, "0", `{"enabled":true}`, 200)
	call("PUT", "Bearer fixture", origin, "0", `{"enabled":false}`, 409)
	status = "inactive"
	call("PUT", "Bearer fixture", origin, "1", `{"enabled":false}`, 200)
	if store.writes != 2 || store.value.Enabled {
		t.Fatal("incorrect writes", store)
	}
}
