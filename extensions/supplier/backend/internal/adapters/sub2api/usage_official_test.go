package sub2api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsageUsesOfficialCostWithoutCustomerOrAccountMultipliers(t *testing.T) {
	start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for _, actual := range []string{"10", "0", "200"} {
		t.Run(actual, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"code":0,"data":{"total":1,"items":[{"id":1,"account_id":7,"created_at":%q,"model":"claude-fixture","input_tokens":1000000,"total_cost":20,"actual_cost":%s,"rate_multiplier":0.5,"account_rate_multiplier":0.2,"account_cost":4}]}}`, start.Add(time.Hour).Format(time.RFC3339), actual)
			}))
			defer up.Close()
			c, e := NewAccountClient(up.URL, "fixture-only")
			if e != nil {
				t.Fatal(e)
			}
			p, e := c.UsagePage(context.Background(), 7, start, start.AddDate(0, 0, 1), 1)
			if e != nil || len(p.Items) != 1 || p.Items[0].Cost != "20.000000000000000000" || p.Items[0].Input != 1000000 {
				t.Fatalf("official raw consumption changed: %+v %v", p, e)
			}
		})
	}
}
