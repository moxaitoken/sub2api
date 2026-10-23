// Package usage synchronizes allowlisted account-scoped usage and serves the
// supplier read model. It never changes billing, balances or gateway behavior.
package usage

import (
	"context"
	"time"
	_ "time/tzdata"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type Service struct {
	Store  ports.UsageStore
	Source ports.UsageSource
	Now    func() time.Time
}

var Location = func() *time.Location {
	l, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		panic(e)
	}
	return l
}()

func Window(period string, now time.Time) (time.Time, time.Time, bool) {
	days := map[string]int{"today": 1, "7d": 7, "30d": 30, "all": 1}[period]
	if days == 0 {
		return time.Time{}, time.Time{}, false
	}
	n := now.In(Location)
	end := time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, Location)
	return end.AddDate(0, 0, -days), end, true
}
func (s *Service) Read(ctx context.Context, supplier string, q ports.UsageQuery) (ports.UsageReport, error) {
	var zero ports.UsageReport
	if s == nil || s.Store == nil || s.Source == nil {
		return zero, ports.ErrUnavailable
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	start, end, ok := Window(q.Period, now)
	if !ok {
		return zero, ports.ErrConflict
	}
	// HTTP reads one database snapshot; opening a page is never a prerequisite
	// for synchronization and cannot launch duplicate source scans.
	return s.Store.UsageReport(ctx, supplier, q, start, end)
}
