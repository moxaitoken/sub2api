package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// ObserveAccount mirrors the main site's state for an already-owned account;
// it never issues upstream approval/scheduling commands or replays OAuth.
func (s *Store) ObserveAccount(ctx context.Context, supplier string, expected ports.Account, state ports.AccountObservation) (ports.Account, error) {
	if expected.SupplierID != supplier || expected.UpstreamID <= 0 || (state.Status != "active" && state.Status != "inactive" && state.Status != "error") {
		return expected, ports.ErrConflict
	}
	return transaction(ctx, s.db, func(tx *sql.Tx) (ports.Account, error) {
		current, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM supplier.supplier_accounts WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND deleted_at IS NULL FOR UPDATE`, expected.ID, supplier, s.instanceID))
		if err != nil {
			return expected, err
		}
		if current.Version != expected.Version || current.UpstreamID != expected.UpstreamID || current.Admission == "provisioning" || current.Admission == "identity_pending" || current.Admission == "deleting" || current.Admission == "deleted" {
			return expected, ports.ErrConflict
		}
		var busy bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier.supplier_operations op
 WHERE op.supplier_id=$1 AND op.upstream_instance_id=$2 AND op.state IN ('pending','running')
 AND ((op.resource_type='account' AND op.resource_id=$3::uuid)
 OR (op.resource_type='oauth' AND op.resource_id IN (SELECT o.id FROM supplier.supplier_oauth_sessions o
 WHERE o.supplier_id=$1 AND o.upstream_instance_id=$2 AND (o.created_account_id=$3::uuid OR o.account_id=$3::uuid)))))`, supplier, s.instanceID, current.ID).Scan(&busy)
		if err != nil {
			return expected, err
		}
		if busy {
			return expected, ports.ErrConflict
		}
		admission, desired := current.Admission, "paused"
		if state.Status == "active" {
			admission = "approved"
			if state.Schedulable {
				desired = "running"
			}
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return expected, err
		}
		var changed bool
		err = tx.QueryRowContext(ctx, `SELECT admission_state<>$1 OR desired_state<>$2 OR observed_state<>$3::jsonb FROM supplier.supplier_accounts WHERE id=$4`, admission, desired, string(encoded), current.ID).Scan(&changed)
		if err != nil {
			return expected, err
		}
		// Refresh observation time without churning the optimistic business version
		// or audit stream on every identical page refresh.
		updated, err := scanAccount(tx.QueryRowContext(ctx, `UPDATE supplier.supplier_accounts SET admission_state=$1,desired_state=$2,observed_state=$3::jsonb,observed_at=now(),updated_at=CASE WHEN $4 THEN now() ELSE updated_at END,version=version+CASE WHEN $4 THEN 1 ELSE 0 END
 WHERE id=$5 AND supplier_id=$6 AND upstream_instance_id=$7 AND version=$8 RETURNING `+accountColumns, admission, desired, string(encoded), changed, current.ID, supplier, s.instanceID, expected.Version))
		if err != nil {
			return expected, err
		}
		if changed {
			// Actor denotes the reader triggering synchronization, not the unknown
			// original administrator who changed the upstream account.
			if err = localAudit(ctx, tx, supplier, "", current.ID, "account", "account.state.sync", "succeeded"); err != nil {
				return expected, err
			}
		}
		return updated, nil
	})
}
