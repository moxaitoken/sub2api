-- Map one or more upstream Anthropic groups to a supplier-facing option name.
-- The mapping remains in the supplier schema; the upstream group schema is untouched.
ALTER TABLE supplier.group_access
  ADD COLUMN supplier_group_name text NOT NULL DEFAULT '' CHECK (char_length(supplier_group_name) <= 100);

ALTER TABLE supplier.group_access_audit
  ADD COLUMN supplier_group_name text NOT NULL DEFAULT '' CHECK (char_length(supplier_group_name) <= 100);

ALTER TABLE supplier.supplier_oauth_sessions
  ADD COLUMN target_group_ids bigint[] NOT NULL DEFAULT '{}';

UPDATE supplier.supplier_oauth_sessions
SET target_group_ids = ARRAY[target_group_id]
WHERE actor_kind = 'supplier' AND target_group_id IS NOT NULL AND cardinality(target_group_ids) = 0;

-- Membership in enabled groups is checked again by the service immediately
-- before OAuth exchange and before the account is enabled.
