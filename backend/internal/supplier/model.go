// Package supplier implements the fork-local supplier role: users who contribute upstream
// accounts and proxies, see the usage those accounts serve, and are settled on the account
// cost recorded in usage_logs.
//
// The package is deliberately self-contained (own tables, own routes) so that merging
// upstream sub2api only touches the handful of wiring lines that register it.
package supplier

import "time"

// Supplier statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Account review statuses. New supplier accounts start pending and are not schedulable
// until an admin approves them and binds them to groups.
const (
	ReviewPending  = "pending"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

// Settlement statuses. Only drafts can be voided without trace or rebuilt; confirmed and
// paid statements are frozen.
const (
	SettlementDraft     = "draft"
	SettlementConfirmed = "confirmed"
	SettlementPaid      = "paid"
	SettlementVoid      = "void"
)

// Supplier is a user that owns supplier accounts and proxies.
type Supplier struct {
	ID                 int64     `json:"id"`
	UserID             int64     `json:"user_id"`
	UserEmail          string    `json:"user_email,omitempty"`
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	DefaultConcurrency int       `json:"default_concurrency"`
	MaxAccounts        int       `json:"max_accounts"`
	Notes              string    `json:"notes"`
	AccountCount       int64     `json:"account_count"`
	ProxyCount         int64     `json:"proxy_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AccountLink records which supplier owns an account and its review state.
type AccountLink struct {
	AccountID    int64      `json:"account_id"`
	SupplierID   int64      `json:"supplier_id"`
	ReviewStatus string     `json:"review_status"`
	ReviewNote   string     `json:"review_note"`
	ReviewedBy   *int64     `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AccountView is what a supplier may see about an account. Credentials are never exposed.
type AccountView struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	Platform                string     `json:"platform"`
	Type                    string     `json:"type"`
	Email                   string     `json:"email,omitempty"`
	Status                  string     `json:"status"`
	Schedulable             bool       `json:"schedulable"`
	ErrorMessage            string     `json:"error_message,omitempty"`
	ProxyID                 *int64     `json:"proxy_id,omitempty"`
	Concurrency             int        `json:"concurrency"`
	ReviewStatus            string     `json:"review_status"`
	ReviewNote              string     `json:"review_note,omitempty"`
	RateLimitResetAt        *time.Time `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil           *time.Time `json:"overload_until,omitempty"`
	TempUnschedulableUntil  *time.Time `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason string     `json:"temp_unschedulable_reason,omitempty"`
	SessionWindowEnd        *time.Time `json:"session_window_end,omitempty"`
	SessionWindowStatus     string     `json:"session_window_status,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	LastUsedAt              *time.Time `json:"last_used_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

// ProxyView is what a supplier may see about one of its proxies. The password is never
// returned; HasPassword tells the UI whether one is stored.
type ProxyView struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Protocol     string     `json:"protocol"`
	Host         string     `json:"host"`
	Port         int        `json:"port"`
	Username     string     `json:"username,omitempty"`
	HasPassword  bool       `json:"has_password"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	AccountCount int64      `json:"account_count"`
	CreatedAt    time.Time  `json:"created_at"`
}

// UsageTotals aggregates usage_logs rows. Amount is the account cost the supplier is paid
// on: COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1),
// the same expression the upstream admin dashboard uses for account cost.
type UsageTotals struct {
	RequestCount        int64   `json:"request_count"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	Amount              float64 `json:"amount"`
}

// Add accumulates other into t.
func (t *UsageTotals) Add(other UsageTotals) {
	t.RequestCount += other.RequestCount
	t.InputTokens += other.InputTokens
	t.OutputTokens += other.OutputTokens
	t.CacheCreationTokens += other.CacheCreationTokens
	t.CacheReadTokens += other.CacheReadTokens
	t.Amount += other.Amount
}

// UsageRow is one aggregated bucket. Fields that were not part of the grouping are zero.
type UsageRow struct {
	Date        string `json:"date,omitempty"`
	AccountID   int64  `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Model       string `json:"model,omitempty"`
	Family      string `json:"family,omitempty"`
	UsageTotals
}

// UsageSummary is the supplier dashboard payload.
type UsageSummary struct {
	Start    time.Time   `json:"start"`
	End      time.Time   `json:"end"`
	Totals   UsageTotals `json:"totals"`
	ByFamily []UsageRow  `json:"by_family"`
	ByModel  []UsageRow  `json:"by_model"`
}

// UsageQuery filters usage aggregation. AccountIDs is always the caller's own account set.
type UsageQuery struct {
	AccountIDs []int64
	Start      time.Time
	End        time.Time
	AccountID  int64
	Model      string
}

// Settlement is a statement for one supplier and period.
type Settlement struct {
	ID           int64      `json:"id"`
	SupplierID   int64      `json:"supplier_id"`
	SupplierName string     `json:"supplier_name,omitempty"`
	PeriodStart  time.Time  `json:"period_start"`
	PeriodEnd    time.Time  `json:"period_end"`
	Status       string     `json:"status"`
	Notes        string     `json:"notes"`
	CreatedBy    *int64     `json:"created_by,omitempty"`
	ConfirmedAt  *time.Time `json:"confirmed_at,omitempty"`
	PaidAt       *time.Time `json:"paid_at,omitempty"`
	VoidedAt     *time.Time `json:"voided_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	UsageTotals
	Items []SettlementItem `json:"items,omitempty"`
}

// SettlementItem is one account x model line of a statement.
type SettlementItem struct {
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	Model       string `json:"model"`
	Family      string `json:"family"`
	UsageTotals
}
