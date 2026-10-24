package sub2api

import (
	"context"
	"errors"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func (c *AccountClient) DeleteProxy(ctx context.Context, id int64) error {
	if id <= 0 {
		return ports.ErrNotFound
	}
	var result map[string]any
	err := c.call(ctx, "DELETE", fmt.Sprintf("/api/v1/admin/proxies/%d", id), nil, &result, "")
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	return err
}

var _ ports.OnboardingUpstream = (*AccountClient)(nil)

func (c *AccountClient) ReadProxy(ctx context.Context, id int64) (ports.RemoteProxy, error) {
	var p ports.RemoteProxy
	if id <= 0 {
		return p, ports.ErrNotFound
	}
	e := c.call(ctx, "GET", fmt.Sprintf("/api/v1/admin/proxies/%d", id), nil, &p, "")
	return p, e
}
func (c *AccountClient) CreateProxy(ctx context.Context, name string, p ports.ProxyConfig, key string) (ports.RemoteProxy, error) {
	var out ports.RemoteProxy
	payload := map[string]any{"name": name, "protocol": p.Protocol, "host": p.Host, "port": p.Port, "username": p.Username, "password": p.Password, "fallback_mode": "none"}
	e := c.call(ctx, "POST", "/api/v1/admin/proxies", payload, &out, key)
	return out, e
}
func (c *AccountClient) TestProxy(ctx context.Context, id int64) (bool, error) {
	var out struct {
		Success bool `json:"success"`
	}
	e := c.call(ctx, "POST", fmt.Sprintf("/api/v1/admin/proxies/%d/test", id), map[string]any{}, &out, "")
	return out.Success, e
}
func (c *AccountClient) AccountGroup(ctx context.Context, id int64) (ports.AccountGroup, error) {
	var g ports.AccountGroup
	e := c.call(ctx, "GET", fmt.Sprintf("/api/v1/admin/groups/%d", id), nil, &g, "")
	return g, e
}
func (c *AccountClient) GenerateBoundOAuth(ctx context.Context, proxy int64) (ports.RemoteOAuth, error) {
	var out ports.RemoteOAuth
	if proxy <= 0 {
		return out, ports.ErrForbidden
	}
	e := c.call(ctx, "POST", "/api/v1/admin/accounts/generate-auth-url", map[string]int64{"proxy_id": proxy}, &out, "")
	return out, e
}
func (c *AccountClient) ExchangeBoundOAuth(ctx context.Context, session, code string, proxy int64) (ports.ClaudeTokens, error) {
	var out ports.ClaudeTokens
	if proxy <= 0 {
		return out, ports.ErrForbidden
	}
	e := c.call(ctx, "POST", "/api/v1/admin/accounts/exchange-code", map[string]any{"session_id": session, "code": code, "proxy_id": proxy}, &out, "")
	return out, e
}
func (c *AccountClient) CreateBoundAccount(ctx context.Context, name, key string, tokens ports.ClaudeTokens, group, proxy int64, settings ports.AccountSettings) (ports.RemoteAccount, error) {
	return c.createClaudeAccount(ctx, name, key, tokens, group, &proxy, settings)
}
