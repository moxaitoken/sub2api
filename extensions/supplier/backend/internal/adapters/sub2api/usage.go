package sub2api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func (c *AccountClient) UsagePage(ctx context.Context, account int64, start, end time.Time, page int) (ports.UsageSourcePage, error) {
	var result ports.UsageSourcePage
	if account <= 0 || page < 1 || !start.Before(end) {
		return result, ports.ErrForbidden
	}
	q := url.Values{"account_id": {fmt.Sprint(account)}, "start_date": {start.Format("2006-01-02")}, "end_date": {end.AddDate(0, 0, -1).Format("2006-01-02")}, "timezone": {"Asia/Shanghai"}, "page": {fmt.Sprint(page)}, "page_size": {"100"}, "exact_total": {"true"}, "sort_by": {"id"}, "sort_order": {"asc"}}
	var out struct {
		Items []struct {
			ports.UsageRecord
			Cost json.Number `json:"total_cost"`
		}
		Total *int64 `json:"total"`
	}
	if err := c.call(ctx, "GET", "/api/v1/admin/usage?"+q.Encode(), nil, &out, ""); err != nil {
		return result, err
	}
	if out.Total == nil || *out.Total < 0 {
		return result, badResponse()
	}
	result.Total = *out.Total
	for _, item := range out.Items {
		r := item.UsageRecord
		if len(item.Cost) == 0 || len(item.Cost) > 80 {
			return result, badResponse()
		}
		// Bound exponent expansion before constructing arbitrary-precision values.
		if parts := strings.FieldsFunc(string(item.Cost), func(r rune) bool { return r == 'e' || r == 'E' }); len(parts) > 1 {
			exponent, err := strconv.Atoi(parts[1])
			if err != nil || exponent < -38 || exponent > 38 {
				return result, badResponse()
			}
		}
		cost, ok := new(big.Rat).SetString(string(item.Cost))
		if !ok || len(item.Cost) > 80 || cost.Sign() < 0 || len(cost.FloatString(18)) > 39 {
			return result, badResponse()
		}
		// Do not silently round beyond the existing numeric(38,18) projection.
		exact, _ := new(big.Rat).SetString(cost.FloatString(18))
		if exact.Cmp(cost) != 0 {
			return result, badResponse()
		}
		r.Cost = cost.FloatString(18)
		if r.ID <= 0 || r.AccountID != account || r.OccurredAt.Before(start) || !r.OccurredAt.Before(end) || r.Model == "" || len(r.Model) > 512 || r.Input < 0 || r.Output < 0 || r.CacheRead < 0 || r.CacheWrite < 0 {
			return result, badResponse()
		}
		result.Items = append(result.Items, r)
	}
	if len(result.Items) > 100 {
		return ports.UsageSourcePage{}, badResponse()
	}
	return result, nil
}

// Discover the oldest still-retained record, including usage predating module
// onboarding. No user/API-key filter: all callers of this account are included.
func (c *AccountClient) UsageEarliest(ctx context.Context, account int64) (time.Time, error) {
	if account <= 0 {
		return time.Time{}, ports.ErrForbidden
	}
	q := url.Values{"account_id": {fmt.Sprint(account)}, "page": {"1"}, "page_size": {"1"}, "exact_total": {"true"}, "sort_by": {"created_at"}, "sort_order": {"asc"}}
	var out struct {
		Items []struct {
			Account int64     `json:"account_id"`
			Created time.Time `json:"created_at"`
		}
		Total *int64 `json:"total"`
	}
	if err := c.call(ctx, "GET", "/api/v1/admin/usage?"+q.Encode(), nil, &out, ""); err != nil {
		return time.Time{}, err
	}
	if out.Total == nil || *out.Total < 0 || len(out.Items) > 1 || (*out.Total == 0) != (len(out.Items) == 0) {
		return time.Time{}, badResponse()
	}
	if *out.Total == 0 {
		return time.Time{}, nil
	}
	r := out.Items[0]
	if r.Account != account || r.Created.Before(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) || r.Created.After(time.Now().Add(24*time.Hour)) {
		return time.Time{}, badResponse()
	}
	return r.Created, nil
}
