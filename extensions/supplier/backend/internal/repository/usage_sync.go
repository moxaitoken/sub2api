package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

var _ ports.UsageSyncStore = (*Store)(nil)

func (s *Store) UsageDiscoveries(ctx context.Context) ([]ports.UsageAccount, error) {
	// Midnight does not have to wait for hourly source-history rediscovery.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO supplier.supplier_sync_cursors(upstream_instance_id,supplier_id,account_id,usage_date,next_retry_at)
	 SELECT upstream_instance_id,supplier_id,id,(now() AT TIME ZONE 'Asia/Shanghai')::date,now() FROM supplier.supplier_accounts
	 WHERE upstream_instance_id=$1 AND upstream_account_id>0 AND usage_history_start IS NOT NULL
	 ON CONFLICT(account_id,usage_date) DO NOTHING`, s.instanceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id::text,a.supplier_id::text,a.name,a.upstream_account_id
	 FROM supplier.supplier_accounts a JOIN supplier.upstream_instances i ON i.id=a.upstream_instance_id
	 WHERE a.upstream_instance_id=$1 AND a.upstream_account_id>0 AND i.enabled
	 AND (a.usage_discovery_retry_at IS NULL OR a.usage_discovery_retry_at<=now())
	 ORDER BY a.usage_discovery_retry_at NULLS FIRST,a.id LIMIT 10`, s.instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.UsageAccount{}
	for rows.Next() {
		var a ports.UsageAccount
		if err = rows.Scan(&a.ID, &a.SupplierID, &a.Name, &a.UpstreamID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DiscoverUsage(ctx context.Context, a ports.UsageAccount, first time.Time) error {
	_, err := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		// No source records yet: still schedule today and rediscover later.
		if first.IsZero() {
			first = time.Now()
		}
		var day string
		if err := tx.QueryRowContext(ctx, `SELECT to_char($1::timestamptz AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')`, first).Scan(&day); err != nil {
			return false, err
		}
		var start string
		err := tx.QueryRowContext(ctx, `UPDATE supplier.supplier_accounts SET
		 usage_history_start=LEAST(COALESCE(usage_history_start,$5::date),$5::date,
		 (SELECT min((u.occurred_at AT TIME ZONE 'Asia/Shanghai')::date) FROM supplier.supplier_usage_records u WHERE u.account_id=$1 AND u.supplier_id=$2 AND u.upstream_instance_id=$3)),usage_discovered_at=now(),
		 usage_discovery_retry_at=now()+interval '1 hour'
		 WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND upstream_account_id=$4
		 AND $5::date >= DATE '1970-01-01' AND $5::date <= (now() AT TIME ZONE 'Asia/Shanghai')::date
		 RETURNING usage_history_start::text`, a.ID, a.SupplierID, s.instanceID, a.UpstreamID, day).Scan(&start)
		if err != nil {
			return false, mapError(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_sync_cursors(upstream_instance_id,supplier_id,account_id,usage_date,next_retry_at)
		 SELECT $1,$2,$3,d::date,now() FROM generate_series($4::date,(now() AT TIME ZONE 'Asia/Shanghai')::date,interval '1 day') d
		 ON CONFLICT(account_id,usage_date) DO NOTHING`, s.instanceID, a.SupplierID, a.ID, start)
		return true, err
	})
	return err
}

func (s *Store) RetryUsageDiscovery(ctx context.Context, a ports.UsageAccount) error {
	_, err := s.db.ExecContext(ctx, `UPDATE supplier.supplier_accounts SET usage_discovery_retry_at=now()+interval '30 seconds'
	 WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND upstream_account_id=$4`, a.ID, a.SupplierID, s.instanceID, a.UpstreamID)
	return err
}

func (s *Store) ClaimUsagePage(ctx context.Context, owner string) (ports.UsageJob, bool, error) {
	j, err := transaction(ctx, s.db, func(tx *sql.Tx) (ports.UsageJob, error) {
		var j ports.UsageJob
		var date string
		err := tx.QueryRowContext(ctx, `SELECT c.account_id::text,c.supplier_id::text,a.name,a.upstream_account_id,c.usage_date::text,c.page,c.version
		 FROM supplier.supplier_sync_cursors c JOIN supplier.supplier_accounts a ON a.id=c.account_id AND a.supplier_id=c.supplier_id AND a.upstream_instance_id=c.upstream_instance_id
		 JOIN supplier.upstream_instances i ON i.id=c.upstream_instance_id
		 WHERE c.upstream_instance_id=$1 AND i.enabled AND a.upstream_account_id>0
		 AND (c.lease_until IS NULL OR c.lease_until<now()) AND (c.next_retry_at IS NULL OR c.next_retry_at<=now())
		 ORDER BY c.next_retry_at NULLS FIRST,c.usage_date DESC,c.account_id FOR UPDATE OF c SKIP LOCKED LIMIT 1`, s.instanceID).
			Scan(&j.Account.ID, &j.Account.SupplierID, &j.Account.Name, &j.Account.UpstreamID, &date, &j.Page, &j.Version)
		if err != nil {
			return j, err
		}
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return j, err
		}
		j.Day, err = time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return j, err
		}
		j.Owner = owner
		err = tx.QueryRowContext(ctx, `UPDATE supplier.supplier_sync_cursors SET lease_owner=$2,lease_until=now()+interval '60 seconds',status='backfilling',version=version+1
		 WHERE account_id=$1 AND usage_date=$3::date RETURNING version`, j.Account.ID, owner, date).Scan(&j.Version)
		return j, err
	})
	if err == sql.ErrNoRows || err == ports.ErrNotFound {
		return j, false, nil
	}
	return j, err == nil, err
}

// Canonical, chained digest can resume across processes without retaining raw
// customer DTOs or an unbounded in-memory day's records. Stable ID order matters.
func usageFingerprint(previous []byte, r ports.UsageRecord) []byte {
	v := struct {
		ID, AccountID              int64
		Time, Model, Cost          string
		Input, Output, Read, Write int64
	}{r.ID, r.AccountID, r.OccurredAt.UTC().Format(time.RFC3339Nano), r.Model, r.Cost, r.Input, r.Output, r.CacheRead, r.CacheWrite}
	b, _ := json.Marshal(v)
	h := sha256.New()
	h.Write(previous)
	h.Write(b)
	return h.Sum(nil)
}

func (s *Store) ApplyUsagePage(ctx context.Context, j ports.UsageJob, p ports.UsageSourcePage) error {
	_, err := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		var pass, page int
		var total sql.NullInt64
		var count, last int64
		var fingerprint, verify []byte
		date := j.Day.Format("2006-01-02")
		err := tx.QueryRowContext(ctx, `SELECT scan_pass,page,scan_total,scan_count,scan_last_id,scan_fingerprint,verify_fingerprint
		 FROM supplier.supplier_sync_cursors WHERE account_id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND usage_date=$4::date
		 AND lease_owner=$5 AND version=$6 AND lease_until>now() FOR UPDATE`, j.Account.ID, j.Account.SupplierID, s.instanceID, date, j.Owner, j.Version).
			Scan(&pass, &page, &total, &count, &last, &fingerprint, &verify)
		if err != nil {
			return false, mapError(err)
		}
		// Lock the actual ownership row; a cursor cannot authorize reassignment.
		var upstream int64
		err = tx.QueryRowContext(ctx, `SELECT upstream_account_id FROM supplier.supplier_accounts WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 FOR SHARE`, j.Account.ID, j.Account.SupplierID, s.instanceID).Scan(&upstream)
		if err != nil {
			return false, mapError(err)
		}
		if upstream != j.Account.UpstreamID || page != j.Page || p.Total < 0 || len(p.Items) > 100 {
			return false, ports.ErrConflict
		}
		restart := func() (bool, error) {
			_, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_sync_cursors SET page=1,scan_pass=1,scan_total=NULL,scan_count=0,scan_last_id=0,scan_fingerprint=NULL,verify_fingerprint=NULL,
			 status='retry_wait',gap_reason='source_changed',lease_owner=NULL,lease_until=NULL,next_retry_at=now()+interval '5 seconds',version=version+1 WHERE account_id=$1 AND usage_date=$2::date`, j.Account.ID, date)
			return true, e
		}
		// Freeze a finite prefix so newly appended usage cannot starve a busy
		// account's backfill. Both passes verify that same prefix. A deletion or
		// shifted offset is detected by ID order and the second-pass digest.
		target := p.Total
		if total.Valid {
			target = total.Int64
		}
		if p.Total < target {
			return restart()
		}
		if len(p.Items) == 0 && count != target {
			return restart()
		}
		for _, r := range p.Items {
			if count == target {
				break
			}
			if r.ID <= last {
				return restart()
			}
			if r.AccountID != upstream || r.OccurredAt.Before(j.Day) || !r.OccurredAt.Before(j.Day.AddDate(0, 0, 1)) || !usageDecimal.MatchString(r.Cost) || r.Model == "" || r.Input < 0 || r.Output < 0 || r.CacheRead < 0 || r.CacheWrite < 0 {
				return false, ports.ErrConflict
			}
			if err = s.writeUsageRecord(ctx, tx, j.Account, r); err != nil {
				return false, err
			}
			fingerprint = usageFingerprint(fingerprint, r)
			last = r.ID
			count++
		}
		if count > target {
			return restart()
		}
		if count < target {
			_, err = tx.ExecContext(ctx, `UPDATE supplier.supplier_sync_cursors SET page=page+1,scan_total=$3,scan_count=$4,scan_last_id=$5,scan_fingerprint=$6,
			 retry_count=0,lease_owner=NULL,lease_until=NULL,next_retry_at=now(),version=version+1 WHERE account_id=$1 AND usage_date=$2::date`, j.Account.ID, date, target, count, last, fingerprint)
			return true, err
		}
		if pass == 1 {
			_, err = tx.ExecContext(ctx, `UPDATE supplier.supplier_sync_cursors SET page=1,scan_pass=2,scan_total=$4,scan_count=0,scan_last_id=0,scan_fingerprint=NULL,verify_fingerprint=$3,
			 retry_count=0,lease_owner=NULL,lease_until=NULL,next_retry_at=now(),version=version+1 WHERE account_id=$1 AND usage_date=$2::date`, j.Account.ID, date, fingerprint, target)
			return true, err
		}
		if !bytes.Equal(fingerprint, verify) {
			return restart()
		}
		// Reconcile every ID, timestamp, token component and decimal cost with
		// the module projection. Extra retained rows flag a source retention gap.
		rows, err := tx.QueryContext(ctx, `SELECT upstream_usage_id,occurred_at,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost::text
		 FROM supplier.supplier_usage_records WHERE account_id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND occurred_at >= $4 AND occurred_at < $5 ORDER BY upstream_usage_id`, j.Account.ID, j.Account.SupplierID, s.instanceID, j.Day, j.Day.AddDate(0, 0, 1))
		if err != nil {
			return false, err
		}
		var local []byte
		var localCount int64
		for rows.Next() {
			r := ports.UsageRecord{AccountID: upstream}
			if err = rows.Scan(&r.ID, &r.OccurredAt, &r.Model, &r.Input, &r.Output, &r.CacheRead, &r.CacheWrite, &r.Cost); err != nil {
				rows.Close()
				return false, err
			}
			local = usageFingerprint(local, r)
			localCount++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		state, reason := "reconciled", ""
		if p.Total > target {
			state, reason = "live", "source_appended"
		}
		if localCount != count || !bytes.Equal(local, fingerprint) {
			state, reason = "gap", "source_projection_mismatch"
		}
		_, err = tx.ExecContext(ctx, `UPDATE supplier.supplier_sync_cursors SET status=$3,gap_reason=NULLIF($4,''),observed_count=$5,last_scan_at=now(),
		 last_reconciled_at=CASE WHEN $3='reconciled' THEN now() ELSE last_reconciled_at END,
		 page=1,scan_pass=1,scan_total=NULL,scan_count=0,scan_last_id=0,scan_fingerprint=NULL,verify_fingerprint=NULL,retry_count=0,
		 lease_owner=NULL,lease_until=NULL,next_retry_at=now()+CASE WHEN $3='gap' THEN interval '1 minute' WHEN $3='live' THEN interval '5 seconds'
		 WHEN usage_date >= (now() AT TIME ZONE 'Asia/Shanghai')::date-1 THEN interval '30 seconds' ELSE interval '6 hours' END,
		 generation=generation+1,version=version+1,updated_at=now() WHERE account_id=$1 AND usage_date=$2::date`, j.Account.ID, date, state, reason, count)
		return true, err
	})
	return err
}

func (s *Store) RetryUsagePage(ctx context.Context, j ports.UsageJob) error {
	_, err := s.db.ExecContext(ctx, `UPDATE supplier.supplier_sync_cursors SET status='retry_wait',gap_reason='source_unavailable',retry_count=retry_count+1,
	 next_retry_at=now()+make_interval(secs=>LEAST(900,5*power(2,LEAST(retry_count,8)))::double precision),lease_owner=NULL,lease_until=NULL,version=version+1
	 WHERE account_id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND usage_date=$4::date AND lease_owner=$5 AND version=$6`, j.Account.ID, j.Account.SupplierID, s.instanceID, j.Day.Format("2006-01-02"), j.Owner, j.Version)
	return err
}
