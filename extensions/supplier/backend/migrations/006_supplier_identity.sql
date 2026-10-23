-- R2 local supplier identity; historical 001-005 bytes remain unchanged.
ALTER TABLE supplier.suppliers
 ADD COLUMN login_name varchar(64),
 ADD COLUMN login_normalized varchar(64),
 ADD COLUMN password_hash text,
 ADD COLUMN credential_version bigint NOT NULL DEFAULT 1 CHECK(credential_version>0),
 ADD COLUMN session_epoch bigint NOT NULL DEFAULT 1 CHECK(session_epoch>0),
 ADD CONSTRAINT supplier_login_unique UNIQUE(login_normalized),
 ADD CONSTRAINT supplier_login_shape CHECK((login_name IS NULL AND login_normalized IS NULL AND password_hash IS NULL) OR
 (login_name IS NOT NULL AND login_normalized IS NOT NULL AND login_name ~ '^[A-Za-z0-9_.@-]{4,64}$' AND login_normalized=lower(login_name) AND password_hash IS NOT NULL));
-- Replace only the historical pair/kind checks; keep all other session constraints.
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='supplier.supplier_sessions'::regclass AND contype='c'
 AND (pg_get_constraintdef(oid) LIKE '%kind%' OR pg_get_constraintdef(oid) LIKE '%supplier_id%') LOOP
  EXECUTE format('ALTER TABLE supplier.supplier_sessions DROP CONSTRAINT %I',c.conname);
 END LOOP;
END $$;
ALTER TABLE supplier.supplier_sessions
 ADD COLUMN credential_version bigint NOT NULL DEFAULT 0 CHECK(credential_version>=0),
 ADD COLUMN session_epoch bigint NOT NULL DEFAULT 0 CHECK(session_epoch>=0),
 ADD CONSTRAINT supplier_session_kind CHECK(kind IN ('challenge','supplier','ops','local_supplier','admin','admin_challenge')),
 ADD CONSTRAINT supplier_session_scope CHECK(
 (kind='supplier' AND supplier_id IS NOT NULL AND member_id IS NOT NULL AND upstream_user_id IS NOT NULL) OR
 (kind='local_supplier' AND supplier_id IS NOT NULL AND member_id IS NULL AND upstream_user_id IS NULL AND credential_version>0 AND session_epoch>0) OR
 (kind='admin' AND supplier_id IS NULL AND member_id IS NULL AND upstream_user_id IS NOT NULL) OR
 (kind IN ('challenge','admin_challenge') AND supplier_id IS NULL AND member_id IS NULL AND upstream_user_id IS NULL) OR
 (kind='ops' AND revoked_at IS NOT NULL AND supplier_id IS NULL AND member_id IS NULL AND upstream_user_id IS NOT NULL));
ALTER TABLE supplier.supplier_audit_events ALTER COLUMN actor_user_id DROP NOT NULL;
ALTER TABLE supplier.supplier_audit_events
 ADD COLUMN actor_kind text,
 ADD COLUMN actor_id text,
 ADD CONSTRAINT supplier_audit_actor CHECK(actor_user_id IS NOT NULL OR (actor_kind IS NOT NULL AND actor_id IS NOT NULL AND actor_kind='supplier' AND actor_id=supplier_id::text));
CREATE TABLE supplier.supplier_balances(
 supplier_id uuid PRIMARY KEY REFERENCES supplier.suppliers(id) ON DELETE RESTRICT,
 currency text NOT NULL DEFAULT 'USD' CHECK(currency='USD'),
 unsettled_cents bigint NOT NULL DEFAULT 0 CHECK(unsettled_cents>=0),
 settled_cents bigint NOT NULL DEFAULT 0 CHECK(settled_cents>=0),
 accrual_remainder numeric(30,12) NOT NULL DEFAULT 0,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0));
-- There is no pre-existing finance implementation in this module.
INSERT INTO supplier.supplier_balances(supplier_id) SELECT id FROM supplier.suppliers;

-- Invalidate the previous local playground authentication generation on cutover.
UPDATE supplier.supplier_sessions SET revoked_at=COALESCE(revoked_at,now()),updated_at=now(),version=version+1
 WHERE kind IN ('supplier','challenge','ops') AND revoked_at IS NULL;
