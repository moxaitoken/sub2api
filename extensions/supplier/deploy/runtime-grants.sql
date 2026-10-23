-- Execute as supplier_migrator, connected ONLY to sub2api_supplier, after -migrate.
REVOKE ALL ON SCHEMA supplier FROM PUBLIC;
GRANT USAGE ON SCHEMA supplier TO supplier_runtime;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA supplier TO supplier_runtime;
REVOKE INSERT,UPDATE,DELETE ON supplier.schema_migrations,supplier.upstream_instances FROM supplier_runtime;
REVOKE UPDATE,DELETE ON supplier.supplier_audit_events,supplier.supplier_settlements,supplier.supplier_finance_ledger,supplier.supplier_accrual_events FROM supplier_runtime;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA supplier TO supplier_runtime;

REVOKE UPDATE,DELETE ON supplier.group_access_audit FROM supplier_runtime;
