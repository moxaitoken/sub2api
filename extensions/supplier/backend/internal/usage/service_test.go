package usage

import (
	"testing"
	"time"
)

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
