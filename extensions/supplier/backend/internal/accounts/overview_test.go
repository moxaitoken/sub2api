package accounts

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type overviewStoreFixture struct {
	*memory
	refs map[string][]ports.OverviewAccount
}

func (s *overviewStoreFixture) OverviewAccounts(_ context.Context, id string) ([]ports.OverviewAccount, error) {
	return append([]ports.OverviewAccount(nil), s.refs[id]...), nil
}

type overviewSourceFixture struct {
	*remote
	runtimeCalls atomic.Int32
	quotaCalls   atomic.Int32
	fail         bool
	stale        bool
	seen         sync.Map
}

func (s *overviewSourceFixture) RuntimeBatch(_ context.Context, ids []int64) (ports.RuntimeBatch, error) {
	s.runtimeCalls.Add(1)
	if s.fail {
		return ports.RuntimeBatch{}, errors.New("synthetic source failure")
	}
	time.Sleep(time.Millisecond)
	now := time.Now().UTC()
	if s.stale {
		now = now.Add(-time.Minute)
	}
	out := ports.RuntimeBatch{SampledAt: now, RPMWindow: "current_minute", TokenWindow: 60, Accounts: map[int64]ports.RuntimeAccount{}}
	for _, id := range ids {
		s.seen.Store(id, true)
		rpm, conc := int64(2), int64(1)
		tokens, uncached := "9007199254740993", "5"
		out.Accounts[id] = ports.RuntimeAccount{RPM: &rpm, Concurrency: &conc, Tokens: &tokens, UncachedTokens: &uncached}
	}
	return out, nil
}
func (s *overviewSourceFixture) QuotaBatch(_ context.Context, ids []int64) (map[int64]ports.AccountQuota, error) {
	s.quotaCalls.Add(1)
	if s.fail {
		return nil, errors.New("synthetic source failure")
	}
	out := map[int64]ports.AccountQuota{}
	for _, id := range ids {
		now := time.Now().UTC()
		reset := now.Add(time.Hour)
		pct := 95.0
		if id == 2 {
			now = now.Add(-10 * time.Minute)
		}
		w := &ports.QuotaWindow{Utilization: &pct, ResetsAt: &reset}
		out[id] = ports.AccountQuota{Source: "passive", UpdatedAt: &now, FiveHour: w, SevenDay: w, Fable: w}
	}
	return out, nil
}
func TestOverviewAllOwnedAccountsPrecisionCacheAndPartial(t *testing.T) {
	store := &overviewStoreFixture{memory: &memory{}, refs: map[string][]ports.OverviewAccount{"one": {{ID: "a", UpstreamID: 1}, {ID: "b", UpstreamID: 2}, {ID: "not-linked"}}, "other": {{ID: "foreign", UpstreamID: 99}}}}
	source := &overviewSourceFixture{remote: &remote{}}
	svc := &Service{Store: store, Upstream: source}
	v, err := svc.Overview(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	if *v.TPM.Value != "18014398509481986" || v.TPM.Known != 2 || v.TPM.Total != 3 || v.TPM.Status != "partial" || *v.Concurrency.Value != "2" {
		t.Fatal("not all accounts or precision lost", v)
	}
	if _, found := source.seen.Load(int64(99)); found {
		t.Fatal("foreign resource fetched")
	}
	if v.Windows["five_hour"].Known != 1 || *v.Windows["five_hour"].Maximum != 95 || *v.HighQuota.Value != "1" || v.HighQuota.Status != "partial" {
		t.Fatal("stale/missing quota treated as zero", v)
	}
	_, err = svc.Overview(context.Background(), "one")
	if err != nil || source.runtimeCalls.Load() != 1 || source.quotaCalls.Load() != 1 {
		t.Fatal("cache not shared", err)
	}
	svc.overviewMu.Lock()
	svc.overviewEntries["one"].expires = time.Time{}
	svc.overviewMu.Unlock()
	_, err = svc.Overview(context.Background(), "one")
	if err != nil || source.runtimeCalls.Load() != 2 || source.quotaCalls.Load() != 1 {
		t.Fatal("quota polled at runtime frequency", err)
	}
	store.refs["one"] = []ports.OverviewAccount{{ID: "a", UpstreamID: 1}}
	v, err = svc.Overview(context.Background(), "one")
	if err != nil || v.RPM.Total != 1 || *v.RPM.Value != "2" || source.quotaCalls.Load() != 2 {
		t.Fatal("membership cache not invalidated", v, err)
	}
	source.fail = true
	store.refs["one"] = append(store.refs["one"], ports.OverviewAccount{ID: "new", UpstreamID: 3})
	v, err = svc.Overview(context.Background(), "one")
	if err != nil || v.RPM.Value != nil || v.RPM.Status != "unavailable" || v.TPM.Value != nil {
		t.Fatal("failure fabricated zero", v, err)
	}
	v, err = svc.Overview(context.Background(), "empty")
	if err != nil || *v.RPM.Value != "0" || v.RPM.Status != "ok" {
		t.Fatal("empty supplier should be known zero", v, err)
	}
}
func TestOverviewSingleflightAllPagesAndStaleSource(t *testing.T) {
	refs := []ports.OverviewAccount{}
	for i := 1; i <= 205; i++ {
		refs = append(refs, ports.OverviewAccount{ID: fmt.Sprint(i), UpstreamID: int64(i)})
	}
	source := &overviewSourceFixture{remote: &remote{}}
	svc := &Service{Store: &overviewStoreFixture{memory: &memory{}, refs: map[string][]ports.OverviewAccount{"owner": refs}}, Upstream: source}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := svc.Overview(context.Background(), "owner")
			if e != nil || v.Concurrency.Known != 205 || *v.Concurrency.Value != "205" {
				t.Error("incomplete overview", e)
			}
		}()
	}
	wg.Wait()
	if source.runtimeCalls.Load() != 3 || source.quotaCalls.Load() != 3 {
		t.Fatal("concurrent reads multiplied batches", source.runtimeCalls.Load(), source.quotaCalls.Load())
	}
	source.stale = true
	svc.overviewEntries = nil
	v, e := svc.Overview(context.Background(), "owner")
	if e != nil || v.Concurrency.Value != nil || v.RPM.Value != nil {
		t.Fatal("stale runtime accepted", v, e)
	}
}
