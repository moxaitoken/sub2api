package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"strings"
)

var _ ports.IdentityStore = (*Store)(nil)

func scanLocal(row scanner) (ports.SupplierRecord, error) {
	var r ports.SupplierRecord
	err := row.Scan(&r.Supplier.ID, &r.Supplier.Name, &r.Supplier.PortalStatus, &r.Supplier.Version, &r.Login, &r.PasswordHash, &r.CredentialVersion, &r.SessionEpoch)
	return r, mapError(err)
}

const localColumns = `id::text,name,portal_status,version,COALESCE(login_name,''),COALESCE(password_hash,''),credential_version,session_epoch`

func (s *Store) LocalCredentials(ctx context.Context, login string) (ports.SupplierRecord, error) {
	return scanLocal(s.db.QueryRowContext(ctx, `SELECT `+localColumns+` FROM supplier.suppliers WHERE login_normalized=$1`, login))
}
func (s *Store) LocalSupplier(ctx context.Context, id string) (ports.SupplierRecord, error) {
	return scanLocal(s.db.QueryRowContext(ctx, `SELECT `+localColumns+` FROM supplier.suppliers WHERE id=$1`, id))
}

const managedColumns = `s.id::text,s.name,s.portal_status,s.version,COALESCE(s.login_name,''),COALESCE(b.unsettled_cents,0),COALESCE(b.settled_cents,0)`

func scanManaged(row scanner) (ports.ManagedSupplier, error) {
	var m ports.ManagedSupplier
	var unsettled, settled int64
	err := row.Scan(&m.ID, &m.Name, &m.PortalStatus, &m.Version, &m.LoginAccount, &unsettled, &settled)
	if err != nil {
		return m, mapError(err)
	}
	m.Unsettled = cents(unsettled)
	m.Settled = cents(settled)
	// Sum as decimal components, without overflowing int64 on two valid balances.
	m.Total = fmt.Sprintf("%d.%02d", unsettled/100+settled/100+(unsettled%100+settled%100)/100, (unsettled%100+settled%100)%100)
	return m, nil
}
func cents(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }
func (s *Store) ManagedSupplier(ctx context.Context, id string) (ports.ManagedSupplier, error) {
	return scanManaged(s.db.QueryRowContext(ctx, `SELECT `+managedColumns+` FROM supplier.suppliers s LEFT JOIN supplier.supplier_balances b ON b.supplier_id=s.id WHERE s.id=$1`, id))
}
func (s *Store) ManagedSuppliers(ctx context.Context, search string, page, size int) ([]ports.ManagedSupplier, int64, error) {
	if page < 1 || page > 100000 || size < 1 || size > 100 || len([]rune(search)) > 100 {
		return nil, 0, ports.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	search = strings.ToLower(strings.TrimSpace(search))
	where := ` WHERE strpos(lower(s.name),$1)>0 OR strpos(COALESCE(s.login_normalized,''),$1)>0`
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM supplier.suppliers s`+where, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+managedColumns+` FROM supplier.suppliers s LEFT JOIN supplier.supplier_balances b ON b.supplier_id=s.id`+where+` ORDER BY s.created_at DESC,s.id DESC LIMIT $2 OFFSET $3`, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	items := []ports.ManagedSupplier{}
	for rows.Next() {
		m, e := scanManaged(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, m)
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
func (s *Store) MutateSupplier(ctx context.Context, in ports.SupplierMutation) (ports.ManagedSupplier, bool, error) {
	replay := false
	result, err := transaction(ctx, s.db, func(tx *sql.Tx) (ports.ManagedSupplier, error) {
		var zero ports.ManagedSupplier
		if in.ActorID <= 0 || !validHash(in.Hash) || (in.Action != "supplier.create" && in.Action != "supplier.status" && in.Action != "supplier.reset") {
			return zero, ports.ErrForbidden
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:%d:%s:%s", s.instanceID, in.ActorID, in.Action, in.Key)); e != nil {
			return zero, e
		}
		var oldHash string
		var safe []byte
		e := tx.QueryRowContext(ctx, `SELECT encode(request_hash,'hex'),safe_result FROM supplier.supplier_operations WHERE upstream_instance_id=$1 AND actor_user_id=$2 AND action=$3 AND idempotency_key=$4`, s.instanceID, in.ActorID, in.Action, in.Key).Scan(&oldHash, &safe)
		if e == nil {
			if oldHash != in.Hash {
				return zero, ports.ErrConflict
			}
			if e = json.Unmarshal(safe, &zero); e != nil {
				return zero, e
			}
			replay = true
			return zero, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
		switch in.Action {
		case "supplier.create":
			if in.PasswordHash == "" {
				return zero, ports.ErrConflict
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.suppliers(id,name,login_name,login_normalized,password_hash) VALUES($1,$2,$3::varchar(64),lower($3::varchar(64)),$4)`, in.ID, in.Name, in.Login, in.PasswordHash); e != nil {
				return zero, e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_balances(supplier_id) VALUES($1)`, in.ID); e != nil {
				return zero, e
			}
		case "supplier.status", "supplier.reset":
			var version int64
			var login string
			if e = tx.QueryRowContext(ctx, `SELECT version,COALESCE(login_name,'') FROM supplier.suppliers WHERE id=$1 FOR UPDATE`, in.ID).Scan(&version, &login); e != nil {
				return zero, e
			}
			if version != in.Version || in.Action == "supplier.reset" && login == "" {
				return zero, ports.ErrConflict
			}
			if in.Action == "supplier.status" {
				if in.Status != "active" && in.Status != "disabled" {
					return zero, ports.ErrConflict
				}
				_, e = tx.ExecContext(ctx, `UPDATE supplier.suppliers SET portal_status=$2,session_epoch=session_epoch+1,version=version+1,updated_at=now() WHERE id=$1`, in.ID, in.Status)
			} else {
				if in.PasswordHash == "" {
					return zero, ports.ErrConflict
				}
				_, e = tx.ExecContext(ctx, `UPDATE supplier.suppliers SET password_hash=$2,credential_version=credential_version+1,session_epoch=session_epoch+1,version=version+1,updated_at=now() WHERE id=$1`, in.ID, in.PasswordHash)
			}
			if e != nil {
				return zero, e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_sessions SET revoked_at=COALESCE(revoked_at,now()),updated_at=now(),version=version+1 WHERE supplier_id=$1 AND revoked_at IS NULL`, in.ID); e != nil {
				return zero, e
			}
		}
		m, e := scanManaged(tx.QueryRowContext(ctx, `SELECT `+managedColumns+` FROM supplier.suppliers s JOIN supplier.supplier_balances b ON b.supplier_id=s.id WHERE s.id=$1`, in.ID))
		if e != nil {
			return zero, e
		}
		operationID, e := newID()
		if e != nil {
			return zero, e
		}
		safe, e = json.Marshal(m)
		if e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash,state,step,safe_result) VALUES($1,$2,$3,$4,$5,'supplier',$2,$6,decode($7,'hex'),'succeeded','committed',$8)`, operationID, in.ID, in.ActorID, s.instanceID, in.Action, in.Key, in.Hash, string(safe)); e != nil {
			return zero, e
		}
		auditID, e := newID()
		if e != nil {
			return zero, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_user_id,actor_kind,actor_id,operation_id,resource_type,resource_id,action,result,request_id) VALUES($1,$2,$3,'admin',($3::bigint)::text,$4,'supplier',$2,$5,'succeeded',$4)`, auditID, in.ID, in.ActorID, operationID, in.Action)
		return m, e
	})
	return result, replay, err
}
