-- Completed offline-payment registration. Procurement accrual is deliberately not inferred.
CREATE TABLE supplier.supplier_settlements (
 id uuid PRIMARY KEY,
 supplier_id uuid NOT NULL REFERENCES supplier.suppliers(id) ON DELETE RESTRICT,
 supplier_name text NOT NULL,
 login_account text NOT NULL,
 amount_cents bigint NOT NULL CHECK(amount_cents>0),
 before_cents bigint NOT NULL CHECK(before_cents>=amount_cents),
 after_cents bigint NOT NULL CHECK(after_cents=before_cents-amount_cents),
 settled_cents bigint NOT NULL CHECK(settled_cents>=amount_cents),
 payment_method text NOT NULL CHECK(payment_method IN ('usd_transfer','cny_transfer','usdt_transfer','other')),
 reference text NOT NULL CHECK(char_length(reference)<=200),
 remark text NOT NULL CHECK(char_length(remark)<=1000),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 upstream_instance_id uuid NOT NULL,
 idempotency_key text NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(upstream_instance_id,actor_id,idempotency_key)
);
CREATE TABLE supplier.supplier_finance_ledger (
 id uuid PRIMARY KEY,
 settlement_id uuid NOT NULL UNIQUE REFERENCES supplier.supplier_settlements(id) ON DELETE RESTRICT
);
CREATE INDEX supplier_settlement_history ON supplier.supplier_settlements(supplier_id,created_at DESC,id);
