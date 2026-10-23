-- Forward-only R2 account onboarding. No fictitious upstream user/member is created.
ALTER TABLE supplier.supplier_oauth_sessions ALTER COLUMN actor_user_id DROP NOT NULL;
ALTER TABLE supplier.supplier_operations ALTER COLUMN actor_user_id DROP NOT NULL;
ALTER TABLE supplier.supplier_oauth_sessions
 ADD COLUMN actor_kind text NOT NULL DEFAULT 'upstream_user',
 ADD COLUMN actor_id text,
 ADD COLUMN credential_version bigint,
 ADD COLUMN session_epoch bigint,
 ADD COLUMN target_group_id bigint,
 ADD COLUMN begin_key uuid,
 ADD COLUMN request_hash bytea,
 ADD COLUMN proxy_version bigint,
 ADD COLUMN created_account_id uuid,
 ADD CONSTRAINT oauth_local_actor CHECK(
 (actor_kind='upstream_user' AND actor_user_id IS NOT NULL) OR
 (actor_kind='supplier' AND actor_user_id IS NULL AND actor_id IS NOT NULL AND actor_id=supplier_id::text AND credential_version IS NOT NULL AND session_epoch IS NOT NULL AND target_group_id IS NOT NULL AND proxy_version IS NOT NULL AND credential_version>0 AND session_epoch>0 AND target_group_id>0 AND proxy_id IS NOT NULL AND proxy_version>0 AND begin_key IS NOT NULL AND request_hash IS NOT NULL AND octet_length(request_hash)=32)),
 ADD FOREIGN KEY (supplier_id,created_account_id) REFERENCES supplier.supplier_accounts(supplier_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX oauth_local_begin_key ON supplier.supplier_oauth_sessions(supplier_id,begin_key) WHERE actor_kind='supplier';
ALTER TABLE supplier.supplier_operations
 ADD COLUMN actor_kind text NOT NULL DEFAULT 'upstream_user',
 ADD COLUMN actor_id text,
 ADD CONSTRAINT operation_local_actor CHECK((actor_kind='upstream_user' AND actor_user_id IS NOT NULL) OR (actor_kind='supplier' AND actor_user_id IS NULL AND actor_id IS NOT NULL AND actor_id=supplier_id::text));
CREATE UNIQUE INDEX operation_local_key ON supplier.supplier_operations(supplier_id,action,idempotency_key) WHERE actor_kind='supplier';
ALTER TABLE supplier.supplier_proxies
 ADD COLUMN address text NOT NULL DEFAULT '',
 ADD COLUMN region text NOT NULL DEFAULT 'unknown',
 ADD COLUMN config_hash bytea,
 ADD COLUMN capacity bigint NOT NULL DEFAULT 1 CHECK(capacity>0 AND capacity<=1000),
 ADD COLUMN create_operation_id uuid,
 ADD FOREIGN KEY (supplier_id,create_operation_id) REFERENCES supplier.supplier_operations(supplier_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX supplier_proxy_configuration ON supplier.supplier_proxies(supplier_id,upstream_instance_id,config_hash) WHERE deleted_at IS NULL;
CREATE INDEX supplier_oauth_proxy_capacity ON supplier.supplier_oauth_sessions(proxy_id,status,expires_at) WHERE actor_kind='supplier';
