package sub2api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsagePageValidation(t *testing.T) {
	start := time.Date(2026, 10, 22, 0, 0, 0, 0, time.FixedZone("Shanghai", 8*3600))
	end := start.AddDate(0, 0, 1)
	for _, tc := range []struct {
		name, cost string
		account    int
		date       string
		valid      bool
	}{
		{"exact", "0.123456789012345678", 7, "2026-10-22T00:00:00+08:00", true},
		{"precision", "0.1234567890123456789", 7, "2026-10-22T00:00:00+08:00", false},
		{"negative", "-1", 7, "2026-10-22T00:00:00+08:00", false},
		{"exponent expansion", "1e1000000000", 7, "2026-10-22T00:00:00+08:00", false},
		{"foreign", "1", 8, "2026-10-22T00:00:00+08:00", false},
		{"exclusive end", "1", 7, "2026-10-23T00:00:00+08:00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"code":0,"data":{"total":1,"items":[{"id":1,"account_id":%d,"created_at":%q,"model":"claude","total_cost":%s,"user":{"email":"hidden"}}]}}`, tc.account, tc.date, tc.cost)
			}))
			defer up.Close()
			c, e := NewAccountClient(up.URL, "fixture")
			if e != nil {
				t.Fatal(e)
			}
			v, e := c.UsagePage(context.Background(), 7, start, end, 1)
			if (e == nil) != tc.valid {
				t.Fatal("validation", e)
			}
			if tc.valid && v.Items[0].Cost != tc.cost {
				t.Fatal("decimal changed")
			}
		})
	}
}
