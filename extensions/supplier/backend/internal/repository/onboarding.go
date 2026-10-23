package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

var _ ports.OnboardingStore = (*Store)(nil)

func (s *Store) BeginProxyDelete(ctx context.Context, r ports.SupplierRecord, id string) (ports.OwnProxy, error) {
	return transaction(ctx, s.db, func(tx *sql.Tx) (ports.OwnProxy, error) {
		var p ports.OwnProxy
		if e := localGeneration(ctx, tx, r); e != nil {
			return p, e
		}
		p, e := scanProxy(tx.QueryRowContext(ctx, `SELECT `+proxyColumns+` FROM supplier.supplier_proxies p WHERE p.id=$1 AND p.supplier_id=$2 AND p.upstream_instance_id=$3 AND p.deleted_at IS NULL FOR UPDATE`, id, r.Supplier.ID, s.instanceID))
		if e != nil {
			return p, e
		}
		if p.Accounts != 0 || p.Held != 0 || p.UpstreamID <= 0 || (p.Status != "active" && p.Status != "disabled" && p.Status != "needs_attention" && p.Status != "deleting") {
			return p, ports.ErrConflict
		}
		if p.Status == "deleting" {
			return p, nil
		}
		_, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_proxies SET status='deleting',version=version+1,updated_at=now() WHERE id=$1 AND supplier_id=$2`, p.ID, p.SupplierID)
		p.Status = "deleting"
		p.Version++
		return p, e
	})
}

func (s *Store) FinishProxyDelete(ctx context.Context, p ports.OwnProxy) error {
	_, e := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_proxies SET status='deleted',deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND version=$4 AND status='deleting' AND deleted_at IS NULL`, p.ID, p.SupplierID, s.instanceID, p.Version)
		if e != nil {
			return false, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return false, e
		}
		if n == 0 {
			var deleted bool
			e = tx.QueryRowContext(ctx, `SELECT status='deleted' AND deleted_at IS NOT NULL FROM supplier.supplier_proxies WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3`, p.ID, p.SupplierID, s.instanceID).Scan(&deleted)
			if e != nil || !deleted {
				return false, ports.ErrConflict
			}
			return true, nil
		}
		return true, localAudit(ctx, tx, p.SupplierID, p.OperationID, p.ID, "proxy", "proxy.delete", "succeeded")
	})
	return e
}

const proxyColumns = `p.id::text,p.supplier_id::text,p.name,p.address,p.region,p.status,p.capacity,p.version,COALESCE(p.upstream_proxy_id,0),COALESCE(encode(p.config_hash,'hex'),''),COALESCE(p.create_operation_id::text,''),
 (SELECT count(*) FROM supplier.supplier_accounts a WHERE a.proxy_id=p.id AND a.deleted_at IS NULL),
 (SELECT count(*) FROM supplier.supplier_oauth_sessions o WHERE o.proxy_id=p.id AND o.actor_kind='supplier' AND o.created_account_id IS NULL AND ((o.status='created' AND o.expires_at>now()) OR o.status IN ('exchanging','account_creating','needs_attention','exchange_unknown')))`

