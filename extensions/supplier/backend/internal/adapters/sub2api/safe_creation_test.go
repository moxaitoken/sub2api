package sub2api

import (
	"context"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBoundCreationRequiresAtomicPauseCapability(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "old upstream refused", true: "safe paused creation"}[supported], func(t *testing.T) {
			creates := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/admin/accounts/creation-capabilities" {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]bool{"schedulable_on_create": supported}})
					return
				}
				if r.Method != "POST" || r.URL.Path != "/api/v1/admin/accounts" {
					t.Error("unexpected request", r.URL.Path)
				}
				creates++
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				if in["schedulable"] != false {
					t.Error("missing atomic pause")
				}
				if in["group_ids"].([]any)[0].(float64) != 8 {
					t.Error("wrong selected group")
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 5, "schedulable": false}})
			}))
			defer up.Close()
			c, _ := NewAccountClient(up.URL, "fixture-key")
			_, e := c.CreateBoundAccount(context.Background(), "fixture", "fixture-idempotency", ports.ClaudeTokens{}, 8, 2, ports.DefaultAccountSettings())
			if supported && (e != nil || creates != 1) {
				t.Fatal(e, creates)
			}
			if !supported && (e == nil || creates != 0) {
				t.Fatal("unsafe upstream used", e, creates)
			}
		})
	}
}
