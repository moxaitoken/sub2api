package accounts

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// EnrichAccounts mirrors owned upstream state and enriches cached metrics.
// Read/synchronization failures preserve the base row with an explicit marker.
// Ownership is resolved by the existing list query, never by browser upstream IDs.
// At most four upstream reads run concurrently, within one four-second budget.
func (s *Service) EnrichAccounts(ctx context.Context, supplier string, rows []ports.Account) {
	if len(rows) == 0 {
		return
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		rows[i].Metrics = &ports.AccountMetrics{}
		rows[i].StateSync = "pending"
		if rows[i].SupplierID == supplier && rows[i].UpstreamID > 0 {
			ids = append(ids, rows[i].ID)
			rows[i].StateSync = "failed"
		}
	}
	if store, ok := s.Store.(ports.AccountConsumptionStore); ok && len(ids) > 0 {
		metricsCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		values, err := store.AccountConsumption(metricsCtx, supplier, ids, time.Now())
		cancel()
		if err == nil {
			for i := range rows {
				if rows[i].SupplierID == supplier {
					if value, found := values[rows[i].ID]; found {
						v := value
						rows[i].Metrics.Consumption = &v
					}
				}
			}
		}
	}
	source, ok := s.Upstream.(ports.AccountConfigurationSource)
	if !ok {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	jobs := make(chan int, len(rows))
	for i := range rows {
		if rows[i].SupplierID == supplier && rows[i].UpstreamID > 0 {
			jobs <- i
		}
	}
	close(jobs)
	var workers sync.WaitGroup
	for n := 0; n < 4; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if readCtx.Err() != nil {
					return
				}
				value, err := source.AccountConfiguration(readCtx, rows[i].UpstreamID)
				if err == nil {
					rows[i].Metrics.Configuration = &value
					if store, ok := s.Store.(ports.AccountObservationStore); ok && value.State != nil {
						updated, syncErr := store.ObserveAccount(readCtx, supplier, rows[i], *value.State)
						if syncErr == nil {
							updated.Metrics = rows[i].Metrics
							updated.StateSync = "synced"
							rows[i] = updated
						} else if errors.Is(syncErr, ports.ErrConflict) {
							rows[i].StateSync = "pending"
						}
					}
				}
			}
		}()
	}
	workers.Wait()
}
