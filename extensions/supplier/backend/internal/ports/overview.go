package ports

import (
	"context"
	"time"
)

type OverviewAccount struct {
	ID         string
	UpstreamID int64
}
type OverviewStore interface {
	OverviewAccounts(context.Context, string) ([]OverviewAccount, error)
}
type RuntimeAccount struct {
	RPM            *int64  `json:"rpm"`
	Concurrency    *int64  `json:"concurrency"`
	Tokens         *string `json:"tokens"`
	UncachedTokens *string `json:"uncached_tokens"`
}
type RuntimeBatch struct {
	SampledAt   time.Time                `json:"sampled_at"`
	RPMWindow   string                   `json:"rpm_window"`
	TokenWindow int                      `json:"token_window_seconds"`
	Accounts    map[int64]RuntimeAccount `json:"accounts"`
}
type QuotaWindow struct {
	Utilization *float64   `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}
type AccountQuota struct {
	Source    string       `json:"source"`
	UpdatedAt *time.Time   `json:"updated_at"`
	FiveHour  *QuotaWindow `json:"five_hour"`
	SevenDay  *QuotaWindow `json:"seven_day"`
	Fable     *QuotaWindow `json:"seven_day_fable"`
}
type OverviewSource interface {
	RuntimeBatch(context.Context, []int64) (RuntimeBatch, error)
	QuotaBatch(context.Context, []int64) (map[int64]AccountQuota, error)
}
type OverviewMetric struct {
	Value  *string `json:"value"`
	Known  int     `json:"known"`
	Total  int     `json:"total"`
	Status string  `json:"status"`
}
type OverviewQuota struct {
	Maximum   *float64   `json:"maximum"`
	Known     int        `json:"known"`
	Total     int        `json:"total"`
	UpdatedAt *time.Time `json:"updated_at"`
	ResetsAt  *time.Time `json:"resets_at"`
}
type PoolOverview struct {
	ObservedAt     time.Time                `json:"observed_at"`
	RPM            OverviewMetric           `json:"rpm"`
	TPM            OverviewMetric           `json:"tpm"`
	UncachedTPM    OverviewMetric           `json:"uncached_tpm"`
	Concurrency    OverviewMetric           `json:"concurrency"`
	HighQuota      OverviewMetric           `json:"high_quota"`
	Windows        map[string]OverviewQuota `json:"windows"`
	AccountQuotas  map[string]AccountQuota  `json:"account_quotas"`
	QuotaFetchedAt *time.Time               `json:"quota_fetched_at"`
}
