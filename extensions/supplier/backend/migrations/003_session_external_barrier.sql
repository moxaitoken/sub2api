-- Once persisted, this marker must remain until the new ciphertext commits.
-- A crash, timeout or canceled request after external token consumption therefore
-- cannot replay the old refresh token on a subsequent request.
ALTER TABLE supplier.supplier_sessions
    ADD COLUMN external_pending boolean NOT NULL DEFAULT false;
