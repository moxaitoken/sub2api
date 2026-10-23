package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

var _ ports.AccountStore = (*Store)(nil)

const accountColumns = `id::text,supplier_id::text,COALESCE(upstream_account_id,0),name,admission_state,desired_state,observed_at,version`

func scanAccount(row scanner) (ports.Account, error) {
	var a ports.Account
	err := row.Scan(&a.ID, &a.SupplierID, &a.UpstreamID, &a.Name, &a.Admission, &a.Desired, &a.ObservedAt, &a.Version)
	return a, mapError(err)
}

func (s *Store) Accounts(ctx context.Context, supplier string, page, size int) ([]ports.Account, int64, error) {
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, 0, ports.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM supplier.supplier_accounts WHERE supplier_id=$1 AND upstream_instance_id=$2 AND deleted_at IS NULL`, supplier, s.instanceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+accountColumns+` FROM supplier.supplier_accounts WHERE supplier_id=$1 AND upstream_instance_id=$2 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, supplier, s.instanceID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	items := []ports.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) Account(ctx context.Context, supplier, id string) (ports.Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM supplier.supplier_accounts WHERE supplier_id=$1 AND id=$2 AND upstream_instance_id=$3 AND deleted_at IS NULL`, supplier, id, s.instanceID))
}

func (s *Store) ReviewAccount(ctx context.Context, id string) (ports.Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM supplier.supplier_accounts WHERE id=$1 AND upstream_instance_id=$2 AND deleted_at IS NULL`, id, s.instanceID))
}

func (s *Store) ReviewAccounts(ctx context.Context, page, size int) ([]ports.Account, int64, error) {
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, 0, ports.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM supplier.supplier_accounts WHERE upstream_instance_id=$1 AND admission_state IN ('pending','needs_attention') AND deleted_at IS NULL`, s.instanceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+accountColumns+` FROM supplier.supplier_accounts WHERE upstream_instance_id=$1 AND admission_state IN ('pending','needs_attention') AND deleted_at IS NULL ORDER BY created_at,id LIMIT $2 OFFSET $3`, s.instanceID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	items := []ports.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) CheckAccountScope(ctx context.Context, supplier string, actor int64, review bool) error {
	var eligible bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier.suppliers s WHERE s.id=$1 AND s.portal_status='active') AND EXISTS(SELECT 1 FROM supplier.supplier_members m JOIN supplier.suppliers a ON a.id=m.supplier_id WHERE m.upstream_instance_id=$2 AND m.upstream_user_id=$3 AND m.status='active' AND a.portal_status='active' AND ($4 OR m.supplier_id=$1))`, supplier, s.instanceID, actor, review).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ports.ErrForbidden
	}
	return nil
}

