-- Freeze supplier-selected account limits across the OAuth redirect.
ALTER TABLE supplier.supplier_oauth_sessions
  ADD COLUMN account_settings jsonb NOT NULL DEFAULT '{}'::jsonb;
