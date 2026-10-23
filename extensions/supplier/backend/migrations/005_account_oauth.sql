-- Forward-only: historical schema and identity bindings are unchanged.
ALTER TABLE supplier.supplier_oauth_sessions
    ADD COLUMN name varchar(100) NOT NULL DEFAULT '';
-- Local metadata only, never an upstream credential or permission source.
CREATE INDEX supplier_operation_actor_list
    ON supplier.supplier_operations(supplier_id,actor_user_id,created_at DESC);
