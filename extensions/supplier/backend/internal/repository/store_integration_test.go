//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

const testInstance = "10000000-0000-4000-8000-000000000001"
const testSource = "a0d8370a13243ffdb136a2522a082b4164b8c0ad"

var userSequence atomic.Int64

func fixtureID(t *testing.T) string {
	t.Helper()
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func fixtureHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Fixtures initialize only the exact disposable test database. They are not a
// production provisioning API and never infer eligibility from a main-site role.
func fixtureSupplier(t *testing.T, s *Store) (ports.Supplier, ports.Member) {
	t.Helper()
	supplier := ports.Supplier{ID: fixtureID(t), Name: "Fixture Supplier", PortalStatus: "active", Version: 1}
	owner := ports.Member{ID: fixtureID(t), SupplierID: supplier.ID, UpstreamUserID: userSequence.Add(1) + 100, Role: "owner", Status: "active", Version: 1}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO supplier.suppliers(id,name) VALUES($1,$2)`, supplier.ID, supplier.Name); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO supplier.supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES($1,$2,$3,$4,'owner')`, owner.ID, supplier.ID, s.instanceID, owner.UpstreamUserID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return supplier, owner
}
func fixtureSession(t *testing.T, s *Store, supplier ports.Supplier, member ports.Member) ports.Session {
	t.Helper()
	x := ports.Session{ID: fixtureID(t), Hash: fixtureHash(fixtureID(t)), InstanceID: testInstance, UserID: member.UpstreamUserID, SupplierID: supplier.ID, MemberID: member.ID, Kind: "supplier", Cipher: []byte("fixture-ciphertext"), KeyID: "fixture-key", CSRFHash: fixtureHash("fixture-csrf"), ClientHash: fixtureHash("fixture-client"), ExpiresAt: time.Now().UTC().Add(time.Hour), LastSeenAt: time.Now().UTC(), Version: 1}
	if err := s.CreateSession(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestPostgresStore(t *testing.T) {
	raw := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("explicit isolated SUPPLIER_TEST_DATABASE_URL required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.Path != "/supplier_repository_test" {
		t.Fatal("integration tests refuse non-loopback or non-fixture database")
	}
	db, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var name string
	if err = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "supplier_repository_test" {
		t.Fatal("wrong fixture database")
	}
	if _, err = db.ExecContext(ctx, `DROP SCHEMA IF EXISTS supplier CASCADE`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS supplier CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	s := New(db, testInstance, testSource)
	t.Run("explicit migrations and checksum", func(t *testing.T) {
		if err := s.CheckMigrations(ctx); err == nil {
			t.Fatal("unmigrated DB accepted")
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatal("second migration:", err)
		}
		if err := s.CheckMigrations(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supplier.schema_migrations`).Scan(&count); err != nil || count != 10 {
			t.Fatalf("migration count %d: %v", count, err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE supplier.schema_migrations SET checksum=repeat('0',64) WHERE version='002_session_binding.sql'`); err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(ctx); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("tampered migration: %v", err)
		}
		files, _ := migrationFiles()
		if _, err = db.ExecContext(ctx, `UPDATE supplier.schema_migrations SET checksum=$1 WHERE version='002_session_binding.sql'`, files[1].checksum); err != nil {
			t.Fatal(err)
		}
		if err := New(db, testInstance, strings.Repeat("b", 40)).CheckMigrations(ctx); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("wrong source accepted: %v", err)
		}
	})
	t.Run("single identity uniqueness and tenant scoping", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		other, otherOwner := fixtureSupplier(t, s)
		got, err := s.Membership(ctx, testInstance, owner.UpstreamUserID)
		if err != nil || got.Supplier.ID != x.ID {
			t.Fatal("membership", err)
		}
		if _, err := s.Membership(ctx, "10000000-0000-4000-8000-000000000099", owner.UpstreamUserID); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("wrong instance accepted")
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO supplier.supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES($1,$2,$3,$4,'owner')`, fixtureID(t), x.ID, testInstance, userSequence.Add(1)+100); !errors.Is(mapError(err), ports.ErrConflict) {
			t.Fatal("second identity accepted", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE supplier.supplier_members SET role='viewer' WHERE id=$1`, owner.ID); !errors.Is(mapError(err), ports.ErrConflict) {
			t.Fatal("non-owner identity accepted", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE supplier.supplier_members SET upstream_user_id=$1 WHERE id=$2`, owner.UpstreamUserID, otherOwner.ID); !errors.Is(mapError(err), ports.ErrConflict) {
			t.Fatal("one upstream identity claimed by two suppliers", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE supplier.supplier_members SET status='disabled' WHERE id=$1`, owner.ID); err != nil {
			t.Fatal(err)
		}
		fresh, err := s.Membership(ctx, testInstance, owner.UpstreamUserID)
		if err != nil || fresh.Member.Status != "disabled" || fresh.Supplier.ID != x.ID {
			t.Fatal("stale or wrong binding", err)
		}
		otherIdentity, err := s.Membership(ctx, testInstance, otherOwner.UpstreamUserID)
		if err != nil || otherIdentity.Supplier.ID != other.ID || otherIdentity.Member.Status != "active" {
			t.Fatal("other supplier changed", err)
		}
	})
	t.Run("session row lock rolls back callback errors", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		session := fixtureSession(t, s, x, owner)
		sentinel := errors.New("callback failure")
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error { p.Revoked = true; p.Cipher = []byte("discarded"); return sentinel }); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			if p.Revoked || string(p.Cipher) != "fixture-ciphertext" || p.Version != 1 {
				t.Fatal("failed callback persisted")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error { p.UserID++; return nil }); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("session identity mutable: %v", err)
		}
	})
	t.Run("session callbacks serialize across stores", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		session := fixtureSession(t, s, x, owner)
		otherDB, err := sql.Open("postgres", raw)
		if err != nil {
			t.Fatal(err)
		}
		defer otherDB.Close()
		otherStore := New(otherDB, testInstance, testSource)
		entered := make(chan struct{})
		release := make(chan struct{})
		secondEntered := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			errs <- s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
				p.ExternalPending = true
				if err := p.Checkpoint(); err != nil {
					return err
				}
				close(entered)
				<-release
				p.Cipher = []byte("first refresh")
				p.ExternalPending = false
				return nil
			})
		}()
		<-entered
		go func() {
			errs <- otherStore.WithSession(ctx, session.Hash, func(p *ports.Session) error {
				close(secondEntered)
				if string(p.Cipher) != "first refresh" {
					return errors.New("stale refresh ciphertext")
				}
				p.Cipher = []byte("second refresh")
				return nil
			})
		}()
		select {
		case <-secondEntered:
			t.Fatal("concurrent session callback entered before row unlocked")
		case <-time.After(100 * time.Millisecond):
		}
		close(release)
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("checkpoint survives later callback rollback", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		session := fixtureSession(t, s, x, owner)
		sentinel := errors.New("after external callback error")
		err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			p.ExternalPending = true
			if err := p.Checkpoint(); err != nil {
				return err
			}
			p.ExternalPending = false
			p.Cipher = []byte("uncommitted-new-cipher")
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			if !p.ExternalPending || string(p.Cipher) != "fixture-ciphertext" {
				t.Fatal("external barrier rolled back or partial new ciphertext persisted")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("checkpoint survives canceled final commit", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		session := fixtureSession(t, s, x, owner)
		request, cancelRequest := context.WithCancel(ctx)
		defer cancelRequest()
		err := s.WithSession(request, session.Hash, func(p *ports.Session) error {
			p.ExternalPending = true
			if err := p.Checkpoint(); err != nil {
				return err
			}
			p.ExternalPending = false
			p.Cipher = []byte("canceled-new-cipher")
			cancelRequest()
			return nil
		})
		if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
			t.Fatalf("canceled request unexpectedly succeeded: %v", err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			if !p.ExternalPending || string(p.Cipher) != "fixture-ciphertext" {
				t.Fatal("cancel removed external barrier")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("2fa consumed checkpoint survives later failure", func(t *testing.T) {
		session := ports.Session{ID: fixtureID(t), Hash: fixtureHash(fixtureID(t)), InstanceID: testInstance, Kind: "challenge", Cipher: []byte("fixture-challenge-cipher"), KeyID: "fixture-key", CSRFHash: fixtureHash("fixture-csrf"), ClientHash: fixtureHash("fixture-client"), ExpiresAt: time.Now().Add(time.Minute), LastSeenAt: time.Now()}
		if err := s.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("after challenge consumed")
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			p.Revoked = true
			if err := p.Checkpoint(); err != nil {
				return err
			}
			return sentinel
		}); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			if !p.Revoked {
				t.Fatal("challenge was reusable after external failure")
			}
			p.Revoked = false
			return nil
		}); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("challenge resurrected: %v", err)
		}
	})
	t.Run("checkpoint rechecks concurrent session revocation", func(t *testing.T) {
		x, owner := fixtureSupplier(t, s)
		session := fixtureSession(t, s, x, owner)
		done := make(chan error, 1)
		err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			go func() {
				_, err := db.ExecContext(ctx, `UPDATE supplier.supplier_sessions /* checkpoint_revocation_fixture */ SET revoked_at=now(),version=version+1 WHERE id=$1`, session.ID)
				done <- err
			}()
			// Ensure the external update is already waiting for this row before
			// checkpoint releases it, rather than depending on scheduler timing.
			waiting := false
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				var count int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE query LIKE '%checkpoint_revocation_fixture%' AND wait_event_type='Lock'`).Scan(&count); err != nil {
					return err
				}
				if count > 0 {
					waiting = true
					break
				}
				time.Sleep(time.Millisecond)
			}
			if !waiting {
				return errors.New("concurrent revocation never reached row lock")
			}
			p.ExternalPending = true
			if err := p.Checkpoint(); !errors.Is(err, ports.ErrForbidden) {
				return errors.New("checkpoint did not reject concurrently revoked session")
			}
			if !p.Revoked {
				return errors.New("checkpoint discarded concurrent revocation")
			}
			// Returning an error cannot undo the separately committed disable.
			return ports.ErrForbidden
		})
		if !errors.Is(err, ports.ErrForbidden) {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := s.WithSession(ctx, session.Hash, func(p *ports.Session) error {
			if !p.Revoked {
				t.Fatal("concurrent revocation lost")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("disabled supplier or binding cannot create session", func(t *testing.T) {
		for _, table := range []string{"suppliers", "supplier_members"} {
			x, owner := fixtureSupplier(t, s)
			column, id := "portal_status", x.ID
			if table == "supplier_members" {
				column, id = "status", owner.ID
			}
			if _, err := db.ExecContext(ctx, `UPDATE supplier.`+table+` SET `+column+`='disabled' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			session := ports.Session{ID: fixtureID(t), Hash: fixtureHash(fixtureID(t)), InstanceID: testInstance, UserID: owner.UpstreamUserID, SupplierID: x.ID, MemberID: owner.ID, Kind: "supplier", Cipher: []byte("fixture"), KeyID: "fixture", CSRFHash: fixtureHash("csrf"), ClientHash: fixtureHash("client"), ExpiresAt: time.Now().Add(time.Hour), LastSeenAt: time.Now()}
			if err := s.CreateSession(ctx, session); !errors.Is(err, ports.ErrForbidden) {
				t.Fatalf("disabled %s logged in: %v", table, err)
			}
		}
	})
	t.Run("audit append-only no session secrets", func(t *testing.T) {
		var content string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(safe_summary::text,''),'') FROM supplier.supplier_audit_events`).Scan(&content); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"fixture-ciphertext", "fixture-key", "csrf_hash", "client_hash", "encrypted_tokens"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("audit contains secret field %s", forbidden)
			}
		}
		if _, err := db.ExecContext(ctx, `UPDATE supplier.supplier_audit_events SET result='tampered'`); err == nil {
			t.Fatal("audit mutable")
		}
	})
	t.Run("forward migration keeps data or rejects incompatible bindings", func(t *testing.T) {
		for _, scenario := range []string{"one_owner", "multiple_bindings", "non_owner"} {
			t.Run(scenario, func(t *testing.T) {
				if _, err := db.ExecContext(ctx, `DROP SCHEMA supplier CASCADE; CREATE SCHEMA supplier; CREATE TABLE supplier.schema_migrations(version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
					t.Fatal(err)
				}
				files, err := migrationFiles()
				if err != nil {
					t.Fatal(err)
				}
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				for _, file := range files[:3] {
					if _, err = tx.ExecContext(ctx, file.text); err != nil {
						t.Fatal(err)
					}
					if _, err = tx.ExecContext(ctx, `INSERT INTO supplier.schema_migrations(version,checksum) VALUES($1,$2)`, file.name, file.checksum); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO supplier.upstream_instances(id,code,source_sha) VALUES($1::uuid,$1::text,$2)`, testInstance, testSource); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				x, owner := fixtureSupplier(t, s)
				wantRows := 1
				if scenario == "multiple_bindings" {
					if _, err = db.ExecContext(ctx, `INSERT INTO supplier.supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES($1,$2,$3,$4,'owner')`, fixtureID(t), x.ID, testInstance, userSequence.Add(1)+100); err != nil {
						t.Fatal(err)
					}
					wantRows = 2
				}
				if scenario == "non_owner" {
					if _, err = db.ExecContext(ctx, `UPDATE supplier.supplier_members SET role='viewer' WHERE id=$1`, owner.ID); err != nil {
						t.Fatal(err)
					}
				}
				opsID := fixtureID(t)
				if _, err = db.ExecContext(ctx, `INSERT INTO supplier.supplier_sessions(id,session_hash,upstream_instance_id,upstream_user_id,kind,encrypted_tokens,key_id,csrf_hash,client_hash,expires_at) VALUES($1,decode($2,'hex'),$3,900,'ops','fixture','fixture',decode($2,'hex'),decode($2,'hex'),now()+interval '1 hour')`, opsID, fixtureHash(opsID), testInstance); err != nil {
					t.Fatal(err)
				}
				err = s.Migrate(ctx)
				if scenario == "one_owner" && err != nil {
					t.Fatal("compatible upgrade failed", err)
				}
				if scenario != "one_owner" && !errors.Is(err, ports.ErrConflict) {
					t.Fatal("incompatible upgrade not refused", err)
				}
				var count, migrations int
				var revoked bool
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supplier.supplier_members WHERE supplier_id=$1`, x.ID).Scan(&count); err != nil || count != wantRows {
					t.Fatal("migration changed binding rows", count, err)
				}
				if err := db.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM supplier.supplier_sessions WHERE id=$1`, opsID).Scan(&revoked); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supplier.schema_migrations`).Scan(&migrations); err != nil {
					t.Fatal(err)
				}
				if scenario == "one_owner" {
					if !revoked || migrations != 10 {
						t.Fatal("retired ops session or migration state", revoked, migrations)
					}
					if _, err := db.ExecContext(ctx, `UPDATE supplier.supplier_sessions SET revoked_at=NULL WHERE id=$1`, opsID); !errors.Is(mapError(err), ports.ErrConflict) {
						t.Fatal("ops session resurrected", err)
					}
				} else if revoked || migrations != 3 {
					t.Fatal("rejected migration partially committed", revoked, migrations)
				}
			})
		}
	})

}
