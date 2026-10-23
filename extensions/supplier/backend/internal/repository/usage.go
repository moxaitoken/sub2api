package repository

import (
	"context"
	"database/sql"
	"math/big"
	"regexp"
	"time"

	"github.com/lib/pq"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

var _ ports.UsageStore = (*Store)(nil)
var usageDecimal = regexp.MustCompile(`^[0-9]{1,20}(\.[0-9]{1,18})?$`)

func (s *Store) UsageAccounts(ctx context.Context, supplier string) ([]ports.UsageAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id::text,a.name,a.deleted_at IS NOT NULL OR a.admission_state='deleted',a.upstream_account_id
	 FROM supplier.supplier_accounts a JOIN supplier.suppliers s ON s.id=a.supplier_id
	 WHERE a.supplier_id=$1 AND a.upstream_instance_id=$2 AND a.upstream_account_id>0 AND s.portal_status='active'
	 ORDER BY (SELECT max(c.last_scan_at) FROM supplier.supplier_sync_cursors c WHERE c.account_id=a.id) NULLS FIRST,a.id`, supplier, s.instanceID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := []ports.UsageAccount{}
	for rows.Next() {
		var a ports.UsageAccount
		if err = rows.Scan(&a.ID, &a.Name, &a.Archived, &a.UpstreamID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Only a complete, stable upstream scan is committed. Missing historical source
// rows flag a gap; they never erase previously observed supplier usage.
func (s *Store) SyncUsageWindow(ctx context.Context, supplier string, a ports.UsageAccount, start, end time.Time, fetch func(context.Context) ([]ports.UsageRecord, error)) error {
	_, err := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		var locked bool
		if e := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('supplier-usage'),hashtext($1))`, a.ID).Scan(&locked); e != nil {
			return false, e
		}
		if !locked {
			return false, ports.ErrUnavailable
		}
		var id int64
		if e := tx.QueryRowContext(ctx, `SELECT a.upstream_account_id FROM supplier.supplier_accounts a JOIN supplier.suppliers s ON s.id=a.supplier_id WHERE a.id=$1 AND a.supplier_id=$2 AND a.upstream_instance_id=$3 AND s.portal_status='active' FOR SHARE OF a,s`, a.ID, supplier, s.instanceID).Scan(&id); e != nil {
			return false, e
		}
		if id <= 0 || id != a.UpstreamID {
			return false, ports.ErrForbidden
		}
		a.SupplierID = supplier
		records, e := fetch(ctx)
		if e != nil {
			return false, e
		}
		ids := make([]int64, 0, len(records))
		seen := map[int64]bool{}
		for _, r := range records {
			if !usageDecimal.MatchString(r.Cost) {
				return false, ports.ErrConflict
			}
			cost, ok := new(big.Rat).SetString(r.Cost)
			if !ok || cost.Sign() < 0 || len(r.Cost) > 80 || seen[r.ID] || r.ID <= 0 || r.AccountID != id || r.OccurredAt.Before(start) || !r.OccurredAt.Before(end) || r.Input < 0 || r.Output < 0 || r.CacheRead < 0 || r.CacheWrite < 0 || r.Model == "" {
				return false, ports.ErrConflict
			}
			seen[r.ID] = true
			ids = append(ids, r.ID)
			if e = s.writeUsageRecord(ctx, tx, a, r); e != nil {
				return false, e
			}
		}
		for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
			var count int64
			var gap bool
			e = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(bool_or(NOT(upstream_usage_id=ANY($6::bigint[]))),false) FROM supplier.supplier_usage_records WHERE supplier_id=$1 AND upstream_instance_id=$2 AND account_id=$3 AND occurred_at >= $4 AND occurred_at < $5`, supplier, s.instanceID, a.ID, day, day.AddDate(0, 0, 1), pq.Array(ids)).Scan(&count, &gap)
			if e != nil {
				return false, e
			}
			state := "reconciled"
			if gap {
				state = "gap"
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_sync_cursors(upstream_instance_id,supplier_id,account_id,usage_date,status,observed_count,last_scan_at,last_reconciled_at,gap_reason)
			 VALUES($1,$2,$3,$4,$5,$6,now(),CASE WHEN $5='reconciled' THEN now() END,CASE WHEN $5='gap' THEN 'source_rows_missing' END)
			 ON CONFLICT(account_id,usage_date) DO UPDATE SET status=EXCLUDED.status,observed_count=EXCLUDED.observed_count,last_scan_at=EXCLUDED.last_scan_at,last_reconciled_at=EXCLUDED.last_reconciled_at,gap_reason=EXCLUDED.gap_reason,generation=supplier.supplier_sync_cursors.generation+1,updated_at=now(),version=supplier.supplier_sync_cursors.version+1`, s.instanceID, supplier, a.ID, day.Format("2006-01-02"), state, count)
			if e != nil {
				return false, e
			}
		}
		return true, nil
	})
	return err
}

