package ports

import (
	"context"
	"time"
)

// Account is a supplier-owned view, never an upstream credential DTO.
type Account struct {
	ID         string          `json:"id"`
	SupplierID string          `json:"-"`
	UpstreamID int64           `json:"-"`
	Name       string          `json:"name"`
	Admission  string          `json:"admission_state"`
	Desired    string          `json:"desired_state"`
	ObservedAt *time.Time      `json:"observed_at"`
	Version    int64           `json:"version"`
	Metrics    *AccountMetrics `json:"metrics,omitempty"`
	StateSync  string          `json:"state_sync,omitempty"`
}

// AccountMetrics is an allowlisted read model, never an upstream account DTO.
// Nil values mean unavailable, not zero. These are config limits and recorded
// official-price costs, not live concurrency or supplier settlement balances.
type AccountMetrics struct {
	Configuration *AccountConfiguration `json:"configuration"`
	Consumption   *AccountConsumption   `json:"consumption"`
}
type AccountConfiguration struct {
	ConcurrencyLimit int `json:"concurrency_limit"`
	Priority         int `json:"priority"`
	// State is read in the same upstream snapshot, never exposed as a config DTO.
	State *AccountObservation `json:"-"`
}
type AccountObservation struct {
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
}
type AccountObservationStore interface {
	ObserveAccount(context.Context, string, Account, AccountObservation) (Account, error)
}
type AccountConsumption struct {
	TodayCost   *string   `json:"today_cost_usd"`
	TotalCost   *string   `json:"total_cost_usd"`
	TodayStatus string    `json:"today_status"`
	TotalStatus string    `json:"total_status"`
	ObservedAt  time.Time `json:"observed_at"`
}
type AccountConfigurationSource interface {
	AccountConfiguration(context.Context, int64) (AccountConfiguration, error)
}
type AccountConsumptionStore interface {
	AccountConsumption(context.Context, string, []string, time.Time) (map[string]AccountConsumption, error)
}

type OAuthSession struct {
	ID, SupplierID, Name, StateHash, KeyID, Status string
	ActorID                                        int64
	Cipher                                         []byte
	ExpiresAt                                      time.Time
}

type AccountOperation struct {
	ID              string `json:"id"`
	SupplierID      string `json:"-"`
	ActorID         int64  `json:"-"`
	ResourceID      string `json:"-"`
	Action          string `json:"action"`
	State           string `json:"state"`
	Step            string `json:"step"`
	AccountID       string `json:"account_id,omitempty"`
	Key, Hash       string `json:"-"`
	ExpectedVersion int64  `json:"-"`
	Reason          string `json:"-"`
}

type AccountStore interface {
	Accounts(context.Context, string, int, int) ([]Account, int64, error)
	Account(context.Context, string, string) (Account, error)
	ReviewAccounts(context.Context, int, int) ([]Account, int64, error)
	ReviewAccount(context.Context, string) (Account, error)
	CheckAccountScope(context.Context, string, int64, bool) error
	SaveOAuth(context.Context, OAuthSession) error
	OAuth(context.Context, string, int64, string) (OAuthSession, error)
	BeginAccountOperation(context.Context, AccountOperation) (AccountOperation, bool, error)
	CheckpointAccountOperation(context.Context, AccountOperation, string, string) error
	ReserveAccount(context.Context, string, OAuthSession, string, string) (Account, error)
	FinishAccount(context.Context, AccountOperation, Account, string, string) error
	AccountOperation(context.Context, string, int64, string) (AccountOperation, error)
}

type RemoteAccount struct {
	ID          int64          `json:"id"`
	ProxyID     *int64         `json:"proxy_id"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Schedulable bool           `json:"schedulable"`
	Status      string         `json:"status"`
	Credentials map[string]any `json:"credentials"`
	Groups      []struct {
		ID int64 `json:"id"`
	} `json:"groups"`
}

type RemoteOAuth struct {
	URL       string `json:"auth_url"`
	SessionID string `json:"session_id"`
}
type ClaudeTokens struct {
	Access      string `json:"access_token"`
	Email       string `json:"email_address"`
	Refresh     string `json:"refresh_token"`
	ExpiresAt   int64  `json:"expires_at"`
	AccountUUID string `json:"account_uuid"`
	OrgUUID     string `json:"org_uuid"`
}

type AccountUpstream interface {
	CheckQuarantine(context.Context, int64) error
	GenerateClaudeOAuth(context.Context) (RemoteOAuth, error)
	ExchangeClaudeOAuth(context.Context, string, string) (ClaudeTokens, error)
	CheckClaudeIdentity(context.Context, string) error
	CreateClaudeAccount(context.Context, string, string, ClaudeTokens, int64) (RemoteAccount, error)
	ReadAccount(context.Context, int64) (RemoteAccount, error)
	SetAccountSchedulable(context.Context, int64, bool) error
	SetAccountGroups(context.Context, int64, []int64) error
}