func (s *Store) SaveOAuth(ctx context.Context, o ports.OAuthSession) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO supplier.supplier_oauth_sessions(id,supplier_id,actor_user_id,upstream_instance_id,purpose,upstream_session_cipher,key_id,expected_state_hash,expires_at,name) SELECT $1,$2,$3,$4,'create',$5,$6,decode($7,'hex'),$8,$9 WHERE EXISTS(SELECT 1 FROM supplier.supplier_members m JOIN supplier.suppliers s ON s.id=m.supplier_id WHERE m.supplier_id=$2 AND m.upstream_user_id=$3 AND m.upstream_instance_id=$4 AND m.status='active' AND s.portal_status='active')`, o.ID, o.SupplierID, o.ActorID, s.instanceID, o.Cipher, o.KeyID, o.StateHash, o.ExpiresAt, o.Name)
	if err != nil {
		return mapError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrForbidden
	}
	return nil
}

func (s *Store) OAuth(ctx context.Context, supplier string, actor int64, id string) (ports.OAuthSession, error) {
	var o ports.OAuthSession
	err := s.db.QueryRowContext(ctx, `SELECT id::text,supplier_id::text,actor_user_id,name,encode(expected_state_hash,'hex'),key_id,status,upstream_session_cipher,expires_at FROM supplier.supplier_oauth_sessions WHERE supplier_id=$1 AND actor_user_id=$2 AND id=$3 AND upstream_instance_id=$4`, supplier, actor, id, s.instanceID).Scan(&o.ID, &o.SupplierID, &o.ActorID, &o.Name, &o.StateHash, &o.KeyID, &o.Status, &o.Cipher, &o.ExpiresAt)
	return o, mapError(err)
}

const operationColumns = `id::text,supplier_id::text,actor_user_id,resource_id::text,action,state,step,COALESCE(safe_result->>'account_id',''),idempotency_key::text,encode(request_hash,'hex')`

func scanAccountOperation(row scanner) (ports.AccountOperation, error) {
	var o ports.AccountOperation
	err := row.Scan(&o.ID, &o.SupplierID, &o.ActorID, &o.ResourceID, &o.Action, &o.State, &o.Step, &o.AccountID, &o.Key, &o.Hash)
	return o, mapError(err)
}

func (s *Store) BeginAccountOperation(ctx context.Context, o ports.AccountOperation) (ports.AccountOperation, bool, error) {
	replayed := false
	out, err := transaction(ctx, s.db, func(tx *sql.Tx) (ports.AccountOperation, error) {
		if !validHash(o.Hash) || o.ActorID <= 0 {
			return o, ports.ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.instanceID+":"+o.SupplierID+":"+o.Key); e != nil {
			return o, e
		}
		old, e := scanAccountOperation(tx.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM supplier.supplier_operations WHERE supplier_id=$1 AND actor_user_id=$2 AND action=$3 AND idempotency_key=$4 AND upstream_instance_id=$5`, o.SupplierID, o.ActorID, o.Action, o.Key, s.instanceID))
		if e == nil {
			if old.Hash != o.Hash {
				return o, ports.ErrConflict
			}
			replayed = true
			return old, nil
		}
		if !errors.Is(e, ports.ErrNotFound) {
			return o, e
		}
		var active bool
		if e = tx.QueryRowContext(ctx, `SELECT portal_status='active' FROM supplier.suppliers WHERE id=$1 FOR UPDATE`, o.SupplierID).Scan(&active); e != nil {
			return o, e
		}
		if !active {
			return o, ports.ErrForbidden
		}
		// Approval is a separately authenticated platform-admin action. All other
		// mutations also require the actor's current binding to this supplier.
		if o.Action != "account.approve" {
			if e = tx.QueryRowContext(ctx, `SELECT status='active' FROM supplier.supplier_members WHERE supplier_id=$1 AND upstream_instance_id=$2 AND upstream_user_id=$3 FOR UPDATE`, o.SupplierID, s.instanceID, o.ActorID).Scan(&active); e != nil {
				return o, e
			}
			if !active {
				return o, ports.ErrForbidden
			}
		}
		resourceType := "account"
		if o.Action == "oauth.complete" {
			resourceType = "oauth"
			result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET status='exchanging',consumed_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND supplier_id=$2 AND actor_user_id=$3 AND upstream_instance_id=$4 AND status='created' AND expires_at>now()`, o.ResourceID, o.SupplierID, o.ActorID, s.instanceID)
			if e != nil {
				return o, e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return o, e
			}
			if n != 1 {
				return o, ports.ErrConflict
			}
		} else if o.Action == "account.pause" || o.Action == "account.approve" {
			var version int64
			if e = tx.QueryRowContext(ctx, `SELECT version FROM supplier.supplier_accounts WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND deleted_at IS NULL FOR UPDATE`, o.ResourceID, o.SupplierID, s.instanceID).Scan(&version); e != nil {
				return o, e
			}
			if version != o.ExpectedVersion {
				return o, ports.ErrConflict
			}
		} else {
			return o, ports.ErrForbidden
		}
		o.State = "running"
		o.Step = "accepted"
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash,state,step) VALUES($1,$2,$3,$4,$5,$6,$7,$8,decode($9,'hex'),'running','accepted')`, o.ID, o.SupplierID, o.ActorID, s.instanceID, o.Action, resourceType, o.ResourceID, o.Key, o.Hash)
		return o, e
	})
	return out, replayed, err
}

