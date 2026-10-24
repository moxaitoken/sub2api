//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func TestAccountObservationPersistsStateWithConcurrencyAndAudit(t *testing.T) {
	s, owner, a, _ := accrualFixture(t, false)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE supplier.supplier_accounts SET admission_state='needs_attention',identity_fingerprint=decode(repeat('12',32),'hex'),identity_key_id='fixture' WHERE id=$1`, a.ID)
	row, err := s.Account(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	initial := row
	state := ports.AccountObservation{Status: "active", Schedulable: true}
	row, err = s.ObserveAccount(ports.WithAdminActor(ctx, 42), owner.ID, row, state)
	if err != nil || row.Admission != "approved" || row.Desired != "running" || row.Version != initial.Version+1 || row.ObservedAt == nil {
		t.Fatal(row, err)
	}
	stored, err := s.Account(ctx, owner.ID, a.ID)
	if err != nil || stored.Admission != "approved" || stored.Desired != "running" {
		t.Fatal("only the returned DTO changed", stored, err)
	}
	var events int
	var actor string
	if err = s.db.QueryRow(`SELECT count(*),min(actor_kind||':'||actor_id) FROM supplier.supplier_audit_events WHERE action='account.state.sync' AND resource_id=$1`, a.ID).Scan(&events, &actor); err != nil || events != 1 || actor != "admin:42" {
		t.Fatal("sync audit", events, actor, err)
	}
	unchanged, err := s.ObserveAccount(ctx, owner.ID, row, state)
	if err != nil || unchanged.Version != row.Version {
		t.Fatal("identical refresh churned version", unchanged, err)
	}
	if _, err = s.ObserveAccount(ctx, owner.ID, initial, state); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("stale version overwrote result", err)
	}
	foreign, _ := fixtureSupplier(t, s)
	if _, err = s.ObserveAccount(ctx, foreign.ID, row, state); err == nil {
		t.Fatal("foreign supplier updated account")
	}
	state.Schedulable = false
	row, err = s.ObserveAccount(ctx, owner.ID, row, state)
	if err != nil || row.Admission != "approved" || row.Desired != "paused" {
		t.Fatal("pause lost approval", row, err)
	}
	state.Status = "error"
	state.Schedulable = true
	row, err = s.ObserveAccount(ctx, owner.ID, row, state)
	if err != nil || row.Desired != "paused" {
		t.Fatal("error account shown running", row, err)
	}
	// An in-flight account mutation owns the state until it completes.
	op := fixtureID(t)
	exec(`INSERT INTO supplier.supplier_operations(id,supplier_id,upstream_instance_id,actor_user_id,action,resource_type,resource_id,idempotency_key,request_hash,state) VALUES($1,$2,$3,42,'account.pause','account',$4,$5,decode(repeat('34',32),'hex'),'running')`, op, owner.ID, testInstance, a.ID, fixtureID(t))
	state.Status = "active"
	if _, err = s.ObserveAccount(ctx, owner.ID, row, state); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("in-flight mutation overwritten", err)
	}
	exec(`UPDATE supplier.supplier_operations SET state='needs_attention' WHERE id=$1`, op)
	row, err = s.ObserveAccount(ctx, owner.ID, row, state)
	if err != nil || row.Desired != "running" {
		t.Fatal("historical failure prevented confirmed upstream recovery", row, err)
	}
	var outcome string
	if err = s.db.QueryRow(`SELECT state FROM supplier.supplier_operations WHERE id=$1`, op).Scan(&outcome); err != nil || outcome != "needs_attention" {
		t.Fatal("history was rewritten", err)
	}
	// The OAuth path uses an OAuth resource ID, not the account resource ID.
	oauth, oauthOp := fixtureID(t), fixtureID(t)
	exec(`INSERT INTO supplier.supplier_oauth_sessions(id,supplier_id,actor_user_id,upstream_instance_id,purpose,upstream_session_cipher,key_id,expected_state_hash,status,expires_at,created_account_id) VALUES($1,$2,42,$3,'create','fixture'::bytea,'fixture',decode(repeat('56',32),'hex'),'account_creating',now()+interval '1 hour',$4)`, oauth, owner.ID, testInstance, a.ID)
	exec(`INSERT INTO supplier.supplier_operations(id,supplier_id,upstream_instance_id,actor_user_id,action,resource_type,resource_id,idempotency_key,request_hash,state) VALUES($1,$2,$3,42,'oauth.complete','oauth',$4,$5,decode(repeat('78',32),'hex'),'running')`, oauthOp, owner.ID, testInstance, oauth, fixtureID(t))
	if _, err = s.ObserveAccount(ctx, owner.ID, row, state); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("in-flight OAuth operation overwritten", err)
	}
	exec(`UPDATE supplier.supplier_operations SET state='needs_attention' WHERE id=$1`, oauthOp)
	// Two refreshes based on the same version cannot both replace changed state.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ObserveAccount(ctx, owner.ID, row, ports.AccountObservation{Status: "inactive"})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, ports.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("concurrent observation lost update", successes, conflicts)
	}
	row, err = s.Account(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The observation and its audit either commit together or both roll back.
	exec(`CREATE FUNCTION supplier.reject_state_sync() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='account.state.sync' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER reject_state_sync BEFORE INSERT ON supplier.supplier_audit_events FOR EACH ROW EXECUTE FUNCTION supplier.reject_state_sync()`)
	if _, err = s.ObserveAccount(ctx, owner.ID, row, state); err == nil {
		t.Fatal("audit failure was ignored")
	}
	unchanged, err = s.Account(ctx, owner.ID, a.ID)
	if err != nil || unchanged.Version != row.Version || unchanged.Desired != row.Desired {
		t.Fatal("audit failure committed account change", unchanged, err)
	}
	exec(`DROP TRIGGER reject_state_sync ON supplier.supplier_audit_events`)
	exec(`UPDATE supplier.supplier_accounts SET admission_state='provisioning',desired_state='paused',version=version+1 WHERE id=$1`, a.ID)
	row, err = s.Account(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ObserveAccount(ctx, owner.ID, row, state); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("provisioning account was approved", err)
	}
	exec(`UPDATE supplier.supplier_accounts SET admission_state='needs_attention',deleted_at=now(),version=version+1 WHERE id=$1`, a.ID)
	if _, err = s.ObserveAccount(ctx, owner.ID, row, state); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("deleted account resurrected", err)
	}
}
