// Package sub2api translates the supplier service's ports to the upstream HTTP API.
// It deliberately does not expose upstream DTOs, cookies, messages, or raw errors.
package sub2api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

const maxResponseBytes = 1 << 20

// Error contains only adapter-owned, non-sensitive diagnostics.
type Error struct {
	Status int
	Reason string
}

func (e *Error) Error() string { return fmt.Sprintf("upstream HTTP %d: %s", e.Status, e.Reason) }

func (e *Error) StatusCode() int    { return e.Status }
func (e *Error) ReasonCode() string { return e.Reason }

func (e *Error) Unwrap() error {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ports.ErrForbidden
	case http.StatusNotFound:
		return ports.ErrNotFound
	default:
		return ports.ErrUnavailable
	}
}

func statusError(status int) *Error {
	reason := "UPSTREAM_REJECTED"
	switch {
	case status == http.StatusUnauthorized:
		reason = "UPSTREAM_UNAUTHORIZED"
	case status == http.StatusForbidden:
		reason = "UPSTREAM_FORBIDDEN"
	case status == http.StatusNotFound:
		reason = "UPSTREAM_NOT_FOUND"
	case status == http.StatusLocked:
		reason = "UPSTREAM_LOCKED"
	case status == http.StatusTooManyRequests:
		reason = "UPSTREAM_RATE_LIMITED"
	case status >= http.StatusInternalServerError:
		reason = "UPSTREAM_UNAVAILABLE"
	}
	return &Error{Status: status, Reason: reason}
}

func badResponse() *Error {
	return &Error{Status: http.StatusBadGateway, Reason: "UPSTREAM_BAD_RESPONSE"}
}

// Client owns a fixed origin and a bounded HTTP client. All request paths are
// adapter constants; callers cannot send arbitrary URLs or browser headers.
type Client struct {
	baseURL string
	http    *http.Client
}

var _ ports.Upstream = (*Client)(nil)

// New accepts an explicitly configured internal HTTP or HTTPS origin. A supplied
// client is cloned, with redirects and cookies disabled and timeout capped at ten
// seconds. Standard transports are cloned without environment proxy discovery.
func New(baseURL string, client *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || strings.Contains(baseURL, "#") || strings.HasSuffix(u.Host, ":") ||
		strings.ContainsAny(baseURL, "\r\n\t") {
		return nil, &Error{Status: http.StatusInternalServerError, Reason: "UPSTREAM_INVALID_ORIGIN"}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, &Error{Status: http.StatusInternalServerError, Reason: "UPSTREAM_INVALID_ORIGIN"}
		}
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		c.Timeout = 10 * time.Second
	}
	c.Jar = nil
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.Transport == nil {
		// Do not inherit a replaceable global DefaultTransport or its proxy settings.
		c.Transport = &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		}
	} else if transport, ok := c.Transport.(*http.Transport); ok {
		cloned := transport.Clone()
		cloned.Proxy = nil
		c.Transport = cloned
	}
	return &Client{baseURL: u.Scheme + "://" + u.Host, http: &c}, nil
}

type envelope struct {
	Code *int            `json:"code"`
	Data json.RawMessage `json:"data"`
}

