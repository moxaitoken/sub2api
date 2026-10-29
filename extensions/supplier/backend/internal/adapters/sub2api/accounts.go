package sub2api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// AccountClient has its own fixed-origin client. The machine key is never used
// by the public login adapter or returned through supplier DTOs.
type AccountClient struct {
	client *Client
	key    string
}

const (
	supplierDefaultSessionIdleTimeoutMin = 5
	supplierDefaultWindowStickyReserve   = 10
)

func NewAccountClient(origin, key string) (*AccountClient, error) {
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return nil, fmt.Errorf("server-side admin key required")
	}
	c, err := New(origin, nil)
	if err != nil {
		return nil, err
	}
	return &AccountClient{c, key}, nil
}

func (c *AccountClient) call(ctx context.Context, method, path string, input, out any, key string) error {
	// A private client instance uses a credential marker understood only by
	// request; the public adapter's caller tokens never use this marker.
	return c.client.requestWithAdmin(ctx, method, path, c.key, input, out, key)
}

func (c *AccountClient) CheckQuarantine(ctx context.Context, id int64) error {
	if id <= 0 {
		return ports.ErrUnavailable
	}
	var group struct {
		Platform, Status string
		IsExclusive      bool   `json:"is_exclusive"`
		Fallback         *int64 `json:"fallback_group_id"`
		InvalidFallback  *int64 `json:"fallback_group_id_on_invalid_request"`
	}
	if err := c.call(ctx, "GET", "/api/v1/admin/groups/"+strconv.FormatInt(id, 10), nil, &group, ""); err != nil {
		return err
	}
	if group.Platform != "anthropic" || group.Status != "inactive" || !group.IsExclusive || group.Fallback != nil || group.InvalidFallback != nil {
		return ports.ErrForbidden
	}
	var keys struct {
		Total int64 `json:"total"`
		Items []any `json:"items"`
	}
	if err := c.call(ctx, "GET", fmt.Sprintf("/api/v1/admin/groups/%d/api-keys?page=1&page_size=1", id), nil, &keys, ""); err != nil {
		return err
	}
	if keys.Total != 0 || len(keys.Items) != 0 {
		return ports.ErrForbidden
	}
	return nil
}

func (c *AccountClient) GenerateClaudeOAuth(ctx context.Context) (ports.RemoteOAuth, error) {
	var result ports.RemoteOAuth
	err := c.call(ctx, "POST", "/api/v1/admin/accounts/generate-auth-url", map[string]any{}, &result, "")
	return result, err
}

func (c *AccountClient) ExchangeClaudeOAuth(ctx context.Context, session, code string) (ports.ClaudeTokens, error) {
	var result ports.ClaudeTokens
	err := c.call(ctx, "POST", "/api/v1/admin/accounts/exchange-code", map[string]string{"session_id": session, "code": code}, &result, "")
	return result, err
}

func (c *AccountClient) CheckClaudeIdentity(ctx context.Context, identity string) error {
	for page := 1; page <= 1000; page++ {
		var response struct {
			Items []ports.RemoteAccount `json:"items"`
			Total int64                 `json:"total"`
		}
		path := fmt.Sprintf("/api/v1/admin/accounts?platform=anthropic&type=oauth&page=%d&page_size=100", page)
		if err := c.call(ctx, "GET", path, nil, &response, ""); err != nil {
			return err
		}
		for _, a := range response.Items {
			uuid, ok := a.Credentials["account_uuid"].(string)
			if !ok || strings.TrimSpace(uuid) == "" {
				return ports.ErrUnavailable
			}
			if strings.EqualFold(uuid, identity) {
				return ports.ErrConflict
			}
		}
		if int64(page*100) >= response.Total {
			return nil
		}
		if len(response.Items) == 0 {
			return ports.ErrUnavailable
		}
	}
	return ports.ErrUnavailable
}

