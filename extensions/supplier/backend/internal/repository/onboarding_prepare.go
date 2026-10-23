package repository

import (
	"context"
	"database/sql"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func (s *Store) PrepareOnboarding(ctx context.Context, o ports.BoundOAuth, op ports.AccountOperation, a ports.Account) (ports.Account, error) {
	return transaction(ctx, s.db, func(tx *sql.Tx) (ports.Account, error) {
		if a.UpstreamID <= 0 {
			return a, ports.ErrConflict
		}
		e := tx.QueryRowContext(ctx, `UPDATE supplier.supplier_accounts SET upstream_account_id=$1,admission_state='approved',desired_state='paused',updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3 AND upstream_instance_id=$4 AND version=$5 RETURNING version`, a.UpstreamID, a.ID, o.SupplierID, s.instanceID, a.Version).Scan(&a.Version)
		if e != nil {
			return a, e
		}
		res, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_operations SET step='account_enable_started',safe_result=jsonb_build_object('account_id',$1::text),updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3 AND upstream_instance_id=$4 AND state='running'`, a.ID, op.ID, o.SupplierID, s.instanceID)
		if e != nil {
			return a, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return a, ports.ErrConflict
		}
		return a, nil
	})
}
