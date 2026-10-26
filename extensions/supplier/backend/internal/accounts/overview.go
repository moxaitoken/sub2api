package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"math/big"
	"sort"
	"sync"
	"time"
)

type overviewEntry struct {
	fingerprint string
	ready       chan struct{}
	value       ports.PoolOverview
	expires     time.Time
	quotas      map[int64]ports.AccountQuota
	quotaAt     time.Time
}

// Overview never trusts browser upstream IDs, nor a single account-list page.
// A short per-supplier single-flight cache coalesces portal/admin polling.
func (s *Service) Overview(ctx context.Context, supplier string) (ports.PoolOverview, error) {
	store, ok := s.Store.(ports.OverviewStore)
	if !ok {
		return ports.PoolOverview{}, ports.ErrUnavailable
	}
	readCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	refs, err := store.OverviewAccounts(readCtx, supplier)
	if err != nil {
		return ports.PoolOverview{}, err
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	h := sha256.New()
	for _, a := range refs {
		fmt.Fprintf(h, "%s:%d;", a.ID, a.UpstreamID)
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	now := time.Now()
	s.overviewMu.Lock()
	if s.overviewEntries == nil {
		s.overviewEntries = map[string]*overviewEntry{}
	}
	if s.overviewGate == nil {
		s.overviewGate = make(chan struct{}, 8)
	}
	old := s.overviewEntries[supplier]
	if old != nil && old.fingerprint == fingerprint {
		if old.ready != nil {
			ready := old.ready
			s.overviewMu.Unlock()
			select {
			case <-ready:
				return old.value, nil
			case <-readCtx.Done():
				return ports.PoolOverview{}, readCtx.Err()
			}
		}
		if now.Before(old.expires) {
			v := old.value
			s.overviewMu.Unlock()
			return v, nil
		}
	}
	// Bound cache memory; only completed entries can be evicted.
	if len(s.overviewEntries) >= 256 {
		for key, e := range s.overviewEntries {
			if e.ready == nil && (now.After(e.expires) || key != supplier) {
				delete(s.overviewEntries, key)
				break
			}
		}
		if len(s.overviewEntries) >= 256 {
			s.overviewMu.Unlock()
			return ports.PoolOverview{}, ports.ErrUnavailable
		}
	}
	entry := &overviewEntry{fingerprint: fingerprint, ready: make(chan struct{})}
	if old != nil && old.fingerprint == fingerprint && now.Sub(old.quotaAt) < time.Minute {
		entry.quotas = old.quotas
		entry.quotaAt = old.quotaAt
	}
	s.overviewEntries[supplier] = entry
	s.overviewMu.Unlock()
	value, quotas, quotaAt := s.collectOverview(readCtx, refs, entry.quotas, entry.quotaAt)
	s.overviewMu.Lock()
	entry.value = value
	entry.quotas = quotas
	entry.quotaAt = quotaAt
	entry.expires = time.Now().Add(5 * time.Second)
	// Never carry previous-minute RPM past a minute boundary through our cache.
	if minuteEnd := value.ObservedAt.Truncate(time.Minute).Add(time.Minute); minuteEnd.Before(entry.expires) {
		entry.expires = minuteEnd
	}
	ready := entry.ready
	entry.ready = nil
	close(ready)
	s.overviewMu.Unlock()
	return value, nil
}

func (s *Service) collectOverview(ctx context.Context, refs []ports.OverviewAccount, cached map[int64]ports.AccountQuota, quotaAt time.Time) (ports.PoolOverview, map[int64]ports.AccountQuota, time.Time) {
	now := time.Now().UTC()
	total := len(refs)
	out := ports.PoolOverview{ObservedAt: now, Windows: map[string]ports.OverviewQuota{}, AccountQuotas: map[string]ports.AccountQuota{}}
	for _, key := range []string{"five_hour", "seven_day", "fable"} {
		out.Windows[key] = ports.OverviewQuota{Total: total}
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, a := range refs {
		if a.UpstreamID > 0 && !seen[a.UpstreamID] {
			seen[a.UpstreamID] = true
			ids = append(ids, a.UpstreamID)
		}
	}
	runtime := map[int64]ports.RuntimeAccount{}
	quotas := cached
	fetchQuota := cached == nil
	if fetchQuota {
		quotas = map[int64]ports.AccountQuota{}
	}
	source, ok := s.Upstream.(ports.OverviewSource)
	var mu sync.Mutex
	var wg sync.WaitGroup
	gate := make(chan struct{}, 4)
	if ok {
		for start := 0; start < len(ids); start += 100 {
			end := start + 100
			if end > len(ids) {
				end = len(ids)
			}
			chunk := append([]int64(nil), ids[start:end]...)
			select {
			case gate <- struct{}{}:
			case <-ctx.Done():
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-gate }()
				select {
				case s.overviewGate <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-s.overviewGate }()
				batch, err := source.RuntimeBatch(ctx, chunk)
				mu.Lock()
				if err == nil && !batch.SampledAt.After(now.Add(5*time.Second)) && now.Sub(batch.SampledAt) < 15*time.Second {
					for id, v := range batch.Accounts {
						if !batch.SampledAt.Truncate(time.Minute).Equal(now.Truncate(time.Minute)) {
							v.RPM = nil
						}
						runtime[id] = v
					}
				}
				mu.Unlock()
				if fetchQuota {
					values, e := source.QuotaBatch(ctx, chunk)
					if e == nil {
						mu.Lock()
						for id, v := range values {
							quotas[id] = v
						}
						mu.Unlock()
					}
				}
			}()
		}
	}
	wg.Wait()
	if fetchQuota {
		quotaAt = now
	}
	out.QuotaFetchedAt = &quotaAt
	sums := []*big.Int{new(big.Int), new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
	known := make([]int, 5)
	for _, id := range ids {
		v, found := runtime[id]
		if !found {
			continue
		}
		for i, n := range []*int64{v.RPM, v.Concurrency} {
			if n != nil {
				slot := i
				if i == 1 {
					slot = 3
				}
				sums[slot].Add(sums[slot], big.NewInt(*n))
				known[slot]++
			}
		}
		for i, n := range []*string{v.Tokens, v.UncachedTokens} {
			if n != nil {
				value, valid := new(big.Int).SetString(*n, 10)
				if valid && value.Sign() >= 0 {
					sums[i+1].Add(sums[i+1], value)
					known[i+1]++
				}
			}
		}
	}
	for _, a := range refs {
		q, found := quotas[a.UpstreamID]
		if !found {
			continue
		}
		out.AccountQuotas[a.ID] = q
		valid, high := 0, false
		for key, w := range map[string]*ports.QuotaWindow{"five_hour": q.FiveHour, "seven_day": q.SevenDay, "fable": q.Fable} {
			if !quotaFresh(q, w, now) {
				continue
			}
			valid++
			if *w.Utilization >= 90 {
				high = true
			}
			summary := out.Windows[key]
			summary.Known++
			if summary.Maximum == nil || *w.Utilization > *summary.Maximum {
				v := *w.Utilization
				summary.Maximum = &v
				summary.UpdatedAt = q.UpdatedAt
				summary.ResetsAt = w.ResetsAt
			}
			out.Windows[key] = summary
		}
		// Missing windows are not evidence that an account is low-load.
		if high || valid == 3 {
			known[4]++
			if high {
				sums[4].Add(sums[4], big.NewInt(1))
			}
		}
	}
	metrics := []*ports.OverviewMetric{&out.RPM, &out.TPM, &out.UncachedTPM, &out.Concurrency, &out.HighQuota}
	for i, m := range metrics {
		m.Total = total
		m.Known = known[i]
		m.Status = "unavailable"
		if total == 0 || known[i] > 0 {
			value := sums[i].String()
			m.Value = &value
			m.Status = "partial"
			if known[i] == total {
				m.Status = "ok"
			}
		}
	}
	return out, quotas, quotaAt
}

func quotaFresh(q ports.AccountQuota, w *ports.QuotaWindow, now time.Time) bool {
	return q.Source == "passive" && q.UpdatedAt != nil && !q.UpdatedAt.After(now.Add(5*time.Second)) && now.Sub(*q.UpdatedAt) <= 5*time.Minute && w != nil && w.Utilization != nil && (w.ResetsAt == nil || w.ResetsAt.After(now))
}
