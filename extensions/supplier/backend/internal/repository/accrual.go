package repository

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strconv"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// Both usage sync paths call this inside their existing transaction. Lock the
// supplier balance before the source row, matching settlement's financial lock.
func (s *Store) writeUsageRecord(ctx context.Context, tx *sql.Tx, a ports.UsageAccount, r ports.UsageRecord) error {
	if _, e := tx.ExecContext(ctx, `INSERT INTO supplier.supplier_balances(supplier_id) VALUES($1) ON CONFLICT DO NOTHING`, a.SupplierID); e != nil {
		return e
	}
	var before int64
	var remainder string
	if e := tx.QueryRowContext(ctx, `SELECT unsettled_cents,accrual_remainder::text FROM supplier.supplier_balances WHERE supplier_id=$1 FOR UPDATE`, a.SupplierID).Scan(&before, &remainder); e != nil {
		return e
	}
	previous := "0"
	var owner, account string
	var same bool
	e := tx.QueryRowContext(ctx, `SELECT supplier_id::text,account_id::text,total_cost::text,total_cost=$3::numeric FROM supplier.supplier_usage_records WHERE upstream_instance_id=$1 AND upstream_usage_id=$2 FOR UPDATE`, s.instanceID, r.ID, r.Cost).Scan(&owner, &account, &previous, &same)
	exists := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if exists && (owner != a.SupplierID || account != a.ID) {
		return ports.ErrConflict
	}
	var version int64
	e = tx.QueryRowContext(ctx, `INSERT INTO supplier.supplier_usage_records(upstream_instance_id,upstream_usage_id,supplier_id,account_id,occurred_at,account_name_snapshot,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
 ON CONFLICT(upstream_instance_id,upstream_usage_id) DO UPDATE SET occurred_at=EXCLUDED.occurred_at,account_name_snapshot=EXCLUDED.account_name_snapshot,requested_model=EXCLUDED.requested_model,input_tokens=EXCLUDED.input_tokens,output_tokens=EXCLUDED.output_tokens,cache_read_tokens=EXCLUDED.cache_read_tokens,cache_creation_tokens=EXCLUDED.cache_creation_tokens,total_cost=EXCLUDED.total_cost,observed_at=now(),updated_at=now(),version=supplier.supplier_usage_records.version+1
 WHERE supplier.supplier_usage_records.supplier_id=EXCLUDED.supplier_id AND supplier.supplier_usage_records.account_id=EXCLUDED.account_id RETURNING version`, s.instanceID, r.ID, a.SupplierID, a.ID, r.OccurredAt, a.Name, r.Model, r.Input, r.Output, r.CacheRead, r.CacheWrite, r.Cost).Scan(&version)
	if errors.Is(e, sql.ErrNoRows) {
		return ports.ErrConflict
	}
	if e != nil {
		return e
	}
	if exists && same {
		return nil
	}
	// PostgreSQL numeric retains all 18 source decimal places; no float or
	// per-request cent rounding. Negative revisions can consume carried fractions.
	var postedText, afterRemainder string
	if e = tx.QueryRowContext(ctx, `SELECT floor(amount*100)::text,(amount-floor(amount*100)/100)::numeric(38,18)::text FROM (SELECT $1::numeric+$2::numeric-$3::numeric AS amount) n`, remainder, r.Cost, previous).Scan(&postedText, &afterRemainder); e != nil {
		return e
	}
	posted, e := strconv.ParseInt(postedText, 10, 64)
	if e != nil {
		return ports.ErrConflict
	}
	after := new(big.Int).Add(big.NewInt(before), big.NewInt(posted))
	if after.Sign() < 0 || !after.IsInt64() {
		return ports.ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_accrual_events(upstream_instance_id,upstream_usage_id,source_version,supplier_id,account_id,rule_revision,origin,previous_cost,standard_cost,exact_delta,posted_cents,before_cents,after_cents,remainder_before,remainder_after)
 VALUES($1,$2,$3,$4,$5,'official_usd_v1','sync',$6,$7,$7::numeric-$6::numeric,$8,$9,$10,$11,$12)`, s.instanceID, r.ID, version, a.SupplierID, a.ID, previous, r.Cost, posted, before, after.Int64(), remainder, afterRemainder); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_balances SET unsettled_cents=$2,accrual_remainder=$3,version=version+1 WHERE supplier_id=$1`, a.SupplierID, after.Int64(), afterRemainder)
	return e
}
