-- Phase-1 reference schema. Apply ONLY to a disposable isolated database.
-- Not an applied production migration; no cross-database SQL or extension needed.
-- Current single-login model only; actual immutable migrations 001-003 are
-- upgraded by 004, which fails transactionally on incompatible existing data.
BEGIN;
CREATE SCHEMA supplier;
SET LOCAL search_path = supplier, pg_catalog;

CREATE TABLE upstream_instances (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    source_sha text NOT NULL CHECK (source_sha ~ '^[0-9a-f]{40}$'),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE suppliers (
    id uuid PRIMARY KEY,
    name varchar(100) NOT NULL CHECK (length(btrim(name)) > 0),
    portal_status text NOT NULL DEFAULT 'active' CHECK (portal_status IN ('active','disabled')),
    policy jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(policy) = 'object'),
    policy_version bigint NOT NULL DEFAULT 1 CHECK (policy_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE supplier_members (
    id uuid PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    upstream_user_id bigint NOT NULL CHECK (upstream_user_id > 0),
    -- Historical table/column names are internal compatibility, not product roles.
    role text NOT NULL DEFAULT 'owner' CHECK (role = 'owner'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (supplier_id, id),
    UNIQUE (supplier_id),
    UNIQUE (upstream_instance_id, upstream_user_id)
);
CREATE TABLE supplier_sessions (
    id uuid PRIMARY KEY,
    session_hash bytea NOT NULL UNIQUE CHECK (octet_length(session_hash) = 32),
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    upstream_user_id bigint CHECK (upstream_user_id > 0),
    supplier_id uuid REFERENCES suppliers(id) ON DELETE RESTRICT,
    member_id uuid,
    kind text NOT NULL CHECK (kind IN ('challenge','supplier')),
    encrypted_tokens bytea NOT NULL,
    key_id text NOT NULL,
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    FOREIGN KEY (supplier_id, member_id) REFERENCES supplier_members(supplier_id,id) ON DELETE RESTRICT,
    CHECK (kind <> 'supplier' OR (supplier_id IS NOT NULL AND member_id IS NOT NULL AND upstream_user_id IS NOT NULL)),
    CHECK ((supplier_id IS NULL AND member_id IS NULL) OR (supplier_id IS NOT NULL AND member_id IS NOT NULL))
);
CREATE INDEX supplier_sessions_expiry ON supplier_sessions(expires_at) WHERE revoked_at IS NULL;
CREATE TABLE supplier_proxies (
    id uuid PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    upstream_proxy_id bigint CHECK (upstream_proxy_id > 0),
    name varchar(100) NOT NULL,
    status text NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning','active','disabled','deleting','deleted','needs_attention')),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (supplier_id, id),
    UNIQUE (supplier_id, upstream_instance_id, id),
    UNIQUE (upstream_instance_id, upstream_proxy_id)
);
CREATE TABLE supplier_accounts (
    id uuid PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    upstream_account_id bigint CHECK (upstream_account_id > 0),
    identity_fingerprint bytea CHECK (octet_length(identity_fingerprint) = 32),
    identity_key_id text,
    name varchar(100) NOT NULL,
    platform text NOT NULL DEFAULT 'anthropic' CHECK (platform = 'anthropic'),
    account_type text NOT NULL DEFAULT 'oauth' CHECK (account_type = 'oauth'),
    admission_state text NOT NULL DEFAULT 'provisioning' CHECK (admission_state IN ('provisioning','identity_pending','pending','approved','rejected','blocked','deleting','deleted','needs_attention')),
    desired_state text NOT NULL DEFAULT 'paused' CHECK (desired_state IN ('paused','running')),
    observed_state jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(observed_state) = 'object'),
    observed_at timestamptz,
    proxy_id uuid,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (supplier_id, id),
    UNIQUE (supplier_id, upstream_instance_id, id),
    UNIQUE (upstream_instance_id, upstream_account_id),
    UNIQUE (upstream_instance_id, identity_fingerprint),
    FOREIGN KEY (supplier_id, upstream_instance_id, proxy_id) REFERENCES supplier_proxies(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT,
    CHECK ((identity_fingerprint IS NULL) = (identity_key_id IS NULL)),
    CHECK (admission_state <> 'approved' OR (upstream_account_id IS NOT NULL AND identity_fingerprint IS NOT NULL)),
    CHECK (desired_state <> 'running' OR admission_state = 'approved')
);
CREATE INDEX supplier_accounts_list ON supplier_accounts(supplier_id,admission_state,created_at,id);
CREATE TABLE supplier_oauth_sessions (
    id uuid PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    actor_user_id bigint NOT NULL CHECK (actor_user_id > 0),
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    account_id uuid,
    proxy_id uuid,
    purpose text NOT NULL CHECK (purpose IN ('create','reauthorize')),
    upstream_session_cipher bytea NOT NULL,
    key_id text NOT NULL,
    expected_state_hash bytea NOT NULL CHECK (octet_length(expected_state_hash) = 32),
    status text NOT NULL DEFAULT 'created' CHECK (status IN ('created','exchanging','token_ready','account_creating','completed','expired','failed','exchange_unknown','needs_attention')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (supplier_id, upstream_instance_id, id),
    FOREIGN KEY (supplier_id,upstream_instance_id,account_id) REFERENCES supplier_accounts(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (supplier_id,upstream_instance_id,proxy_id) REFERENCES supplier_proxies(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT,
    CHECK ((purpose = 'create' AND account_id IS NULL) OR (purpose = 'reauthorize' AND account_id IS NOT NULL))
);
CREATE INDEX supplier_oauth_expiry ON supplier_oauth_sessions(expires_at) WHERE status = 'created';
CREATE TABLE supplier_operations (
    id uuid PRIMARY KEY,
    parent_operation_id uuid,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    actor_user_id bigint NOT NULL CHECK (actor_user_id > 0),
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    action text NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('account','proxy','oauth')),
    resource_id uuid NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','retry_wait','succeeded','failed','needs_attention')),
    step text NOT NULL DEFAULT 'accepted',
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_owner text,
    lease_until timestamptz,
    next_retry_at timestamptz,
    upstream_idempotency_key text,
    upstream_payload_hash bytea CHECK (octet_length(upstream_payload_hash) = 32),
    first_sent_at timestamptz,
    replay_until timestamptz,
    upstream_result_id bigint CHECK (upstream_result_id > 0),
    safe_result jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(safe_result) = 'object'),
    transient_cipher bytea,
    transient_key_id text,
    transient_expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (supplier_id, id),
    UNIQUE (supplier_id,actor_user_id,action,idempotency_key),
    FOREIGN KEY (supplier_id,parent_operation_id) REFERENCES supplier_operations(supplier_id,id) ON DELETE RESTRICT,
    CHECK ((transient_cipher IS NULL AND transient_key_id IS NULL AND transient_expires_at IS NULL) OR (transient_cipher IS NOT NULL AND transient_key_id IS NOT NULL AND transient_expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX supplier_one_inflight_resource ON supplier_operations(upstream_instance_id,resource_type,resource_id)
    WHERE state IN ('pending','running','retry_wait','needs_attention');
CREATE INDEX supplier_operations_due ON supplier_operations(next_retry_at,lease_until) WHERE state IN ('pending','running','retry_wait');
CREATE TABLE supplier_usage_records (
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    upstream_usage_id bigint NOT NULL CHECK (upstream_usage_id > 0),
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    account_name_snapshot varchar(100) NOT NULL,
    requested_model text NOT NULL,
    upstream_model text,
    input_tokens bigint NOT NULL CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL CHECK (output_tokens >= 0),
    cache_read_tokens bigint NOT NULL CHECK (cache_read_tokens >= 0),
    cache_creation_tokens bigint NOT NULL CHECK (cache_creation_tokens >= 0),
    total_cost numeric(38,18) NOT NULL CHECK (total_cost >= 0),
    account_stats_cost numeric(38,18) CHECK (account_stats_cost >= 0),
    account_rate_multiplier numeric(38,18) CHECK (account_rate_multiplier >= 0),
    account_cost numeric(38,18) GENERATED ALWAYS AS
        (COALESCE(account_stats_cost,total_cost) * COALESCE(account_rate_multiplier,1)) STORED,
    observed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (upstream_instance_id, upstream_usage_id),
    FOREIGN KEY (supplier_id,upstream_instance_id,account_id) REFERENCES supplier_accounts(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT
);
CREATE INDEX supplier_usage_time ON supplier_usage_records(supplier_id,occurred_at DESC,upstream_usage_id DESC,upstream_instance_id DESC);
CREATE INDEX supplier_usage_account_time ON supplier_usage_records(supplier_id,account_id,occurred_at DESC,upstream_usage_id DESC);
CREATE INDEX supplier_usage_model_time ON supplier_usage_records(supplier_id,requested_model,occurred_at DESC,upstream_usage_id DESC);
CREATE TABLE supplier_sync_cursors (
    upstream_instance_id uuid NOT NULL REFERENCES upstream_instances(id) ON DELETE RESTRICT,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    account_id uuid NOT NULL,
    usage_date date NOT NULL,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    page integer NOT NULL DEFAULT 1 CHECK (page > 0),
    status text NOT NULL DEFAULT 'backfilling' CHECK (status IN ('backfilling','live','reconciled','lagging','gap','unknown','retry_wait')),
    observed_count bigint CHECK (observed_count >= 0),
    scan_fingerprint bytea,
    last_scan_at timestamptz,
    last_reconciled_at timestamptz,
    gap_reason text,
    lease_owner text,
    lease_until timestamptz,
    next_retry_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (account_id,usage_date),
    FOREIGN KEY (supplier_id,upstream_instance_id,account_id) REFERENCES supplier_accounts(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT
);
CREATE INDEX supplier_sync_due ON supplier_sync_cursors(next_retry_at,lease_until);
CREATE TABLE supplier_audit_events (
    id uuid PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    actor_user_id bigint NOT NULL CHECK (actor_user_id > 0),
    operation_id uuid,
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    action text NOT NULL,
    result text NOT NULL,
    safe_summary jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(safe_summary) = 'object'),
    request_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (supplier_id,operation_id) REFERENCES supplier_operations(supplier_id,id) ON DELETE RESTRICT
);
CREATE INDEX supplier_audit_time ON supplier_audit_events(supplier_id,occurred_at DESC,id DESC);
-- Runtime code must enforce remaining workflow, tenant and actor rules.
-- These DDL constraints do not authorize callers or make cross-service writes atomic.
-- Audit INSERT-only grants, runtime DML grants and immutable-ownership checks must
-- be installed by the eventual migration/provisioning mechanism, not by this sample.
COMMIT;
