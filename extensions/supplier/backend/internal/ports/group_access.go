package ports

import "context"

// Supplier-owned admission policy; does not change the upstream group or pricing.
type GroupAccess struct {
	GroupID           int64  `json:"group_id"`
	Enabled           bool   `json:"enabled"`
	Version           int64  `json:"version"`
	SupplierGroupName string `json:"supplier_group_name,omitempty"`
}
type GroupAccessStore interface {
	GroupAccess(context.Context, int64) (GroupAccess, error)
	EnabledGroupIDs(context.Context) ([]int64, error)
	SetGroupAccess(context.Context, GroupAccess, int64) (GroupAccess, error)
}

// GroupAccessLister is an optional extension used by grouped onboarding
// options and the main admin table. It preserves compatibility with older
// in-memory stores and test fixtures implementing only GroupAccessStore.
type GroupAccessLister interface {
	ListGroupAccess(context.Context) ([]GroupAccess, error)
	EnabledGroupAccess(context.Context) ([]GroupAccess, error)
}
