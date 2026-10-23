package ports

import "context"

// Supplier-owned admission policy; does not change the upstream group or pricing.
type GroupAccess struct {
	GroupID int64 `json:"group_id"`
	Enabled bool  `json:"enabled"`
	Version int64 `json:"version"`
}
type GroupAccessStore interface {
	GroupAccess(context.Context, int64) (GroupAccess, error)
	EnabledGroupIDs(context.Context) ([]int64, error)
	SetGroupAccess(context.Context, GroupAccess, int64) (GroupAccess, error)
}