const usageSums = `count(u.upstream_usage_id)::text,COALESCE(sum(u.input_tokens),0)::text,COALESCE(sum(u.output_tokens),0)::text,COALESCE(sum(u.cache_read_tokens),0)::text,COALESCE(sum(u.cache_creation_tokens),0)::text,COALESCE(sum(u.input_tokens::numeric+u.output_tokens),0)::text,COALESCE(sum(u.cache_read_tokens::numeric+u.cache_creation_tokens),0)::text,COALESCE(sum(u.total_cost),0)::text`

func metricDest(m *ports.UsageMetrics) []any {
	return []any{&m.Records, &m.Input, &m.Output, &m.CacheRead, &m.CacheWrite, &m.Tokens, &m.Cache, &m.Cost}
}
func zeroMetrics() ports.UsageMetrics {
	return ports.UsageMetrics{Records: "0", Input: "0", Output: "0", CacheRead: "0", CacheWrite: "0", Tokens: "0", Cache: "0", Cost: "0"}
}

func (s *Store) UsageReport(ctx context.Context, supplier string, q ports.UsageQuery, start, end time.Time) (ports.UsageReport, error) {
	out := ports.UsageReport{Period: q.Period, Timezone: "Asia/Shanghai", Start: start, End: end, AccountID: q.AccountID, Summary: zeroMetrics(), Daily: []ports.UsageDay{}, Accounts: []ports.UsageAccountTotal{}, Options: []ports.UsageAccount{}, Page: q.Page, PageSize: q.Size, Status: "complete", ObservedAt: time.Now().UTC()}
	if q.Page < 1 || q.Size < 1 || q.Size > 100 {
		return out, ports.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT portal_status='active' FROM supplier.suppliers WHERE id=$1`, supplier).Scan(&active); err != nil {
		return out, mapError(err)
	}
	if !active {
		return out, ports.ErrForbidden
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,name,deleted_at IS NOT NULL OR admission_state='deleted' FROM supplier.supplier_accounts WHERE supplier_id=$1 AND upstream_instance_id=$2 AND upstream_account_id>0 ORDER BY name,id`, supplier, s.instanceID)
	if err != nil {
		return out, err
	}
	owned := q.AccountID == ""
	for rows.Next() {
		var a ports.UsageAccount
		if err = rows.Scan(&a.ID, &a.Name, &a.Archived); err != nil {
			rows.Close()
			return out, err
		}
		out.Options = append(out.Options, a)
		if a.ID == q.AccountID {
			owned = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if !owned {
		return out, ports.ErrNotFound
	}
	if q.Period == "all" {
		// Resolve history bounds inside the SAME snapshot as all aggregates.
		var date sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT LEAST(min(usage_history_start),(SELECT min((u.occurred_at AT TIME ZONE 'Asia/Shanghai')::date)
		 FROM supplier.supplier_usage_records u WHERE u.supplier_id=$1 AND u.upstream_instance_id=$2 AND ($3='' OR u.account_id::text=$3)))::text
		 FROM supplier.supplier_accounts WHERE supplier_id=$1 AND upstream_instance_id=$2 AND upstream_account_id>0 AND ($3='' OR id::text=$3)`, supplier, s.instanceID, q.AccountID).Scan(&date)
		if err != nil {
			return out, err
		}
		if date.Valid {
			start, err = time.ParseInLocation("2006-01-02", date.String, start.Location())
			if err != nil {
				return out, err
			}
			out.Start = start
		}
	}
	args := []any{supplier, s.instanceID, start, end, q.AccountID}
	filter := ` FROM supplier.supplier_usage_records u WHERE u.supplier_id=$1 AND u.upstream_instance_id=$2 AND u.occurred_at >= $3 AND u.occurred_at < $4 AND ($5='' OR u.account_id::text=$5)`
	if err = tx.QueryRowContext(ctx, `SELECT `+usageSums+filter, args...).Scan(metricDest(&out.Summary)...); err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT to_char(u.occurred_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD'),`+usageSums+filter+` GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return out, err
	}
	days := map[string]ports.UsageMetrics{}
	for rows.Next() {
		var d string
		m := zeroMetrics()
		if err = rows.Scan(append([]any{&d}, metricDest(&m)...)...); err != nil {
			rows.Close()
			return out, err
		}
		days[d] = m
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		m, ok := days[date]
		if !ok {
			m = zeroMetrics()
		}
		out.Daily = append(out.Daily, ports.UsageDay{Date: date, UsageMetrics: m})
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.id::text,a.name,a.deleted_at IS NOT NULL OR a.admission_state='deleted',`+usageSums+`,
	 (SELECT CASE WHEN a.usage_discovered_at IS NOT NULL AND count(*)=LEAST($6::integer,$8::date-GREATEST($7::date,COALESCE(a.usage_history_start,$7::date)))
	 AND bool_and(c.status='reconciled' AND c.last_scan_at>now()-CASE WHEN c.usage_date >= (now() AT TIME ZONE 'Asia/Shanghai')::date-1 THEN interval '90 seconds' ELSE interval '48 hours' END)
	 THEN 'complete' ELSE 'partial' END FROM supplier.supplier_sync_cursors c WHERE c.account_id=a.id AND c.supplier_id=$1 AND c.upstream_instance_id=$2 AND c.usage_date >= GREATEST($7::date,COALESCE(a.usage_history_start,$7::date)) AND c.usage_date < $8::date),
	 (SELECT min(c.last_scan_at) FROM supplier.supplier_sync_cursors c WHERE c.account_id=a.id AND c.supplier_id=$1 AND c.upstream_instance_id=$2 AND c.usage_date >= $7::date AND c.usage_date < $8::date)
	 FROM supplier.supplier_accounts a LEFT JOIN supplier.supplier_usage_records u ON u.account_id=a.id AND u.supplier_id=$1 AND u.upstream_instance_id=$2 AND u.occurred_at >= $3 AND u.occurred_at < $4
	 WHERE a.supplier_id=$1 AND a.upstream_instance_id=$2 AND a.upstream_account_id>0 AND ($5='' OR a.id::text=$5)
	 GROUP BY a.id ORDER BY COALESCE(sum(u.total_cost),0) DESC,a.id`, append(args, len(out.Daily), start.Format("2006-01-02"), end.Format("2006-01-02"))...)
	if err != nil {
		return out, err
	}
	all := []ports.UsageAccountTotal{}
	for rows.Next() {
		a := ports.UsageAccountTotal{}
		dest := []any{&a.ID, &a.Name, &a.Archived}
		dest = append(dest, metricDest(&a.UsageMetrics)...)
		dest = append(dest, &a.SyncState, &a.SyncedAt)
		if err = rows.Scan(dest...); err != nil {
			rows.Close()
			return out, err
		}
		if a.SyncState != "complete" {
			out.Status = "partial"
		}
		all = append(all, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Total = len(all)
	out.Pages = (out.Total + q.Size - 1) / q.Size
	offset := (q.Page - 1) * q.Size
	if offset < len(all) {
		limit := offset + q.Size
		if limit > len(all) {
			limit = len(all)
		}
		out.Accounts = all[offset:limit]
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
