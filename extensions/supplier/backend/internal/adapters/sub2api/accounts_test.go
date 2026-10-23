package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func TestAccountMachineKeyPathsAndCredentialBoundary(t *testing.T) {
	var creates int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "fixture-admin-key" || r.Header.Get("Authorization") != "" {
			t.Error("management credentials mixed with user auth")
		}
		w.Header().Set("Content-Type", "application/json")
		var data any
		switch r.URL.Path {
		case "/api/v1/admin/groups/1":
			data = map[string]any{"id": 1, "platform": "anthropic", "status": "inactive", "is_exclusive": true}
		case "/api/v1/admin/groups/1/api-keys":
			data = map[string]any{"items": []any{}, "total": 0}
		case "/api/v1/admin/accounts/generate-auth-url":
			data = map[string]any{"auth_url": "https://claude.com/cai/oauth/authorize?state=fixture", "session_id": "fixture-session"}
		case "/api/v1/admin/accounts":
			if r.Method == "GET" {
				data = map[string]any{"items": []any{}, "total": 0}
				break
			}
			creates++
			if r.Header.Get("Idempotency-Key") != "fixture-operation-key" {
				t.Error("create idempotency key missing")
			}
			var input map[string]any
			if e := json.NewDecoder(r.Body).Decode(&input); e != nil {
				t.Error(e)
			}
			if input["platform"] != "anthropic" || input["type"] != "oauth" {
				t.Error("non-Claude creation")
			}
			if input["group_ids"].([]any)[0] != float64(1) {
				t.Error("quarantine not explicit")
			}
			data = map[string]any{"id": 10, "platform": "anthropic", "type": "oauth", "status": "active", "schedulable": true, "groups": []any{map[string]int{"id": 1}}}
		default:
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	c, e := NewAccountClient(server.URL, "fixture-admin-key")
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = c.CheckQuarantine(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = c.GenerateClaudeOAuth(ctx); e != nil {
		t.Fatal(e)
	}
	if e = c.CheckClaudeIdentity(ctx, "fixture-identity"); e != nil {
		t.Fatal(e)
	}
	if _, e = c.CreateClaudeAccount(ctx, "fixture", "fixture-operation-key", ports.ClaudeTokens{Access: "local-access-canary", Refresh: "local-refresh-canary", AccountUUID: "identity", ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1); e != nil {
		t.Fatal(e)
	}
	if creates != 1 {
		t.Fatal(creates)
	}
}

func TestQuarantineAndIdentityUncertaintyFailClosed(t *testing.T) {
	for _, scenario := range []string{"active-group", "nonexclusive", "assigned-key", "existing-identity", "unknown-identity"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data any
				if strings.Contains(r.URL.Path, "api-keys") {
					total := 0
					if scenario == "assigned-key" {
						total = 1
					}
					data = map[string]any{"total": total, "items": []any{}}
				} else if strings.Contains(r.URL.Path, "groups/") {
					status := "inactive"
					if scenario == "active-group" {
						status = "active"
					}
					data = map[string]any{"platform": "anthropic", "status": status, "is_exclusive": scenario != "nonexclusive"}
				} else {
					credentials := map[string]string{}
					if scenario == "existing-identity" {
						credentials["account_uuid"] = "same-identity"
					}
					data = map[string]any{"total": 1, "items": []any{map[string]any{"credentials": credentials}}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c, e := NewAccountClient(server.URL, "fixture")
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "existing-identity" || scenario == "unknown-identity" {
				if e = c.CheckClaudeIdentity(context.Background(), "same-identity"); e == nil {
					t.Fatal("uncertain identity allowed")
				}
			} else {
				if e = c.CheckQuarantine(context.Background(), 1); e == nil {
					t.Fatal("unsafe quarantine allowed")
				}
			}
		})
	}
}
