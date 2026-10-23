package usage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// Run uses the existing service process and module database, not another
// service/queue. Each page commits its checkpoint and releases its lease.
func (s *Service) Run(ctx context.Context) error {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	owner := hex.EncodeToString(key[:])
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		// Source failures are persisted for retry; they never stop the server.
		if err := s.Tick(work, owner, 50); err != nil && ctx.Err() == nil {
			log.Print("supplier usage synchronization delayed; will retry")
		}
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
}

// Tick is bounded for fairness. There is no record/account limit on eventual
// coverage: another tick resumes the persisted page even after a restart.
func (s *Service) Tick(ctx context.Context, owner string, pages int) error {
	store, ok := s.Store.(ports.UsageSyncStore)
	source, sourceOK := s.Source.(ports.UsageHistorySource)
	if !ok || !sourceOK || owner == "" {
		return ports.ErrUnavailable
	}
	accounts, err := store.UsageDiscoveries(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		first, e := source.UsageEarliest(ctx, account.UpstreamID)
		if e == nil {
			e = store.DiscoverUsage(ctx, account, first)
		}
		if e != nil {
			if e = store.RetryUsageDiscovery(ctx, account); e != nil {
				return e
			}
		}
	}
	for i := 0; i < pages && ctx.Err() == nil; i++ {
		job, exists, e := store.ClaimUsagePage(ctx, owner)
		if e != nil || !exists {
			return e
		}
		page, e := s.Source.UsagePage(ctx, job.Account.UpstreamID, job.Day, job.Day.AddDate(0, 0, 1), job.Page)
		if e == nil {
			e = store.ApplyUsagePage(ctx, job, page)
		}
		if e != nil {
			// Use a short independent context to release a lease on cancellation.
			retry, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			e = store.RetryUsagePage(retry, job)
			cancel()
			if e != nil {
				return e
			}
		}
	}
	return ctx.Err()
}
