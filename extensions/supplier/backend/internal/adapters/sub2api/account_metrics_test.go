package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountConfigurationAllowlist(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-id", "wrong-platform", "missing", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/admin/accounts/17" || r.Header.Get("x-api-key") != "fixture-only" {
					t.Error("wrong endpoint or credential boundary")
				}
				data := map[string]any{"id": 17, "platform": "anthropic", "type": "oauth", "concurrency": 23, "priority": 0, "credentials": map[string]string{"access_token": "secret-canary"}, "user": "private-canary"}
				switch scenario {
				case "wrong-id":
					data["id"] = 18
				case "wrong-platform":
					data["platform"] = "openai"
				case "missing":
					delete(data, "priority")
				case "invalid":
					data["concurrency"] = 0
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			client, err := NewAccountClient(server.URL, "fixture-only")
			if err != nil {
				t.Fatal(err)
			}
			value, err := client.AccountConfiguration(context.Background(), 17)
			if scenario != "valid" {
				if err == nil {
					t.Fatal("accepted malformed source")
				}
				return
			}
			if err != nil || value.ConcurrencyLimit != 23 || value.Priority != 0 {
				t.Fatal(value, err)
			}
			encoded, _ := json.Marshal(value)
			if strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), "credentials") {
				t.Fatal("source secrets leaked")
			}
		})
	}
}
