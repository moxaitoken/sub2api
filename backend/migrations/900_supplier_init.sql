-- Supplier module (fork-local).
--
-- A supplier is a regular user who contributes upstream accounts and proxies and is paid
-- for the account cost those accounts generate. Everything lives in separate tables so the
-- upstream accounts / proxies / users schemas stay untouched:
--   - accounts and proxies are soft-deleted upstream, so mappings keep pointing at them and
--     historical usage stays attributable to the supplier;
--   - settlement items copy the numbers they were built from, so later account edits or
--     deletions never change an issued statement.
--
-- The 900_ prefix keeps these files after upstream migrations on fresh installs; the tables
-- only reference long-standing upstream tables (users, accounts, proxies).

CREATE TABLE IF NOT EXISTS suppliers (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT       NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                VARCHAR(100) NOT NULL,
    status              VARCHAR(20)  NOT NULL DEFAULT 'active',
    default_concurrency INT          NOT NULL DEFAULT 1,
    max_accounts        INT          NOT NULL DEFAULT 0,
    notes               TEXT         NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT suppliers_user_id_unique UNIQUE (user_id),
    CONSTRAINT suppliers_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT suppliers_default_concurrency_check CHECK (default_concurrency >= 1),
    CONSTRAINT suppliers_max_accounts_check CHECK (max_accounts >= 0)
);

CREATE TABLE IF NOT EXISTS supplier_accounts (
    account_id    BIGINT      PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    supplier_id   BIGINT      NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    review_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    review_note   TEXT        NOT NULL DEFAULT '',
    reviewed_by   BIGINT,
    reviewed_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT supplier_accounts_review_status_check
        CHECK (review_status IN ('pending', 'approved', 'rejected'))
);

CREATE INDEX IF NOT EXISTS idx_supplier_accounts_supplier_id ON supplier_accounts(supplier_id);

CREATE TABLE IF NOT EXISTS supplier_proxies (
    proxy_id    BIGINT      PRIMARY KEY REFERENCES proxies(id) ON DELETE CASCADE,
    supplier_id BIGINT      NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_proxies_supplier_id ON supplier_proxies(supplier_id);

CREATE TABLE IF NOT EXISTS supplier_settlements (
    id                    BIGSERIAL PRIMARY KEY,
    supplier_id           BIGINT         NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    period_start          TIMESTAMPTZ    NOT NULL,
    period_end            TIMESTAMPTZ    NOT NULL,
    status                VARCHAR(20)    NOT NULL DEFAULT 'draft',
    request_count         BIGINT         NOT NULL DEFAULT 0,
    input_tokens          BIGINT         NOT NULL DEFAULT 0,
    output_tokens         BIGINT         NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT         NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT         NOT NULL DEFAULT 0,
    total_amount          NUMERIC(20,10) NOT NULL DEFAULT 0,
    notes                 TEXT           NOT NULL DEFAULT '',
    created_by            BIGINT,
    confirmed_at          TIMESTAMPTZ,
    paid_at               TIMESTAMPTZ,
    voided_at             TIMESTAMPTZ,
    created_at            TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT supplier_settlements_period_check CHECK (period_end > period_start),
    CONSTRAINT supplier_settlements_status_check
        CHECK (status IN ('draft', 'confirmed', 'paid', 'void'))
);

CREATE INDEX IF NOT EXISTS idx_supplier_settlements_supplier_period
    ON supplier_settlements(supplier_id, period_start);

CREATE TABLE IF NOT EXISTS supplier_settlement_items (
    settlement_id         BIGINT         NOT NULL REFERENCES supplier_settlements(id) ON DELETE CASCADE,
    account_id            BIGINT         NOT NULL,
    account_name          VARCHAR(255)   NOT NULL DEFAULT '',
    model                 VARCHAR(255)   NOT NULL,
    request_count         BIGINT         NOT NULL DEFAULT 0,
    input_tokens          BIGINT         NOT NULL DEFAULT 0,
    output_tokens         BIGINT         NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT         NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT         NOT NULL DEFAULT 0,
    amount                NUMERIC(20,10) NOT NULL DEFAULT 0,
    PRIMARY KEY (settlement_id, account_id, model)
);
