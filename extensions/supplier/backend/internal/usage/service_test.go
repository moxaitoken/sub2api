package usage

import (
	"context"
	"errors"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"testing"
	"time"
)

type sourceFunc func(context.Context, int64, time.Time, time.Time, int) (ports.UsageSourcePage, error)

func (f sourceFunc) UsagePage(c context.Context, a int64, s, e time.Time, p int) (ports.UsageSourcePage, error) {
	return f(c, a, s, e, p)
}
func TestShanghaiWindows(t *testing.T) {
	now := time.Date(2026, 10, 21, 16, 1, 0, 0, time.UTC)
	for period, days := range map[string]int{"today": 1, "7d": 7, "30d": 30} {
		s, e, ok := Window(period, now)
		if !ok || s.Hour() != 0 || e.Format("2006-01-02") != "2026-10-23" || int(e.Sub(s).Hours()) != days*24 {
			t.Fatal(period, s, e)
		}
	}
	if _, _, ok := Window("year", now); ok {
		t.Fatal("unbounded period accepted")
	}
}
func TestRejectUnstableOrUnscopedPages(t *testing.T) {
	s, e, _ := Window("today", time.Now())
	r := ports.UsageRecord{ID: 1, AccountID: 2, OccurredAt: s.Add(time.Hour)}
	for _, scenario := range []string{"duplicate", "foreign", "time", "total_changed", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			svc := &Service{Source: sourceFunc(func(_ context.Context, _ int64, _, _ time.Time, p int) (ports.UsageSourcePage, error) {
				x := r
				switch scenario {
				case "foreign":
					x.AccountID = 3
				case "time":
					x.OccurredAt = e
				case "empty":
					return ports.UsageSourcePage{Total: 1}, nil
				}
				total := int64(2)
				if scenario == "total_changed" && p > 1 {
					total = 3
				}
				return ports.UsageSourcePage{Total: total, Items: []ports.UsageRecord{x}}, nil
			})}
			if _, err := svc.scan(context.Background(), 2, s, e); !errors.Is(err, ports.ErrUnavailable) {
				t.Fatal("unsafe page accepted", err)
			}
		})
	}
}
