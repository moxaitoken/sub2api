package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// WithSession keeps a PostgreSQL session-level advisory lock on a dedicated
// connection, in addition to row locks. Checkpoint may commit a durable external
// consumption barrier without letting another replica refresh the same session.
// A later callback error rolls back only changes after the last checkpoint.
func (s *Store) WithSession(ctx context.Context, hash string, fn func(*ports.Session) error) error {
	if !validHash(hash) {
		return ports.ErrNotFound
	}
	// Hex case must not select a different advisory lock for the same bytea row.
	hash = strings.ToLower(hash)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	lock := "supplier/session/" + strings.ToLower(s.instanceID) + "/" + hash
	var tx *sql.Tx
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
		// Do not return a pooled connection with a surviving session-level lock.
		// Even acquisition errors may be ambiguous after context cancellation.
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRowContext(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock).Scan(&unlocked); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}()
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lock); err != nil {
		return mapError(err)
	}
	read := func() (ports.Session, error) {
		return scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM supplier.supplier_sessions WHERE session_hash=decode($1,'hex') AND upstream_instance_id=$2 FOR UPDATE`, hash, s.instanceID))
	}
	tx, err = conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	before, err := read()
	if err != nil {
		return mapError(err)
	}
	after := before
	after.Cipher = append([]byte(nil), before.Cipher...)
	var checkpoint func() error
	checkpoint = func() error {
		if tx == nil {
			return ports.ErrUnavailable
		}
		if err := s.saveSession(ctx, tx, before, after); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			tx = nil
			return mapError(err)
		}
		tx = nil
		// Concurrent supplier/member disable can acquire the row after commit.
		// Re-read rather than overwriting its revocation with the callback copy.
		requestedRevoked := after.Revoked
		var err error
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		fresh, err := read()
		if err != nil {
			return err
		}
		before = fresh
		after = fresh
		after.Cipher = append([]byte(nil), fresh.Cipher...)
		after.Checkpoint = checkpoint
		if fresh.Revoked && !requestedRevoked {
			return ports.ErrForbidden
		}
		return nil
	}
	after.Checkpoint = checkpoint
	if err = fn(&after); err != nil {
		return err
	}
	if tx == nil {
		return ports.ErrUnavailable
	}
	if err = s.saveSession(ctx, tx, before, after); err != nil {
		return err
	}
	err = tx.Commit()
	tx = nil
	return mapError(err)
}

func (s *Store) saveSession(ctx context.Context, tx *sql.Tx, before, after ports.Session) error {
	if after.CredentialVersion != before.CredentialVersion || after.SessionEpoch != before.SessionEpoch || after.ID != before.ID || after.Hash != before.Hash || after.InstanceID != before.InstanceID || after.UserID != before.UserID || after.SupplierID != before.SupplierID || after.MemberID != before.MemberID || after.Kind != before.Kind || !after.ExpiresAt.Equal(before.ExpiresAt) || !validHash(after.CSRFHash) || !validHash(after.ClientHash) || (before.Revoked && !after.Revoked) {
		return ports.ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE supplier.supplier_sessions SET encrypted_tokens=$2,key_id=$3,csrf_hash=decode($4,'hex'),client_hash=decode($5,'hex'),last_seen_at=$6,revoked_at=CASE WHEN $7 THEN COALESCE(revoked_at,now()) ELSE NULL END,external_pending=$8,updated_at=now(),version=version+1 WHERE id=$1 AND version=$9`, after.ID, after.Cipher, after.KeyID, after.CSRFHash, after.ClientHash, after.LastSeenAt, after.Revoked, after.ExternalPending, before.Version)
	if err != nil {
		return mapError(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ports.ErrConflict
	}
	if !before.Revoked && after.Revoked && before.SupplierID != "" {
		return s.sessionAudit(ctx, tx, before, "session.revoke")
	}
	return nil
}
