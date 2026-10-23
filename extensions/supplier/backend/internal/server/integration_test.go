//go:build integration

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/testutil"
)

func TestS1HTTPWithPostgres(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated SUPPLIER_TEST_DATABASE_URL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var name string
	if err = db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil || name != "supplier_repository_test" {
		t.Fatal("wrong database")
	}
	if _, err = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS supplier CASCADE"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS supplier CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	instance := "30000000-0000-0000-0000-000000000001"
	store := repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad")
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // Guard against nested session/binding connection acquisition.
	vault, err := auth.NewVault(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	up := testutil.NewUpstream()
	up.ProfileValue.Role = "admin"
	svc := &auth.Service{Store: store, Upstream: up, Vault: vault, InstanceID: instance, KeyID: "fixture-v1"}
	h, err := New(svc, Config{LegacyTestMode: true, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(h, "POST", "/api/supplier/v1/auth/login", `{"email":"fixture@example.invalid","password":"synthetic-fixture"}`, nil, ""); w.Code != 403 {
		t.Fatalf("unbound admin accepted: %d", w.Code)
	}
	// Private fixture setup only, never a runtime provisioning endpoint.
	supplierID, bindingID := "10000000-0000-0000-0000-000000000001", "20000000-0000-0000-0000-000000000001"
	if _, err = db.ExecContext(ctx, `INSERT INTO supplier.suppliers(id,name) VALUES($1,'Persisted tenant')`, supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO supplier.supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES($1,$2,$3,42,'owner')`, bindingID, supplierID, instance); err != nil {
		t.Fatal(err)
	}
	cookie, _ := session(t, h) // An explicitly bound admin is an ordinary supplier.
	w := request(h, "GET", "/api/supplier/v1/me?supplier_id=another", "", cookie, "")
	var result struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(result.Data) != 3 || strings.Contains(w.Body.String(), `"is_ops"`) || !strings.Contains(w.Body.String(), supplierID) {
		t.Fatalf("identity contract: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/supplier/v1/members", "/api/supplier/v1/ops/suppliers"} {
		if w := request(h, "GET", path, "", cookie, ""); w.Code != 404 {
			t.Fatal("removed route", path, w.Code)
		}
	}
	h2, err := New(&auth.Service{Store: repository.New(db, instance, "a0d8370a13243ffdb136a2522a082b4164b8c0ad"), Upstream: up, Vault: vault, InstanceID: instance, KeyID: "fixture-v1"}, Config{LegacyTestMode: true, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	if w = request(h2, "GET", "/api/supplier/v1/me", "", cookie, ""); w.Code != 200 {
		t.Fatalf("restart lost session: %d %s", w.Code, w.Body.String())
	}
	if _, err = db.ExecContext(ctx, `UPDATE supplier.suppliers SET portal_status='disabled' WHERE id=$1`, supplierID); err != nil {
		t.Fatal(err)
	}
	if w = request(h2, "GET", "/api/supplier/v1/me", "", cookie, ""); w.Code != 403 {
		t.Fatalf("disabled tenant retained access: %d", w.Code)
	}
	var revoked bool
	if err = db.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM supplier.supplier_sessions WHERE session_hash=decode($1,'hex')`, auth.Hash(cookie.Value)).Scan(&revoked); err != nil || !revoked {
		t.Fatal("disabled supplier session not revoked", err)
	}
	t.Log("PASS: unbound admin forbidden -> explicit fixture binding -> ordinary supplier login -> cross-service session -> disable denies/revokes; synthetic upstream only")
}