func scanProxy(row scanner) (ports.OwnProxy, error) {
	var p ports.OwnProxy
	e := row.Scan(&p.ID, &p.SupplierID, &p.Name, &p.Address, &p.Region, &p.Status, &p.Capacity, &p.Version, &p.UpstreamID, &p.ConfigHash, &p.OperationID, &p.Accounts, &p.Held)
	return p, mapError(e)
}
func (s *Store) Proxy(ctx context.Context, supplier, id string) (ports.OwnProxy, error) {
	return scanProxy(s.db.QueryRowContext(ctx, `SELECT `+proxyColumns+` FROM supplier.supplier_proxies p WHERE p.id=$1 AND p.supplier_id=$2 AND p.upstream_instance_id=$3 AND p.deleted_at IS NULL`, id, supplier, s.instanceID))
}
func (s *Store) Proxies(ctx context.Context, supplier string) ([]ports.OwnProxy, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+proxyColumns+` FROM supplier.supplier_proxies p WHERE p.supplier_id=$1 AND p.upstream_instance_id=$2 AND p.deleted_at IS NULL ORDER BY p.created_at DESC,p.id LIMIT 200`, supplier, s.instanceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ports.OwnProxy{}
	for rows.Next() {
		p, e := scanProxy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func localGeneration(ctx context.Context, tx *sql.Tx, r ports.SupplierRecord) error {
	var ok bool
	e := tx.QueryRowContext(ctx, `SELECT portal_status='active' AND login_name IS NOT NULL AND credential_version=$2 AND session_epoch=$3 FROM supplier.suppliers WHERE id=$1 FOR UPDATE`, r.Supplier.ID, r.CredentialVersion, r.SessionEpoch).Scan(&ok)
	if e != nil {
		return mapError(e)
	}
	if !ok {
		return ports.ErrUnauthenticated
	}
	return nil
}
func localAudit(ctx context.Context, tx *sql.Tx, supplier, op, resource, kind, action, result string) error {
	id, e := newID()
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_user_id,actor_kind,actor_id,operation_id,resource_type,resource_id,action,result,request_id) VALUES($1,$2,NULL,'supplier',($2::uuid)::text,$3,$4,$5,$6,$7,$3)`, id, supplier, op, kind, resource, action, result)
	return e
}
func (s *Store) ReserveProxy(ctx context.Context, r ports.SupplierRecord, p ports.OwnProxy, key, hash string) (ports.OwnProxy, bool, error) {
	replay := false
	out, e := transaction(ctx, s.db, func(tx *sql.Tx) (ports.OwnProxy, error) {
		if !validHash(hash) || !validHash(p.ConfigHash) || p.Capacity < 1 || p.Capacity > 1000 {
			return p, ports.ErrConflict
		}
		if e := localGeneration(ctx, tx, r); e != nil {
			return p, e
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, r.Supplier.ID+":proxy:"+key); e != nil {
			return p, e
		}
		var oldID, oldHash string
		e := tx.QueryRowContext(ctx, `SELECT resource_id::text,encode(request_hash,'hex') FROM supplier.supplier_operations WHERE supplier_id=$1 AND upstream_instance_id=$2 AND actor_kind='supplier' AND action='proxy.create' AND idempotency_key=$3`, r.Supplier.ID, s.instanceID, key).Scan(&oldID, &oldHash)
		if e == nil {
			if hash != oldHash {
				return p, ports.ErrConflict
			}
			replay = true
			return scanProxy(tx.QueryRowContext(ctx, `SELECT `+proxyColumns+` FROM supplier.supplier_proxies p WHERE p.id=$1 AND p.supplier_id=$2`, oldID, r.Supplier.ID))
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return p, e
		}
		p.SupplierID = r.Supplier.ID
		p.OperationID, e = newID()
		if e != nil {
			return p, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_operations(id,supplier_id,actor_user_id,actor_kind,actor_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash,state) VALUES($1,$2,NULL,'supplier',($2::uuid)::text,$3,'proxy.create','proxy',$4,$5,decode($6,'hex'),'running')`, p.OperationID, p.SupplierID, s.instanceID, p.ID, key, hash); e != nil {
			return p, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_proxies(id,supplier_id,upstream_instance_id,name,address,region,config_hash,capacity,create_operation_id) VALUES($1,$2,$3,$4,$5,$6,decode($7,'hex'),$8,$9)`, p.ID, p.SupplierID, s.instanceID, p.Name, p.Address, p.Region, p.ConfigHash, p.Capacity, p.OperationID)
		if e != nil {
			return p, e
		}
		p.Status = "provisioning"
		p.Version = 1
		return p, nil
	})
	return out, replay, e
}
func (s *Store) FinishProxy(ctx context.Context, p ports.OwnProxy, remoteID int64, status string) error {
	if status != "active" && status != "disabled" && status != "needs_attention" {
		return ports.ErrConflict
	}
	_, e := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_proxies SET upstream_proxy_id=NULLIF($1::bigint,0),status=$2,version=version+1,updated_at=now() WHERE id=$3 AND supplier_id=$4 AND upstream_instance_id=$5 AND create_operation_id=$6 AND status='provisioning'`, remoteID, status, p.ID, p.SupplierID, s.instanceID, p.OperationID)
		if e != nil {
			return false, e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return false, ports.ErrConflict
		}
		state := "succeeded"
		if status == "needs_attention" {
			state = "needs_attention"
		}
		if status == "disabled" {
			state = "failed"
		}
		_, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state=$1,step=$2,updated_at=now(),version=version+1 WHERE id=$3 AND supplier_id=$4 AND state='running'`, state, status, p.OperationID, p.SupplierID)
		if e != nil {
			return false, e
		}
		return true, localAudit(ctx, tx, p.SupplierID, p.OperationID, p.ID, "proxy", "proxy.create", state)
	})
	return e
}

const oauthLocalColumns = `o.id::text,o.supplier_id::text,o.name,encode(o.expected_state_hash,'hex'),o.key_id,o.status,o.upstream_session_cipher,o.expires_at,o.target_group_id,o.credential_version,o.session_epoch,o.begin_key::text,encode(o.request_hash,'hex'),COALESCE(o.created_account_id::text,''),o.proxy_version,p.id::text,p.name,p.address,p.region,p.status,p.capacity,COALESCE(p.upstream_proxy_id,0),encode(p.config_hash,'hex')`

func scanOnboarding(row scanner) (ports.BoundOAuth, error) {
	var o ports.BoundOAuth
	e := row.Scan(&o.ID, &o.SupplierID, &o.Name, &o.StateHash, &o.KeyID, &o.Status, &o.Cipher, &o.ExpiresAt, &o.GroupID, &o.CredentialVersion, &o.SessionEpoch, &o.BeginKey, &o.RequestHash, &o.CreatedAccountID, &o.Proxy.Version, &o.Proxy.ID, &o.Proxy.Name, &o.Proxy.Address, &o.Proxy.Region, &o.Proxy.Status, &o.Proxy.Capacity, &o.Proxy.UpstreamID, &o.Proxy.ConfigHash)
	o.Proxy.SupplierID = o.SupplierID
	return o, mapError(e)
}
func (s *Store) Onboarding(ctx context.Context, supplier, id string) (ports.BoundOAuth, error) {
	return scanOnboarding(s.db.QueryRowContext(ctx, `SELECT `+oauthLocalColumns+` FROM supplier.supplier_oauth_sessions o JOIN supplier.supplier_proxies p ON p.id=o.proxy_id AND p.supplier_id=o.supplier_id WHERE o.supplier_id=$1 AND o.id=$2 AND o.upstream_instance_id=$3 AND o.actor_kind='supplier'`, supplier, id, s.instanceID))
}
func (s *Store) ReserveOnboarding(ctx context.Context, r ports.SupplierRecord, o ports.BoundOAuth) (ports.BoundOAuth, bool, error) {
	replay := false
	out, e := transaction(ctx, s.db, func(tx *sql.Tx) (ports.BoundOAuth, error) {
		if e := localGeneration(ctx, tx, r); e != nil {
			return o, e
		}
		if !validHash(o.RequestHash) {
			return o, ports.ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, r.Supplier.ID+":oauth:"+o.BeginKey); e != nil {
			return o, e
		}
		old, e := scanOnboarding(tx.QueryRowContext(ctx, `SELECT `+oauthLocalColumns+` FROM supplier.supplier_oauth_sessions o JOIN supplier.supplier_proxies p ON p.id=o.proxy_id WHERE o.supplier_id=$1 AND o.begin_key=$2 AND o.upstream_instance_id=$3 AND o.actor_kind='supplier'`, r.Supplier.ID, o.BeginKey, s.instanceID))
		if e == nil {
			if old.RequestHash != o.RequestHash {
				return o, ports.ErrConflict
			}
			replay = true
			return old, nil
		}
		if !errors.Is(e, ports.ErrNotFound) {
			return o, e
		}
		// Supplier row serializes auto-selection as well as all capacity mutations.
		query := `SELECT ` + proxyColumns + ` FROM supplier.supplier_proxies p WHERE p.supplier_id=$1 AND p.upstream_instance_id=$2 AND p.deleted_at IS NULL AND p.status='active'`
		args := []any{r.Supplier.ID, s.instanceID}
		if o.Proxy.ID != "" {
			query += ` AND p.id=$3`
			args = append(args, o.Proxy.ID)
		}
		query += ` ORDER BY p.id FOR UPDATE`
		rows, e := tx.QueryContext(ctx, query, args...)
		if e != nil {
			return o, e
		}
		var chosen ports.OwnProxy
		for rows.Next() {
			p, e := scanProxy(rows)
			if e != nil {
				rows.Close()
				return o, e
			}
			if chosen.ID == "" && p.UpstreamID > 0 && p.Accounts+p.Held < p.Capacity {
				chosen = p
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return o, e
		}
		if chosen.ID == "" {
			return o, ports.ErrConflict
		}
		o.Proxy = chosen
		o.CredentialVersion = r.CredentialVersion
		o.SessionEpoch = r.SessionEpoch
		o.SupplierID = r.Supplier.ID
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_oauth_sessions(id,supplier_id,actor_user_id,actor_kind,actor_id,upstream_instance_id,purpose,upstream_session_cipher,key_id,expected_state_hash,expires_at,name,proxy_id,proxy_version,target_group_id,credential_version,session_epoch,begin_key,request_hash) VALUES($1,$2,NULL,'supplier',($2::uuid)::text,$3,'create',''::bytea,$4,decode($5,'hex'),$6,$7,$8,$9,$10,$11,$12,$13,decode($14,'hex'))`, o.ID, o.SupplierID, s.instanceID, o.KeyID, o.StateHash, o.ExpiresAt, o.Name, o.Proxy.ID, o.Proxy.Version, o.GroupID, o.CredentialVersion, o.SessionEpoch, o.BeginKey, o.RequestHash)
		return o, e
	})
	return out, replay, e
}
func (s *Store) PublishOnboarding(ctx context.Context, o ports.BoundOAuth) error {
	result, e := s.db.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET upstream_session_cipher=$1,expected_state_hash=decode($2,'hex'),updated_at=now(),version=version+1 WHERE id=$3 AND supplier_id=$4 AND upstream_instance_id=$5 AND actor_kind='supplier' AND status='created' AND expires_at>now() AND octet_length(upstream_session_cipher)=0`, o.Cipher, o.StateHash, o.ID, o.SupplierID, s.instanceID)
	if e != nil {
		return mapError(e)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ports.ErrConflict
	}
	return nil
}
func (s *Store) CheckOnboarding(ctx context.Context, o ports.BoundOAuth) error {
	var ok bool
	e := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier.suppliers s JOIN supplier.supplier_proxies p ON p.supplier_id=s.id WHERE s.id=$1 AND s.portal_status='active' AND s.credential_version=$2 AND s.session_epoch=$3 AND p.id=$4 AND p.version=$5 AND p.status='active' AND p.deleted_at IS NULL AND p.upstream_instance_id=$6)`, o.SupplierID, o.CredentialVersion, o.SessionEpoch, o.Proxy.ID, o.Proxy.Version, s.instanceID).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return ports.ErrForbidden
	}
	return nil
}

