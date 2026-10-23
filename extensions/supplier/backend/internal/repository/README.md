# Supplier PostgreSQL adapter

Current production composition uses R2 local supplier credentials and an independent
upstream-admin session, not a supplier-membership gate for administrators.
`identity.go` persists provisioning, status, reset and audit atomically; local session
creation checks fresh credential/epoch values under the supplier row lock.

006 extends existing suppliers/sessions/audit and adds zero balances. 001-005 remain
immutable. Old local supplier sessions are revoked at cutover; historical mappings,
resources and audit remain. Passwords/hashes are never returned in managed DTOs.

The following session/legacy constraints describe preserved infrastructure and tests;
legacy supplier membership is not the active R2 login path. Old account mutation
methods await proxy/actor integration and are not registered in the R2 HTTP server.

`New(db, instanceID, sourceSHA) *Store` implements the session and supplier-identity
binding port. The caller owns the pool; this package never accesses the upstream
database or reads credentials. Use a database separate from Sub2API.

- `Migrate(ctx)` is explicit, advisory-locked, checksummed and atomic. HTTP startup
  only calls read-only `CheckMigrations(ctx)`. The primary `sub2api` and PostgreSQL
  maintenance databases are refused as additional guardrails.
- Historical migrations `001`–`003` are immutable. They retain the initial tables,
  browser binding, append-only audit and durable external-consumption barrier.
  Their old member/operation columns and mutation ledger are historical storage,
  not currently exposed product capabilities.
- `004_single_supplier_identity.sql` keeps one login identity per supplier, with
  the historical role fixed to `owner`. It refuses pre-existing multiple bindings
  or non-owner roles without deleting/reassigning data. Such data needs an explicit
  reviewed migration decision. Historical ops sessions are retained but revoked;
  the runtime cannot create or authorize them again.
- No member-management, generic operations/provisioning APIs or multi-role logic
  remain. The narrow account-review action checks current upstream admin status;
  this is not a supplier-provisioning bypass. Every user
  needs an existing active binding; binding initialization belongs to private
  deployment data, not a public API or new CLI. Missing bindings fail closed.
- Binding reads are fresh, not cached. Session creation locks supplier/binding
  rows and checks eligibility. Every authenticated request revalidates both the
  binding and supplier, and detects identity/tenant changes.
- `WithSession` uses a dedicated connection and session-level advisory lock with
  row locks. `Checkpoint()` commits the refresh/2FA-consumption barrier before an
  external call and re-reads the row while retaining the advisory lock. Later
  failure/cancellation cannot silently retry an already-consumed external token.
  Known refresh results survive temporary profile failures. Cleanup discards a
  physical connection if releasing its advisory lock fails.
- Callbacks cannot alter identity or extend hard expiry. Load the binding only
  after releasing the session transaction to support a one-connection pool.
- Ciphertext, browser hashes and raw tokens never enter safe audit summaries.
  Runtime grants should be limited to required DML and audit INSERT/SELECT, not
  audit UPDATE/DELETE. Historical append-only triggers are defense in depth.
- `005_account_oauth.sql` adds the OAuth display name and actor-operation index.
  Account mutations use durable idempotency, version CAS, unique identity reservations
  and an in-flight resource fence. Unknown external results require manual review;
  the runtime does not automatically replay an authorization code or account creation.

## Tests

Unit checks: `go test ./...`, `go vet ./...`, `go build ./cmd/server` from this module.

```sh
SUPPLIER_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:5432/supplier_repository_test?sslmode=disable' \
  go test -tags=integration ./internal/repository ./internal/server ./internal/accounts -count=1 -p=1 -v
```

Only the exact disposable database `supplier_repository_test` on loopback is
accepted. These tests **drop/recreate its supplier schema** and use direct fixture
SQL, never production provisioning APIs. Run a private PostgreSQL 18 container
with `--network none`, tmpfs data and no published ports; a Go runner can share its
network namespace. Tests cover unchanged session barriers, one-login constraints,
fresh tenant isolation, removed-route 404s and compatible/refused forward upgrades.
No real credentials, production database or Claude calls are used.
