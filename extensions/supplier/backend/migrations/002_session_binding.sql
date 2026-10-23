ALTER TABLE supplier.supplier_sessions
    ADD COLUMN client_hash bytea NOT NULL CHECK (octet_length(client_hash) = 32);
CREATE UNIQUE INDEX supplier_operations_actor_key
    ON supplier.supplier_operations(upstream_instance_id, actor_user_id, action, idempotency_key);
-- Failed supplier creation has no supplier row to reference. This ledger records
-- stable local failures without manufacturing a tenant or weakening operation FKs.
CREATE TABLE supplier.mutation_requests (
    upstream_instance_id uuid NOT NULL REFERENCES supplier.upstream_instances(id),
    actor_user_id bigint NOT NULL CHECK(actor_user_id > 0),
    action text NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL CHECK(octet_length(request_hash) = 32),
    result jsonb,
    failure text CHECK(failure IN ('not_found','forbidden','conflict')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(upstream_instance_id,actor_user_id,action,idempotency_key),
    CHECK((result IS NULL) <> (failure IS NULL))
);
CREATE INDEX mutation_requests_retention ON supplier.mutation_requests(created_at);
CREATE FUNCTION supplier.reject_audit_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit records are append-only' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER supplier_audit_append_only
    BEFORE UPDATE OR DELETE ON supplier.supplier_audit_events
    FOR EACH ROW EXECUTE FUNCTION supplier.reject_audit_change();
