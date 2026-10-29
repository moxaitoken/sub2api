package accounts

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func TestAccountSettingsDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"omitted", `{}`, `{"concurrency":20,"max_sessions":100,"session_idle_timeout_minutes":5,"base_rpm":100,"rpm_strategy":"tiered"}`},
		{"only concurrency", `{"concurrency":7}`, `{"concurrency":7,"max_sessions":100,"session_idle_timeout_minutes":5,"base_rpm":100,"rpm_strategy":"tiered"}`},
		{"only sessions", `{"max_sessions":25}`, `{"concurrency":20,"max_sessions":25,"session_idle_timeout_minutes":5,"base_rpm":100,"rpm_strategy":"tiered"}`},
		{"only rpm", `{"base_rpm":50}`, `{"concurrency":20,"max_sessions":100,"session_idle_timeout_minutes":5,"base_rpm":50,"rpm_strategy":"tiered"}`},
		{"disabled with omitted concurrency", `{"max_sessions":0,"base_rpm":0}`, `{"concurrency":20,"max_sessions":0,"session_idle_timeout_minutes":5,"base_rpm":0,"rpm_strategy":"tiered"}`},
		{"custom above defaults", `{"concurrency":120,"max_sessions":150,"session_idle_timeout_minutes":8,"base_rpm":23,"rpm_strategy":"sticky_exempt","window_cost_limit":12.34,"window_cost_sticky_reserve":0}`, `{"concurrency":120,"max_sessions":150,"session_idle_timeout_minutes":8,"base_rpm":23,"rpm_strategy":"sticky_exempt","window_cost_limit":12.34,"window_cost_sticky_reserve":0}`},
		{"disabled", `{"concurrency":7,"max_sessions":0,"base_rpm":0}`, `{"concurrency":7,"max_sessions":0,"session_idle_timeout_minutes":5,"base_rpm":0,"rpm_strategy":"tiered"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input ports.AccountSettings
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}
			got, err := normalizeAccountSettings(input)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(got)
			var actual, want any
			json.Unmarshal(b, &actual)
			json.Unmarshal([]byte(tc.want), &want)
			a, _ := json.Marshal(actual)
			w, _ := json.Marshal(want)
			if string(a) != string(w) {
				t.Fatalf("got %s, want %s", a, w)
			}
		})
	}
}

func TestAccountSettingsRejectInvalidInput(t *testing.T) {
	for _, raw := range []string{`{"concurrency":-1}`, `{"max_sessions":-1}`, `{"session_idle_timeout_minutes":0}`, `{"base_rpm":10001}`, `{"base_rpm":-1}`, `{"rpm_strategy":"invalid"}`, `{"rpm_sticky_buffer":0}`, `{"window_cost_limit":0}`, `{"window_cost_sticky_reserve":-1}`} {
		var input ports.AccountSettings
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		if _, err := normalizeAccountSettings(input); err == nil {
			t.Errorf("accepted invalid settings %s", raw)
		}
	}
	for _, value := range []float64{math.Inf(1), math.NaN()} {
		if _, err := normalizeAccountSettings(ports.AccountSettings{WindowCostLimit: &value}); err == nil {
			t.Error("accepted non-finite cost")
		}
	}
}
