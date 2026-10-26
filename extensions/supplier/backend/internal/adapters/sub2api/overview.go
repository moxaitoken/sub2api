package sub2api

import (
	"context"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"math"
	"math/big"
)

func validOverviewIDs(ids []int64) bool {
	if len(ids) == 0 || len(ids) > 100 {
		return false
	}
	for _, id := range ids {
		if id <= 0 {
			return false
		}
	}
	return true
}
func (c *AccountClient) RuntimeBatch(ctx context.Context, ids []int64) (ports.RuntimeBatch, error) {
	var out ports.RuntimeBatch
	if !validOverviewIDs(ids) {
		return out, badResponse()
	}
	if err := c.call(ctx, "POST", "/api/v1/admin/accounts/runtime/batch", map[string]any{"account_ids": ids}, &out, ""); err != nil {
		return out, err
	}
	if out.SampledAt.IsZero() || out.RPMWindow != "current_minute" || out.TokenWindow != 60 || out.Accounts == nil {
		return out, badResponse()
	}
	allowed := map[int64]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	for id, v := range out.Accounts {
		if !allowed[id] || (v.RPM != nil && *v.RPM < 0) || (v.Concurrency != nil && *v.Concurrency < 0) {
			return out, badResponse()
		}
		for _, s := range []*string{v.Tokens, v.UncachedTokens} {
			if s != nil {
				n, ok := new(big.Int).SetString(*s, 10)
				if !ok || n.Sign() < 0 || len(*s) > 80 {
					return out, badResponse()
				}
			}
		}
	}
	return out, nil
}
func (c *AccountClient) QuotaBatch(ctx context.Context, ids []int64) (map[int64]ports.AccountQuota, error) {
	if !validOverviewIDs(ids) {
		return nil, badResponse()
	}
	var out struct {
		Usage map[int64]ports.AccountQuota `json:"usage"`
	}
	if err := c.call(ctx, "POST", "/api/v1/admin/accounts/usage/batch", map[string]any{"account_ids": ids, "force": false}, &out, ""); err != nil {
		return nil, err
	}
	allowed := map[int64]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	for id, q := range out.Usage {
		if !allowed[id] {
			return nil, badResponse()
		}
		if q.Source != "passive" {
			delete(out.Usage, id)
			continue
		}
		for _, v := range []*ports.QuotaWindow{q.FiveHour, q.SevenDay, q.Fable} {
			if v != nil && v.Utilization != nil && (math.IsNaN(*v.Utilization) || math.IsInf(*v.Utilization, 0) || *v.Utilization < 0) {
				return nil, badResponse()
			}
		}
	}
	return out.Usage, nil
}