func (c *AccountClient) CreateClaudeAccount(ctx context.Context, name, key string, tokens ports.ClaudeTokens, group int64) (ports.RemoteAccount, error) {
	settings := ports.DefaultAccountSettings()
	settings.Concurrency = 100 // Preserve the retired legacy authorization defaults.
	settings.BaseRPM = nil     // Keep the retired legacy authorization path unchanged.
	return c.createClaudeAccount(ctx, name, key, tokens, group, nil, settings)
}
func (c *AccountClient) createClaudeAccount(ctx context.Context, name, key string, tokens ports.ClaudeTokens, group int64, proxy *int64, settings ports.AccountSettings) (ports.RemoteAccount, error) {
	var result ports.RemoteAccount
	if settings.Concurrency == 0 {
		settings.Concurrency = ports.DefaultAccountSettings().Concurrency
	}
	extra := map[string]any{}
	if settings.WindowCostLimit != nil {
		extra["window_cost_limit"] = *settings.WindowCostLimit
		extra["window_cost_sticky_reserve"] = supplierDefaultWindowStickyReserve
		if settings.WindowCostStickyReserve != nil {
			extra["window_cost_sticky_reserve"] = *settings.WindowCostStickyReserve
		}
	}
	if settings.MaxSessions != nil && *settings.MaxSessions > 0 {
		extra["max_sessions"] = *settings.MaxSessions
		idle := supplierDefaultSessionIdleTimeoutMin
		if settings.SessionIdleTimeoutMinutes != nil {
			idle = *settings.SessionIdleTimeoutMinutes
		}
		extra["session_idle_timeout_minutes"] = idle
	}
	if settings.BaseRPM != nil && *settings.BaseRPM > 0 {
		extra["base_rpm"] = *settings.BaseRPM
		if settings.RPMStrategy != "" {
			extra["rpm_strategy"] = settings.RPMStrategy
		}
		if settings.RPMStickyBuffer != nil {
			extra["rpm_sticky_buffer"] = *settings.RPMStickyBuffer
		}
	}
	payload := map[string]any{"name": name, "platform": "anthropic", "type": "oauth", "group_ids": []int64{group}, "concurrency": settings.Concurrency, "priority": 1, "upstream_billing_probe_enabled": false, "extra": extra, "credentials": map[string]any{"access_token": tokens.Access, "refresh_token": tokens.Refresh, "expires_at": strconv.FormatInt(tokens.ExpiresAt, 10), "account_uuid": tokens.AccountUUID, "org_uuid": tokens.OrgUUID}}
	if proxy != nil {
		if *proxy <= 0 {
			return result, ports.ErrForbidden
		}
		var caps struct {
			Safe bool `json:"schedulable_on_create"`
		}
		if err := c.call(ctx, "GET", "/api/v1/admin/accounts/creation-capabilities", nil, &caps, ""); err != nil {
			return result, err
		}
		if !caps.Safe {
			return result, ports.ErrUnavailable
		}
		payload["schedulable"] = false
		payload["proxy_id"] = *proxy
	}
	err := c.call(ctx, "POST", "/api/v1/admin/accounts", payload, &result, key)
	return result, err
}

func (c *AccountClient) ReadAccount(ctx context.Context, id int64) (ports.RemoteAccount, error) {
	var result ports.RemoteAccount
	if id <= 0 {
		return result, ports.ErrNotFound
	}
	err := c.call(ctx, "GET", "/api/v1/admin/accounts/"+strconv.FormatInt(id, 10), nil, &result, "")
	return result, err
}

func (c *AccountClient) SetAccountSchedulable(ctx context.Context, id int64, value bool) error {
	if id <= 0 {
		return ports.ErrNotFound
	}
	return c.call(ctx, "POST", fmt.Sprintf("/api/v1/admin/accounts/%d/schedulable", id), map[string]bool{"schedulable": value}, nil, "")
}

func (c *AccountClient) SetAccountGroups(ctx context.Context, id int64, groups []int64) error {
	if id <= 0 || len(groups) == 0 {
		return ports.ErrForbidden
	}
	for _, group := range groups {
		var result struct{ Platform, Status string }
		if err := c.call(ctx, "GET", fmt.Sprintf("/api/v1/admin/groups/%d", group), nil, &result, ""); err != nil {
			return err
		}
		if result.Platform != "anthropic" || result.Status != "active" {
			return ports.ErrForbidden
		}
	}
	var result struct {
		Success int `json:"success"`
		Failed  int `json:"failed"`
	}
	if err := c.call(ctx, "POST", "/api/v1/admin/accounts/bulk-update", map[string]any{"account_ids": []int64{id}, "group_ids": groups, "schedulable": false}, &result, ""); err != nil {
		return err
	}
	if result.Failed != 0 {
		return ports.ErrUnavailable
	}
	return nil
}

// NewAccountClient shares the same bounded transport, redirect rejection and
// error sanitization as authentication. No generic public forwarding exists.
func (c *Client) requestWithAdmin(ctx context.Context, method, path, key string, input, out any, idempotency string) error {
	ctx = context.WithValue(ctx, adminRequestContext{}, adminRequest{key: key, idempotency: idempotency})
	return c.request(ctx, method, path, "", input, out, ports.ClientInfo{})
}

type adminRequestContext struct{}
type adminRequest struct{ key, idempotency string }

var _ ports.AccountUpstream = (*AccountClient)(nil)
