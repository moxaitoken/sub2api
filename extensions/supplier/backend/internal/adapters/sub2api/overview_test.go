package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOverviewAdapterAllowlistPartialAndPassiveOnly(t *testing.T) {
	mode := "normal"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("x-api-key") != "fixture-only" {
			t.Error("wrong privilege boundary")
		}
		var input struct {
			IDs   []int64 `json:"account_ids"`
			Force bool    `json:"force"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if len(input.IDs) != 1 || input.IDs[0] != 17 || input.Force {
			t.Error("unscoped or active request")
		}
		var data any
		if strings.HasSuffix(r.URL.Path, "runtime/batch") {
			id := "17"
			if mode == "foreign" {
				id = "99"
			}
			data = map[string]any{"sampled_at": time.Now(), "rpm_window": "current_minute", "token_window_seconds": 60, "accounts": map[string]any{id: map[string]any{"rpm": 0, "concurrency": nil, "tokens": "9007199254740993", "uncached_tokens": "0", "credentials": "never-leak"}}}
		}
		if strings.HasSuffix(r.URL.Path, "usage/batch") {
			source := "passive"
			if mode == "active" {
				source = "active"
			}
			data = map[string]any{"usage": map[string]any{"17": map[string]any{"source": source, "updated_at": time.Now(), "five_hour": map[string]any{"utilization": 91.2, "resets_at": time.Now().Add(time.Hour)}, "credentials": "never-leak"}}, "errors": map[string]string{"18": "private-error"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	c, e := NewAccountClient(server.URL, "fixture-only")
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.RuntimeBatch(context.Background(), []int64{17})
	if e != nil || *b.Accounts[17].Tokens != "9007199254740993" || *b.Accounts[17].RPM != 0 || b.Accounts[17].Concurrency != nil {
		t.Fatal(b, e)
	}
	q, e := c.QuotaBatch(context.Background(), []int64{17})
	if e != nil || *q[17].FiveHour.Utilization != 91.2 {
		t.Fatal(q, e)
	}
	encoded, _ := json.Marshal(q)
	if strings.Contains(string(encoded), "never-leak") {
		t.Fatal("secret exposed")
	}
	mode = "foreign"
	if _, e = c.RuntimeBatch(context.Background(), []int64{17}); e == nil {
		t.Fatal("foreign source accepted")
	}
	mode = "active"
	q, e = c.QuotaBatch(context.Background(), []int64{17})
	if e != nil || len(q) != 0 {
		t.Fatal("active quota accepted")
	}
}