func decodeSingle(data []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(result); err != nil {
		return badResponse()
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return badResponse()
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path, access string, input, output any, info ports.ClientInfo) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return badResponse()
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return badResponse()
	}
	req.Header.Set("Accept", "application/json")
	if admin, ok := ctx.Value(adminRequestContext{}).(adminRequest); ok {
		req.Header.Set("x-api-key", admin.key)
		if admin.idempotency != "" {
			req.Header.Set("Idempotency-Key", admin.idempotency)
		}
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if access != "" {
		if strings.ContainsAny(access, "\r\n") {
			return &Error{Status: http.StatusUnauthorized, Reason: "UPSTREAM_INVALID_TOKEN"}
		}
		req.Header.Set("Authorization", "Bearer "+access)
	}
	// ClientInfo is constructed by the trusted local HTTP boundary, not copied
	// from arbitrary browser request headers. Only canonical IPs are forwarded.
	if ip := net.ParseIP(info.IP); ip != nil {
		req.Header.Set("X-Real-IP", ip.String())
		req.Header.Set("X-Forwarded-For", ip.String())
	}
	for key, value := range map[string]string{"User-Agent": info.UserAgent, "Accept-Language": info.Language} {
		if len(value) <= 1024 && !strings.ContainsAny(value, "\r\n") {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Status: http.StatusBadGateway, Reason: "UPSTREAM_TRANSPORT_ERROR"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never retain, parse, return, or log an upstream error body.
		return statusError(resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return badResponse()
	}
	var wrapper envelope
	if err := decodeSingle(data, &wrapper); err != nil || wrapper.Code == nil || *wrapper.Code != 0 {
		return badResponse()
	}
	if len(wrapper.Data) == 0 || bytes.Equal(bytes.TrimSpace(wrapper.Data), []byte("null")) {
		return badResponse()
	}
	if output != nil {
		return decodeSingle(wrapper.Data, output)
	}
	return nil
}

type authData struct {
	Requires2FA  bool   `json:"requires_2fa"`
	TempToken    string `json:"temp_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    *int64 `json:"expires_in"`
}

// tokenExpiry uses JWT payloads only as an untrusted expiry bound. It is never
// signature verification or authentication; the service must call Profile using
// the returned token before creating or authorizing a local session.
func tokenExpiry(data authData, now time.Time) (time.Time, error) {
	var expiry time.Time
	if data.ExpiresIn != nil {
		if *data.ExpiresIn <= 0 || *data.ExpiresIn > int64((1<<63-1)/time.Second) {
			return time.Time{}, badResponse()
		}
		expiry = now.Add(time.Duration(*data.ExpiresIn) * time.Second)
	}
	parts := strings.Split(data.AccessToken, ".")
	if len(parts) == 3 {
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			var claims struct {
				ExpiresAt int64 `json:"exp"`
			}
			if decodeSingle(payload, &claims) == nil && claims.ExpiresAt > 0 {
				jwtExpiry := time.Unix(claims.ExpiresAt, 0)
				if expiry.IsZero() || jwtExpiry.Before(expiry) {
					expiry = jwtExpiry
				}
			}
		}
	}
	if expiry.IsZero() || !expiry.After(now) {
		return time.Time{}, badResponse()
	}
	return expiry, nil
}

func tokensFrom(data authData, requestedAt time.Time) (ports.Tokens, error) {
	if data.Requires2FA || data.AccessToken == "" || strings.ContainsAny(data.AccessToken, "\r\n") {
		return ports.Tokens{}, badResponse()
	}
	expiry, err := tokenExpiry(data, requestedAt)
	if err != nil {
		return ports.Tokens{}, err
	}
	if !expiry.After(time.Now()) {
		return ports.Tokens{}, badResponse()
	}
	return ports.Tokens{AccessToken: data.AccessToken, RefreshToken: data.RefreshToken, ExpiresAt: expiry}, nil
}

func (c *Client) Login(ctx context.Context, input ports.LoginInput, info ports.ClientInfo) (ports.LoginResult, error) {
	requestedAt := time.Now()
	var data authData
	if err := c.request(ctx, http.MethodPost, "/api/v1/auth/login", "", input, &data, info); err != nil {
		return ports.LoginResult{}, err
	}
	if data.Requires2FA {
		if data.TempToken == "" || data.AccessToken != "" || data.RefreshToken != "" {
			return ports.LoginResult{}, badResponse()
		}
		return ports.LoginResult{Challenge: data.TempToken}, nil
	}
	tokens, err := tokensFrom(data, requestedAt)
	return ports.LoginResult{Tokens: tokens}, err
}

func (c *Client) Verify2FA(ctx context.Context, challenge, code string, info ports.ClientInfo) (ports.Tokens, error) {
	requestedAt := time.Now()
	input := struct {
		TempToken string `json:"temp_token"`
		TOTPCode  string `json:"totp_code"`
	}{challenge, code}
	var data authData
	if err := c.request(ctx, http.MethodPost, "/api/v1/auth/login/2fa", "", input, &data, info); err != nil {
		return ports.Tokens{}, err
	}
	return tokensFrom(data, requestedAt)
}

func (c *Client) Refresh(ctx context.Context, refresh string, info ports.ClientInfo) (ports.Tokens, error) {
	requestedAt := time.Now()
	input := struct {
		RefreshToken string `json:"refresh_token"`
	}{refresh}
	var data authData
	if err := c.request(ctx, http.MethodPost, "/api/v1/auth/refresh", "", input, &data, info); err != nil {
		return ports.Tokens{}, err
	}
	return tokensFrom(data, requestedAt)
}

func (c *Client) Logout(ctx context.Context, refresh string, info ports.ClientInfo) error {
	input := struct {
		RefreshToken string `json:"refresh_token"`
	}{refresh}
	return c.request(ctx, http.MethodPost, "/api/v1/auth/logout", "", input, nil, info)
}

// upstreamProfile must not use ports.Profile's json:",string" presentation rule:
// Sub2API returns numeric int64 IDs. Decode only identity/authorization fields.
type upstreamProfile struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

func (c *Client) profile(ctx context.Context, path, access string, info ports.ClientInfo) (ports.Profile, error) {
	if access == "" {
		return ports.Profile{}, &Error{Status: http.StatusUnauthorized, Reason: "UPSTREAM_INVALID_TOKEN"}
	}
	var data upstreamProfile
	if err := c.request(ctx, http.MethodGet, path, access, nil, &data, info); err != nil {
		return ports.Profile{}, err
	}
	if data.ID <= 0 || data.Email == "" || data.Role == "" || data.Status == "" {
		return ports.Profile{}, badResponse()
	}
	return ports.Profile{ID: data.ID, Email: data.Email, Role: data.Role, Status: data.Status}, nil
}

func (c *Client) Profile(ctx context.Context, access string, info ports.ClientInfo) (ports.Profile, error) {
	return c.profile(ctx, "/api/v1/user/profile", access, info)
}
