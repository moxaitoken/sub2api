// Package repository persists only the supplier service's own database.
package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/lib/pq"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type Store struct {
	db                    *sql.DB
	instanceID, sourceSHA string
}

var _ ports.Store = (*Store)(nil)

func New(db *sql.DB, instanceID, sourceSHA string) *Store {
	return &Store{db: db, instanceID: instanceID, sourceSHA: sourceSHA}
}

func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) {
		// Do not surface database detail: unique/FK errors may contain another tenant's IDs.
		switch pg.Code {
		case "23505", "23503", "23514", "23502", "22P02":
			return ports.ErrConflict
		}
	}
	return err
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func validHash(v string) bool { b, err := hex.DecodeString(v); return err == nil && len(b) == 32 }
func transaction[T any](ctx context.Context, db *sql.DB, fn func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	value, err := fn(tx)
	if err != nil {
		return zero, mapError(err)
	}
	if err = tx.Commit(); err != nil {
		return zero, mapError(err)
	}
	return value, nil
}

const sessionColumns = `id::text,encode(session_hash,'hex'),upstream_instance_id::text,COALESCE(upstream_user_id,0),COALESCE(supplier_id::text,''),COALESCE(member_id::text,''),kind,encrypted_tokens,key_id,encode(csrf_hash,'hex'),encode(client_hash,'hex'),expires_at,last_seen_at,revoked_at IS NOT NULL,external_pending,version,credential_version,session_epoch`

type scanner interface{ Scan(...any) error }

func scanSession(row scanner) (ports.Session, error) {
	var x ports.Session
	err := row.Scan(&x.ID, &x.Hash, &x.InstanceID, &x.UserID, &x.SupplierID, &x.MemberID, &x.Kind, &x.Cipher, &x.KeyID, &x.CSRFHash, &x.ClientHash, &x.ExpiresAt, &x.LastSeenAt, &x.Revoked, &x.ExternalPending, &x.Version, &x.CredentialVersion, &x.SessionEpoch)
	return x, mapError(err)
}

func (s *Store) CreateSession(ctx context.Context, x ports.Session) error {
	if x.InstanceID != s.instanceID || !validHash(x.Hash) || !validHash(x.CSRFHash) || !validHash(x.ClientHash) || len(x.Cipher) == 0 || x.Revoked || x.ExternalPending || x.KeyID == "" || (x.Kind != "supplier" && x.Kind != "challenge" && x.Kind != "local_supplier" && x.Kind != "admin" && x.Kind != "admin_challenge") {
		return ports.ErrConflict
	}
	_, err := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		if x.Kind == "supplier" {
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT portal_status='active' FROM supplier.suppliers WHERE id=$1 FOR UPDATE`, x.SupplierID).Scan(&active); err != nil {
				return false, err
			}
			if !active {
				return false, ports.ErrForbidden
			}
			if err := tx.QueryRowContext(ctx, `SELECT status='active' AND role='owner' FROM supplier.supplier_members WHERE id=$1 AND supplier_id=$2 AND upstream_instance_id=$3 AND upstream_user_id=$4 FOR UPDATE`, x.MemberID, x.SupplierID, s.instanceID, x.UserID).Scan(&active); err != nil {
				return false, err
			}
			if !active {
				return false, ports.ErrForbidden
			}
		}
		if x.Kind == "local_supplier" {
			var active bool
			if e := tx.QueryRowContext(ctx, `SELECT portal_status='active' AND password_hash IS NOT NULL AND credential_version=$2 AND session_epoch=$3 FROM supplier.suppliers WHERE id=$1 FOR UPDATE`, x.SupplierID, x.CredentialVersion, x.SessionEpoch).Scan(&active); e != nil {
				return false, e
			}
			if !active {
				return false, ports.ErrUnauthenticated
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO supplier.supplier_sessions(id,session_hash,upstream_instance_id,upstream_user_id,supplier_id,member_id,kind,encrypted_tokens,key_id,csrf_hash,client_hash,expires_at,last_seen_at,version,credential_version,session_epoch) VALUES($1,decode($2,'hex'),$3,NULLIF($4::bigint,0),NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,decode($10,'hex'),decode($11,'hex'),$12,$13,1,$14,$15)`, x.ID, x.Hash, x.InstanceID, x.UserID, x.SupplierID, x.MemberID, x.Kind, x.Cipher, x.KeyID, x.CSRFHash, x.ClientHash, x.ExpiresAt, x.LastSeenAt, x.CredentialVersion, x.SessionEpoch)
		if err == nil && x.SupplierID != "" {
			err = s.sessionAudit(ctx, tx, x, "session.create")
		}
		return true, err
	})
	return err
}

func (s *Store) sessionAudit(ctx context.Context, tx *sql.Tx, x ports.Session, action string) error {
	if x.UserID <= 0 {
		if x.Kind != "local_supplier" {
			return nil
		}
		id, e := newID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_kind,actor_id,resource_type,resource_id,action,result,request_id) VALUES($1,$2,'supplier',($2::uuid)::text,'session',$3,$4,'succeeded',$1)`, id, x.SupplierID, x.ID, action)
		return e
	}
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_user_id,resource_type,resource_id,action,result,request_id) VALUES($1,$2,$3,'session',$4,$5,'succeeded',$1)`, id, x.SupplierID, x.UserID, x.ID, action)
	return err
}

func (s *Store) Membership(ctx context.Context, instanceID string, userID int64) (ports.Membership, error) {
	if instanceID != s.instanceID {
		return ports.Membership{}, ports.ErrNotFound
	}
	var x ports.Membership
	err := s.db.QueryRowContext(ctx, `SELECT m.id::text,m.supplier_id::text,m.upstream_user_id,m.role,m.status,m.version,s.id::text,s.name,s.portal_status,s.version FROM supplier.supplier_members m JOIN supplier.suppliers s ON s.id=m.supplier_id WHERE m.upstream_instance_id=$1 AND m.upstream_user_id=$2`, instanceID, userID).Scan(&x.Member.ID, &x.Member.SupplierID, &x.Member.UpstreamUserID, &x.Member.Role, &x.Member.Status, &x.Member.Version, &x.Supplier.ID, &x.Supplier.Name, &x.Supplier.PortalStatus, &x.Supplier.Version)
	return x, mapError(err)
}
