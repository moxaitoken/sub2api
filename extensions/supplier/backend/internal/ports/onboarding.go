package ports

import (
	"context"
	"time"
)

// OwnProxy never serializes authentication material or upstream identifiers.
type OwnProxy struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	Region      string `json:"region"`
	Status      string `json:"status"`
	Capacity    int64  `json:"capacity"`
	Accounts    int64  `json:"accounts"`
	Held        int64  `json:"held"`
	Version     int64  `json:"version"`
	UpstreamID  int64  `json:"-"`
	SupplierID  string `json:"-"`
	ConfigHash  string `json:"-"`
	OperationID string `json:"-"`
}
type ProxyInput struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Region  string `json:"region"`
}
type ProxyDeletionStore interface {
	BeginProxyDelete(context.Context, SupplierRecord, string) (OwnProxy, error)
	FinishProxyDelete(context.Context, OwnProxy) error
}
type ProxyDeletionUpstream interface {
	DeleteProxy(context.Context, int64) error
}
type ProxyConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type RemoteProxy struct {
	ProxyConfig
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Fallback  string `json:"fallback_mode"`
	Backup    *int64 `json:"backup_proxy_id"`
	ExpiresAt *int64 `json:"expires_at"`
}
type AccountGroup struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Status   string  `json:"status"`
	GroupIDs []int64 `json:"group_ids,omitempty"`
}

// AccountSettings are frozen when a supplier starts OAuth onboarding.
// Omitted limits use defaults; an explicit zero disables sessions or RPM.
type AccountSettings struct {
	Concurrency               int      `json:"concurrency"`
	WindowCostLimit           *float64 `json:"window_cost_limit,omitempty"`
	WindowCostStickyReserve   *float64 `json:"window_cost_sticky_reserve,omitempty"`
	MaxSessions               *int     `json:"max_sessions,omitempty"`
	SessionIdleTimeoutMinutes *int     `json:"session_idle_timeout_minutes,omitempty"`
	BaseRPM                   *int     `json:"base_rpm,omitempty"`
	RPMStrategy               string   `json:"rpm_strategy,omitempty"`
	RPMStickyBuffer           *int     `json:"rpm_sticky_buffer,omitempty"`
}

func DefaultAccountSettings() AccountSettings {
	maxSessions, idle, rpm := 100, 5, 100
	return AccountSettings{Concurrency: 20, MaxSessions: &maxSessions, SessionIdleTimeoutMinutes: &idle, BaseRPM: &rpm, RPMStrategy: "tiered"}
}

type BeginOnboarding struct {
	Name     string           `json:"name"`
	ProxyID  string           `json:"proxy_id"`
	GroupID  int64            `json:"group_id,omitempty"` // legacy single-group clients
	GroupIDs []int64          `json:"group_ids,omitempty"`
	Settings *AccountSettings `json:"settings,omitempty"`
}
type BoundOAuth struct {
	OAuthSession
	Proxy                                   OwnProxy
	GroupID                                 int64
	GroupIDs                                []int64
	Settings                                AccountSettings
	CredentialVersion, SessionEpoch         int64
	BeginKey, RequestHash, CreatedAccountID string
}
type OnboardingUpstream interface {
	ReadProxy(context.Context, int64) (RemoteProxy, error)
	CreateProxy(context.Context, string, ProxyConfig, string) (RemoteProxy, error)
	TestProxy(context.Context, int64) (bool, error)
	AccountGroup(context.Context, int64) (AccountGroup, error)
	GenerateBoundOAuth(context.Context, int64) (RemoteOAuth, error)
	ExchangeBoundOAuth(context.Context, string, string, int64) (ClaudeTokens, error)
	CreateBoundAccount(context.Context, string, string, ClaudeTokens, int64, int64, AccountSettings) (RemoteAccount, error)
}
type OnboardingStore interface {
	Proxies(context.Context, string) ([]OwnProxy, error)
	Proxy(context.Context, string, string) (OwnProxy, error)
	ReserveProxy(context.Context, SupplierRecord, OwnProxy, string, string) (OwnProxy, bool, error)
	FinishProxy(context.Context, OwnProxy, int64, string) error
	RetestProxy(context.Context, OwnProxy, bool) error
	LocalAccountOperation(context.Context, string, string) (AccountOperation, error)
	ReserveOnboarding(context.Context, SupplierRecord, BoundOAuth) (BoundOAuth, bool, error)
	PublishOnboarding(context.Context, BoundOAuth) error
	Onboarding(context.Context, string, string) (BoundOAuth, error)
	ClaimOnboarding(context.Context, BoundOAuth, string, string) (AccountOperation, bool, error)
	CheckOnboarding(context.Context, BoundOAuth) error
	ReserveLocalAccount(context.Context, BoundOAuth, string, string, string) (Account, error)
	PrepareOnboarding(context.Context, BoundOAuth, AccountOperation, Account) (Account, error)
	FinishOnboarding(context.Context, BoundOAuth, AccountOperation, Account, string, string) error
	CancelOnboarding(context.Context, string, string) error
}

// Authorization configuration is frozen at creation, including expiry and identity generation.
func (o BoundOAuth) Expired(now time.Time) bool { return !now.Before(o.ExpiresAt) }
