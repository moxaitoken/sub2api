-- Forward only. Existing records and historical migration checksums are retained.
ALTER TABLE supplier.supplier_accounts
    ADD COLUMN usage_history_start date,
    ADD COLUMN usage_discovered_at timestamptz,
    ADD COLUMN usage_discovery_retry_at timestamptz;
ALTER TABLE supplier.supplier_sync_cursors
    ADD COLUMN scan_pass smallint NOT NULL DEFAULT 1 CHECK (scan_pass IN (1,2)),
    ADD COLUMN scan_total bigint CHECK (scan_total >= 0),
    ADD COLUMN scan_count bigint NOT NULL DEFAULT 0 CHECK (scan_count >= 0),
    ADD COLUMN scan_last_id bigint NOT NULL DEFAULT 0 CHECK (scan_last_id >= 0),
    ADD COLUMN verify_fingerprint bytea,
    ADD COLUMN retry_count integer NOT NULL DEFAULT 0 CHECK (retry_count >= 0);
CREATE INDEX supplier_usage_discovery_due ON supplier.supplier_accounts(upstream_instance_id,usage_discovery_retry_at)
    WHERE upstream_account_id > 0;
