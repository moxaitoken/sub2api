-- Keep historical migrations immutable. Existing identity rows are retained,
-- but the portal now supports only one login identity per supplier.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM supplier.supplier_members GROUP BY supplier_id HAVING count(*) > 1)
       OR EXISTS (SELECT 1 FROM supplier.supplier_members WHERE role <> 'owner') THEN
        RAISE EXCEPTION 'single supplier identity requires review of incompatible existing bindings'
            USING ERRCODE = '23514';
    END IF;
END;
$$;
ALTER TABLE supplier.supplier_members
    ADD CONSTRAINT supplier_single_identity UNIQUE (supplier_id),
    ADD CONSTRAINT supplier_identity_owner CHECK (role = 'owner');
-- Preserve historical ops session rows, but revoke their privilege permanently.
UPDATE supplier.supplier_sessions SET revoked_at = COALESCE(revoked_at, now()),
    updated_at = now(), version = version + 1 WHERE kind = 'ops';
ALTER TABLE supplier.supplier_sessions
    ADD CONSTRAINT supplier_retired_ops CHECK (kind <> 'ops' OR revoked_at IS NOT NULL);
