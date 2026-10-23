package ports

import (
	"context"
	"time"
)

// UsageRecord is the allowlisted upstream read model. It contains no customer,
// API-key, request-body, IP-address or credential fields.
type UsageRecord struct {
	ID         int64     `json:"id"`
	AccountID  int64     `json:"account_id"`
	OccurredAt time.Time `json:"created_at"`
	Model      string    `json:"model"`
	Input      int64     `json:"input_tokens"`
	Output     int64     `json:"output_tokens"`
	CacheRead  int64     `json:"cache_read_tokens"`
	CacheWrite int64     `json:"cache_creation_tokens"`
	Cost       string    `json:"-"`
}
type UsageSourcePage struct {
	Items []UsageRecord
	Total int64
}
type UsageSource interface {
	UsagePage(context.Context, int64, time.Time, time.Time, int) (UsageSourcePage, error)
}
type UsageAccount struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Archived   bool   `json:"archived"`
	UpstreamID int64  `json:"-"`
	SupplierID string `json:"-"`
}
type UsageQuery struct {
	Period, AccountID string
	Page, Size        int
}
type UsageMetrics struct {
	Records    string `json:"record_count"`
	Input      string `json:"input_tokens"`
	Output     string `json:"output_tokens"`
	CacheRead  string `json:"cache_read_tokens"`
	CacheWrite string `json:"cache_creation_tokens"`
	Tokens     string `json:"tokens"`
	Cache      string `json:"cache_tokens"`
	Cost       string `json:"total_cost_usd"`
}
type UsageDay struct {
	Date string `json:"date"`
	UsageMetrics
}
type UsageAccountTotal struct {
	UsageAccount
	UsageMetrics
	SyncState string     `json:"sync_state"`
	SyncedAt  *time.Time `json:"synced_at"`
}
type UsageReport struct {
	Period     string              `json:"period"`
	Timezone   string              `json:"timezone"`
	Start      time.Time           `json:"start"`
	End        time.Time           `json:"end"`
	AccountID  string              `json:"account_id"`
	Summary    UsageMetrics        `json:"summary"`
	Daily      []UsageDay          `json:"daily"`
	Accounts   []UsageAccountTotal `json:"accounts"`
	Options    []UsageAccount      `json:"account_options"`
	Total      int                 `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Pages      int                 `json:"pages"`
	Status     string              `json:"status"`
	ObservedAt time.Time           `json:"observed_at"`
	// The usage API does not cover every failed/retried attempt.
	Attempts *string `json:"request_attempts"`
}
type UsageStore interface {
	UsageReport(context.Context, string, UsageQuery, time.Time, time.Time) (UsageReport, error)
}

// UsageSyncStore persists page checkpoints in the existing per-account/day
// cursors. A lease fences concurrent workers; no customer identity is retained.
type UsageSyncStore interface {
	UsageDiscoveries(context.Context) ([]UsageAccount, error)
	DiscoverUsage(context.Context, UsageAccount, time.Time) error
	RetryUsageDiscovery(context.Context, UsageAccount) error
	ClaimUsagePage(context.Context, string) (UsageJob, bool, error)
	ApplyUsagePage(context.Context, UsageJob, UsageSourcePage) error
	RetryUsagePage(context.Context, UsageJob) error
}
type UsageHistorySource interface {
	UsageEarliest(context.Context, int64) (time.Time, error)
}
type UsageJob struct {
	Account UsageAccount
	Day     time.Time
	Page    int
	Owner   string
	Version int64
}
