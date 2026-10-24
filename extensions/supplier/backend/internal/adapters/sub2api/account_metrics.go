package sub2api

import (
	"context"
	"strconv"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// Decode only safe config fields. No credentials, user, API key, proxy password
// or upstream IDs escape through the supplier metric response.
func (c *AccountClient) AccountConfiguration(ctx context.Context, id int64) (ports.AccountConfiguration, error) {
	var zero ports.AccountConfiguration
	if id <= 0 {
		return zero, ports.ErrNotFound
	}
	var out struct {
		ID          int64  `json:"id"`
		Platform    string `json:"platform"`
		Type        string `json:"type"`
		Concurrency *int   `json:"concurrency"`
		Priority    *int   `json:"priority"`
		Status      string `json:"status"`
		Schedulable *bool  `json:"schedulable"`
	}
	if err := c.call(ctx, "GET", "/api/v1/admin/accounts/"+strconv.FormatInt(id, 10), nil, &out, ""); err != nil {
		return zero, err
	}
	if out.ID != id || out.Platform != "anthropic" || out.Type != "oauth" || out.Concurrency == nil || *out.Concurrency < 1 || out.Priority == nil || *out.Priority < 0 {
		return zero, badResponse()
	}
	value := ports.AccountConfiguration{ConcurrencyLimit: *out.Concurrency, Priority: *out.Priority}
	if out.Schedulable != nil && (out.Status == "active" || out.Status == "inactive" || out.Status == "error") {
		value.State = &ports.AccountObservation{Status: out.Status, Schedulable: *out.Schedulable}
	}
	return value, nil
}