func (s *Store) CheckpointAccountOperation(ctx context.Context, o ports.AccountOperation, state, step string) error {
	if state != "running" && state != "needs_attention" && state != "failed" {
		return ports.ErrConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state=$1,step=$2,safe_result=jsonb_build_object('account_id',$3::text),updated_at=now(),version=version+1 WHERE id=$4 AND supplier_id=$5 AND upstream_instance_id=$6 AND state='running'`, state, step, o.AccountID, o.ID, o.SupplierID, s.instanceID)
	if err != nil {
		return mapError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (s *Store) ReserveAccount(ctx context.Context, id string, o ports.OAuthSession, fingerprint, keyID string) (ports.Account, error) {
	if !validHash(fingerprint) {
		return ports.Account{}, ports.ErrConflict
	}
	return scanAccount(s.db.QueryRowContext(ctx, `INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,name,identity_fingerprint,identity_key_id) VALUES($1,$2,$3,$4,decode($5,'hex'),$6) RETURNING `+accountColumns, id, o.SupplierID, s.instanceID, o.Name, fingerprint, keyID))
}

func (s *Store) FinishAccount(ctx context.Context, o ports.AccountOperation, a ports.Account, state, step string) error {
	if state != "succeeded" && state != "needs_attention" {
		return ports.ErrConflict
	}
	_, err := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		if a.ID != "" {
			result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_accounts SET upstream_account_id=NULLIF($1::bigint,0),admission_state=$2,desired_state=$3,observed_at=now(),updated_at=now(),version=version+1 WHERE id=$4 AND supplier_id=$5 AND upstream_instance_id=$6 AND version=$7`, a.UpstreamID, a.Admission, a.Desired, a.ID, o.SupplierID, s.instanceID, a.Version)
			if e != nil {
				return false, e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return false, e
			}
			if n != 1 {
				return false, ports.ErrConflict
			}
		}
		safe, _ := json.Marshal(map[string]string{"account_id": a.ID})
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state=$1,step=$2,safe_result=$3,transient_cipher=NULL,transient_key_id=NULL,transient_expires_at=NULL,updated_at=now(),version=version+1 WHERE id=$4 AND supplier_id=$5 AND upstream_instance_id=$6 AND state='running'`, state, step, string(safe), o.ID, o.SupplierID, s.instanceID)
		if e != nil {
			return false, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return false, e
		}
		if n != 1 {
			return false, ports.ErrConflict
		}
		if strings.HasPrefix(o.Action, "oauth.") {
			_, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET status=$1,upstream_session_cipher=''::bytea,updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3`, map[bool]string{true: "completed", false: "needs_attention"}[state == "succeeded"], o.ResourceID, o.SupplierID)
			if e != nil {
				return false, e
			}
		}
		id, e := newID()
		if e != nil {
			return false, e
		}
		resourceType := "account"
		if strings.HasPrefix(o.Action, "oauth.") {
			resourceType = "oauth"
		}
		summary, _ := json.Marshal(map[string]string{"reason": o.Reason, "account_id": a.ID})
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_user_id,operation_id,resource_type,resource_id,action,result,request_id,safe_summary) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$4,$9)`, id, o.SupplierID, o.ActorID, o.ID, resourceType, o.ResourceID, o.Action, state, string(summary))
		return true, e
	})
	return err
}

func (s *Store) AccountOperation(ctx context.Context, supplier string, actor int64, id string) (ports.AccountOperation, error) {
	// HTTP business requests have a 25s deadline. An interrupted process must
	// not leave a caller polling "running" forever, nor automatically retry an
	// externally consumed OAuth code. A stale operation remains an in-flight
	// resource fence until explicitly reconciled by the platform.
	if _, err := s.db.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state='needs_attention',step='interrupted_requires_review',updated_at=now(),version=version+1 WHERE id=$1 AND supplier_id=$2 AND actor_user_id=$3 AND upstream_instance_id=$4 AND state='running' AND updated_at<now()-interval '60 seconds'`, id, supplier, actor, s.instanceID); err != nil {
		return ports.AccountOperation{}, mapError(err)
	}
	return scanAccountOperation(s.db.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM supplier.supplier_operations WHERE id=$1 AND supplier_id=$2 AND actor_user_id=$3 AND upstream_instance_id=$4`, id, supplier, actor, s.instanceID))
}
