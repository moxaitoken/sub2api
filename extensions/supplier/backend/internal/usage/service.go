// Package usage synchronizes allowlisted account-scoped usage and serves the
// supplier read model. It never changes billing, balances or gateway behavior.
package usage

import (
	"context"
	"reflect"
	"sort"
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
	days := map[string]int{"today": 1, "7d": 7, "30d": 30}[period]
	if days == 0 {
		return time.Time{}, time.Time{}, false
	}
	n := now.In(Location)
	end := time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, Location)
	return end.AddDate(0, 0, -days), end, true
}
func (s *Service) scan(ctx context.Context, account int64, start, end time.Time) ([]ports.UsageRecord, error) {
	rows := []ports.UsageRecord{}
	seen := map[int64]bool{}
	total := int64(-1)
	for page := 1; page <= 25; page++ {
		p, err := s.Source.UsagePage(ctx, account, start, end, page)
		if err != nil {
			return nil, err
		}
		if total < 0 {
			total = p.Total
		}
		if p.Total != total || total > 2500 || total < 0 {
			return nil, ports.ErrUnavailable
		}
		for _, r := range p.Items {
			if seen[r.ID] || r.AccountID != account || r.ID <= 0 || r.OccurredAt.Before(start) || !r.OccurredAt.Before(end) {
				return nil, ports.ErrUnavailable
			}
			seen[r.ID] = true
			rows = append(rows, r)
		}
		if int64(len(rows)) == total {
			sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
			return rows, nil
		}
		if len(p.Items) == 0 || int64(len(rows)) > total {
			return nil, ports.ErrUnavailable
		}
	}
	return nil, ports.ErrUnavailable
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
	all, err := s.Store.UsageAccounts(ctx, supplier)
	if err != nil {
		return zero, err
	}
	selected := []ports.UsageAccount{}
	for _, a := range all {
		if q.AccountID == "" || q.AccountID == a.ID {
			selected = append(selected, a)
		}
	}
	if q.AccountID != "" && len(selected) == 0 {
		return zero, ports.ErrNotFound
	}
	// Bound foreground work. Unsynced or failed accounts remain explicitly partial.
	syncCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	failed := false
	for i, a := range selected {
		if i >= 20 || syncCtx.Err() != nil {
			failed = true
			break
		}
		err = s.Store.SyncUsageWindow(syncCtx, supplier, a, start, end, func(c context.Context) ([]ports.UsageRecord, error) {
			first, e := s.scan(c, a.UpstreamID, start, end)
			if e != nil {
				return nil, e
			}
			second, e := s.scan(c, a.UpstreamID, start, end)
			if e != nil {
				return nil, e
			}
			if !reflect.DeepEqual(first, second) {
				return nil, ports.ErrUnavailable
			}
			return second, nil
		})
		if err != nil {
			failed = true
		}
	}
	report, err := s.Store.UsageReport(ctx, supplier, q, start, end)
	if failed {
		report.Status = "partial"
	}
	return report, err
}
