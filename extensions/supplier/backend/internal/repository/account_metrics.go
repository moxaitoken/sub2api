package repository

import (
	"context"
	"time"

	"github.com/lib/pq"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

// AccountConsumption reads one page of supplier-owned accounts in one SQL
// snapshot. It reuses official-price usage records, never balances or customer
// rate multipliers. Missing/stale cursors explicitly mean incomplete data.
func (s *Store) AccountConsumption(ctx context.Context, supplier string, ids []string, now time.Time) (map[string]ports.AccountConsumption, error) {
	out := map[string]ports.AccountConsumption{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 100 {
		return nil, ports.ErrConflict
	}
	start, end, _ := usage.Window("today", now)
	rows, err := s.db.QueryContext(ctx, `SELECT a.id::text,
 COALESCE(cost.today_cost,0)::text,COALESCE(cost.total_cost,0)::text,
 (COALESCE(cost.today_rows,0)>0 OR COALESCE(scan.today_observed,false)),
 (COALESCE(cost.total_rows,0)>0 OR COALESCE(scan.any_observed,false)),
 (a.usage_discovered_at IS NOT NULL AND COALESCE(scan.today_complete,false)),
 (a.usage_discovered_at IS NOT NULL AND a.usage_history_start IS NOT NULL
  AND scan.days = ($6::date-a.usage_history_start+1) AND COALESCE(scan.all_complete,false))
 FROM supplier.supplier_accounts a
 LEFT JOIN LATERAL (
  SELECT sum(u.total_cost) FILTER(WHERE u.occurred_at >= $4) AS today_cost,
   sum(u.total_cost) AS total_cost,count(*) FILTER(WHERE u.occurred_at >= $4) AS today_rows,count(*) AS total_rows
  FROM supplier.supplier_usage_records u
  WHERE u.supplier_id=$1 AND u.upstream_instance_id=$2 AND u.account_id=a.id AND u.occurred_at < $5
 ) cost ON true
 LEFT JOIN LATERAL (
  SELECT count(*) AS days,
   bool_or(c.last_scan_at IS NOT NULL) AS any_observed,
   bool_or(c.usage_date=$6::date AND c.last_scan_at IS NOT NULL) AS today_observed,
   bool_or(c.usage_date=$6::date AND c.status='reconciled' AND c.last_scan_at>$7::timestamptz-interval '90 seconds') AS today_complete,
   bool_and(c.status='reconciled' AND c.last_scan_at>$7::timestamptz-CASE WHEN c.usage_date >= $6::date-1 THEN interval '90 seconds' ELSE interval '48 hours' END) AS all_complete
  FROM supplier.supplier_sync_cursors c
  WHERE c.supplier_id=$1 AND c.upstream_instance_id=$2 AND c.account_id=a.id
   AND c.usage_date>=a.usage_history_start AND c.usage_date<=$6::date
 ) scan ON true
 WHERE a.supplier_id=$1 AND a.upstream_instance_id=$2 AND a.id=ANY($3::uuid[])
 AND a.deleted_at IS NULL AND a.upstream_account_id>0`, supplier, s.instanceID, pq.Array(ids), start, end, start.Format("2006-01-02"), now)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, today, total string
		var todayAvailable, totalAvailable, todayComplete, totalComplete bool
		if err := rows.Scan(&id, &today, &total, &todayAvailable, &totalAvailable, &todayComplete, &totalComplete); err != nil {
			return nil, err
		}
		v := ports.AccountConsumption{TodayStatus: "partial", TotalStatus: "partial", ObservedAt: now}
		if todayAvailable {
			v.TodayCost = &today
		}
		if totalAvailable {
			v.TotalCost = &total
		}
		if todayComplete {
			v.TodayStatus = "complete"
		}
		if totalComplete {
			v.TotalStatus = "complete"
		}
		out[id] = v
	}
	return out, rows.Err()
}
