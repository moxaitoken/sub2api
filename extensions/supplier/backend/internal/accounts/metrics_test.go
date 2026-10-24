package accounts

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type metricStore struct {
	*memory
	ids  []string
	fail bool
}

func (m *metricStore) AccountConsumption(_ context.Context, supplier string, ids []string, _ time.Time) (map[string]ports.AccountConsumption, error) {
	m.ids = ids
	if supplier != "owner" || m.fail {
		return nil, errors.New("fixture read failed")
	}
	cost := "0.100000000000000001"
	return map[string]ports.AccountConsumption{"owned": {TodayCost: &cost, TotalCost: &cost, TodayStatus: "partial", TotalStatus: "partial"}}, nil
}

type metricRemote struct {
	*remote
	active atomic.Int32
	max    atomic.Int32
	calls  atomic.Int32
	fail   bool
}

func (r *metricRemote) AccountConfiguration(ctx context.Context, id int64) (ports.AccountConfiguration, error) {
	r.calls.Add(1)
	n := r.active.Add(1)
	defer r.active.Add(-1)
	for previous := r.max.Load(); n > previous; previous = r.max.Load() {
		if r.max.CompareAndSwap(previous, n) {
			break
		}
	}
	select {
	case <-ctx.Done():
		return ports.AccountConfiguration{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if r.fail {
		return ports.AccountConfiguration{}, ports.ErrUnavailable
	}
	return ports.AccountConfiguration{ConcurrencyLimit: int(id), Priority: 0}, nil
}

func TestAccountMetricsOwnershipPrecisionAndFailure(t *testing.T) {
	store := &metricStore{memory: &memory{}}
	source := &metricRemote{remote: &remote{}}
	svc := &Service{Store: store, Upstream: source}
	rows := []ports.Account{{ID: "owned", SupplierID: "owner", UpstreamID: 17}, {ID: "foreign", SupplierID: "other", UpstreamID: 99}, {ID: "pending", SupplierID: "owner"}}
	svc.EnrichAccounts(context.Background(), "owner", rows)
	if len(store.ids) != 1 || store.ids[0] != "owned" || source.calls.Load() != 1 {
		t.Fatal("read escaped ownership", store.ids, source.calls.Load())
	}
	if rows[0].Metrics.Configuration.ConcurrencyLimit != 17 || rows[0].Metrics.Configuration.Priority != 0 || *rows[0].Metrics.Consumption.TotalCost != "0.100000000000000001" {
		t.Fatalf("projection changed values: %+v", rows[0].Metrics)
	}
	if rows[1].Metrics.Configuration != nil || rows[1].Metrics.Consumption != nil || rows[2].Metrics.Configuration != nil {
		t.Fatal("unowned or provisioning metrics exposed")
	}
	store.fail = true
	source.fail = true
	svc.EnrichAccounts(context.Background(), "owner", rows)
	if rows[0].ID != "owned" || rows[0].Metrics.Configuration != nil || rows[0].Metrics.Consumption != nil {
		t.Fatal("failure erased base row or fabricated zero")
	}
}

func TestAccountMetricReadsBoundConcurrencyAndCancellation(t *testing.T) {
	source := &metricRemote{remote: &remote{}}
	svc := &Service{Store: &memory{}, Upstream: source}
	rows := make([]ports.Account, 25)
	for i := range rows {
		rows[i] = ports.Account{ID: "owned", SupplierID: "owner", UpstreamID: int64(i + 1)}
	}
	svc.EnrichAccounts(context.Background(), "owner", rows)
	if source.calls.Load() != 25 || source.max.Load() > 4 {
		t.Fatal("unbounded upstream reads", source.calls.Load(), source.max.Load())
	}
	for i := range rows {
		if rows[i].Metrics.Configuration.ConcurrencyLimit != i+1 {
			t.Fatal("configuration mapped to wrong row")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.EnrichAccounts(ctx, "owner", rows)
	if source.calls.Load() != 25 {
		t.Fatal("cancelled request performed upstream IO")
	}
}
