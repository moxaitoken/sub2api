package repository

import (
	"context"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func (s *Store) OverviewAccounts(ctx context.Context, supplier string) ([]ports.OverviewAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,COALESCE(upstream_account_id,0) FROM supplier.supplier_accounts WHERE supplier_id=$1 AND upstream_instance_id=$2 AND deleted_at IS NULL ORDER BY id LIMIT 10001`, supplier, s.instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.OverviewAccount{}
	for rows.Next() {
		var v ports.OverviewAccount
		if err = rows.Scan(&v.ID, &v.UpstreamID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) > 10000 {
		return nil, ports.ErrUnavailable
	}
	return out, rows.Err()
}