const localOperationColumns = `id::text,supplier_id::text,resource_id::text,action,state,step,COALESCE(safe_result->>'account_id',''),idempotency_key::text,encode(request_hash,'hex')`

func scanLocalOperation(row scanner) (ports.AccountOperation, error) {
	var o ports.AccountOperation
	e := row.Scan(&o.ID, &o.SupplierID, &o.ResourceID, &o.Action, &o.State, &o.Step, &o.AccountID, &o.Key, &o.Hash)
	return o, mapError(e)
}
func (s *Store) ClaimOnboarding(ctx context.Context, o ports.BoundOAuth, key, hash string) (ports.AccountOperation, bool, error) {
	replay := false
	out, e := transaction(ctx, s.db, func(tx *sql.Tx) (ports.AccountOperation, error) {
		var zero ports.AccountOperation
		r := ports.SupplierRecord{Supplier: ports.Supplier{ID: o.SupplierID}, CredentialVersion: o.CredentialVersion, SessionEpoch: o.SessionEpoch}
		if e := localGeneration(ctx, tx, r); e != nil {
			return zero, e
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, o.SupplierID+":complete:"+key); e != nil {
			return zero, e
		}
		old, e := scanLocalOperation(tx.QueryRowContext(ctx, `SELECT `+localOperationColumns+` FROM supplier.supplier_operations WHERE supplier_id=$1 AND upstream_instance_id=$2 AND actor_kind='supplier' AND action='oauth.complete' AND idempotency_key=$3`, o.SupplierID, s.instanceID, key))
		if e == nil {
			if old.Hash != hash || old.ResourceID != o.ID {
				return zero, ports.ErrConflict
			}
			replay = true
			return old, nil
		}
		if !errors.Is(e, ports.ErrNotFound) {
			return zero, e
		}
		var proxyID string
		if e = tx.QueryRowContext(ctx, `SELECT id::text FROM supplier.supplier_proxies WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND version=$4 AND status='active' FOR UPDATE`, o.Proxy.ID, o.SupplierID, s.instanceID, o.Proxy.Version).Scan(&proxyID); e != nil {
			return zero, e
		}
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET status='exchanging',consumed_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND actor_kind='supplier' AND status='created' AND expires_at>now() AND octet_length(upstream_session_cipher)>0`, o.ID, o.SupplierID, s.instanceID)
		if e != nil {
			return zero, e
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return zero, ports.ErrConflict
		}
		id, e := newID()
		if e != nil {
			return zero, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_operations(id,supplier_id,actor_user_id,actor_kind,actor_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash,state,step) VALUES($1,$2,NULL,'supplier',($2::uuid)::text,$3,'oauth.complete','oauth',$4,$5,decode($6,'hex'),'running','exchange_started')`, id, o.SupplierID, s.instanceID, o.ID, key, hash)
		return ports.AccountOperation{ID: id, SupplierID: o.SupplierID, ResourceID: o.ID, Action: "oauth.complete", State: "running", Step: "exchange_started", Key: key, Hash: hash}, e
	})
	return out, replay, e
}
func (s *Store) ReserveLocalAccount(ctx context.Context, o ports.BoundOAuth, id, fingerprint, keyID string) (ports.Account, error) {
	return transaction(ctx, s.db, func(tx *sql.Tx) (ports.Account, error) {
		var zero ports.Account
		r := ports.SupplierRecord{Supplier: ports.Supplier{ID: o.SupplierID}, CredentialVersion: o.CredentialVersion, SessionEpoch: o.SessionEpoch}
		if e := localGeneration(ctx, tx, r); e != nil {
			return zero, e
		}
		var proxyID string
		if e := tx.QueryRowContext(ctx, `SELECT id::text FROM supplier.supplier_proxies WHERE id=$1 AND supplier_id=$2 AND status='active' AND version=$3 FOR UPDATE`, o.Proxy.ID, o.SupplierID, o.Proxy.Version).Scan(&proxyID); e != nil {
			return zero, e
		}
		a, e := scanAccount(tx.QueryRowContext(ctx, `INSERT INTO supplier.supplier_accounts(id,supplier_id,upstream_instance_id,name,identity_fingerprint,identity_key_id,proxy_id) VALUES($1,$2,$3,$4,decode($5,'hex'),$6,$7) RETURNING `+accountColumns, id, o.SupplierID, s.instanceID, o.Name, fingerprint, keyID, o.Proxy.ID))
		if e != nil {
			return zero, e
		}
		// Transfer the one reserved slot to the account in the same transaction.
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET created_account_id=$1,status='account_creating',updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3 AND status='exchanging' AND created_account_id IS NULL`, id, o.ID, o.SupplierID)
		if e != nil {
			return zero, e
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return zero, ports.ErrConflict
		}
		return a, nil
	})
}
func (s *Store) FinishOnboarding(ctx context.Context, o ports.BoundOAuth, op ports.AccountOperation, a ports.Account, state, step string) error {
	if state != "succeeded" && state != "failed" && state != "needs_attention" {
		return ports.ErrConflict
	}
	_, e := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		if a.ID != "" {
			result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_accounts SET upstream_account_id=NULLIF($1::bigint,0),admission_state=$2,desired_state=$3,observed_at=now(),updated_at=now(),version=version+1 WHERE id=$4 AND supplier_id=$5 AND upstream_instance_id=$6 AND version=$7`, a.UpstreamID, a.Admission, a.Desired, a.ID, o.SupplierID, s.instanceID, a.Version)
			if e != nil {
				return false, e
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return false, ports.ErrConflict
			}
		}
		safe, _ := json.Marshal(map[string]string{"account_id": a.ID})
		result, e := tx.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state=$1,step=$2,safe_result=$3,updated_at=now(),version=version+1 WHERE id=$4 AND supplier_id=$5 AND upstream_instance_id=$6 AND actor_kind='supplier' AND state='running'`, state, step, string(safe), op.ID, o.SupplierID, s.instanceID)
		if e != nil {
			return false, e
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return false, ports.ErrConflict
		}
		status := state
		if state == "succeeded" {
			status = "completed"
		}
		_, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET status=$1,upstream_session_cipher=''::bytea,updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3 AND actor_kind='supplier'`, status, o.ID, o.SupplierID)
		if e != nil {
			return false, e
		}
		return true, localAudit(ctx, tx, o.SupplierID, op.ID, o.ID, "oauth", "oauth.complete", state)
	})
	return e
}
func (s *Store) CancelOnboarding(ctx context.Context, supplier, id string) error {
	// This never cancels a code exchange or frees a possibly consumed reservation.
	result, e := s.db.ExecContext(ctx, `UPDATE supplier.supplier_oauth_sessions SET status='expired',upstream_session_cipher=''::bytea,updated_at=now(),version=version+1 WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND actor_kind='supplier' AND status='created'`, id, supplier, s.instanceID)
	if e != nil {
		return mapError(e)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (s *Store) LocalAccountOperation(ctx context.Context, supplier, id string) (ports.AccountOperation, error) {
	if _, e := s.db.ExecContext(ctx, `UPDATE supplier.supplier_operations SET state='needs_attention',step='interrupted_requires_check',updated_at=now(),version=version+1 WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND actor_kind='supplier' AND state='running' AND updated_at<now()-interval '60 seconds'`, id, supplier, s.instanceID); e != nil {
		return ports.AccountOperation{}, e
	}
	return scanLocalOperation(s.db.QueryRowContext(ctx, `SELECT `+localOperationColumns+` FROM supplier.supplier_operations WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND actor_kind='supplier' AND action='oauth.complete'`, id, supplier, s.instanceID))
}
func (s *Store) RetestProxy(ctx context.Context, p ports.OwnProxy, valid bool) error {
	status := "disabled"
	if valid {
		status = "active"
	}
	result, e := s.db.ExecContext(ctx, `UPDATE supplier.supplier_proxies SET status=$1,updated_at=now(),version=version+1 WHERE id=$2 AND supplier_id=$3 AND upstream_instance_id=$4 AND version=$5 AND status IN ('active','disabled')`, status, p.ID, p.SupplierID, s.instanceID, p.Version)
	if e != nil {
		return mapError(e)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ports.ErrConflict
	}
	return nil
}
