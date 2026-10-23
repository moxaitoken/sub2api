-- Official standard usage cost, 1:1 USD. Group/user/account multipliers do not apply.
-- Keep sub-cent USD exactly; only whole cents become available for settlement.
ALTER TABLE supplier.supplier_balances
 ALTER COLUMN accrual_remainder TYPE numeric(38,18),
 ADD CONSTRAINT supplier_accrual_remainder CHECK(accrual_remainder>=0 AND accrual_remainder<0.01);

CREATE TABLE supplier.supplier_accrual_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 upstream_instance_id uuid NOT NULL,
 upstream_usage_id bigint NOT NULL,
 source_version bigint NOT NULL CHECK(source_version>0),
 supplier_id uuid NOT NULL REFERENCES supplier.suppliers(id) ON DELETE RESTRICT,
 account_id uuid NOT NULL,
 rule_revision text NOT NULL CHECK(rule_revision='official_usd_v1'),
 origin text NOT NULL CHECK(origin IN ('backfill','sync')),
 previous_cost numeric(38,18) NOT NULL CHECK(previous_cost>=0),
 standard_cost numeric(38,18) NOT NULL CHECK(standard_cost>=0),
 exact_delta numeric(38,18) NOT NULL CHECK(exact_delta=standard_cost-previous_cost),
 posted_cents bigint NOT NULL,
 before_cents bigint NOT NULL CHECK(before_cents>=0),
 after_cents bigint NOT NULL CHECK(after_cents=before_cents+posted_cents AND after_cents>=0),
 remainder_before numeric(38,18) NOT NULL CHECK(remainder_before>=0 AND remainder_before<0.01),
 remainder_after numeric(38,18) NOT NULL CHECK(remainder_after>=0 AND remainder_after<0.01),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(upstream_instance_id,upstream_usage_id,source_version,rule_revision),
 FOREIGN KEY(upstream_instance_id,upstream_usage_id) REFERENCES supplier.supplier_usage_records(upstream_instance_id,upstream_usage_id) ON DELETE RESTRICT,
 FOREIGN KEY(supplier_id,upstream_instance_id,account_id) REFERENCES supplier.supplier_accounts(supplier_id,upstream_instance_id,id) ON DELETE RESTRICT,
 CHECK(remainder_before+exact_delta=posted_cents::numeric/100+remainder_after)
);
CREATE INDEX supplier_accrual_history ON supplier.supplier_accrual_events(supplier_id,id DESC);

-- One forward cutover: book already-synchronized history once. Later writes use
-- the same balance lock and exact arithmetic in repository.writeUsageRecord.
DO $$
DECLARE
 r supplier.supplier_usage_records;
 before_balance bigint;
 before_remainder numeric;
 exact_amount numeric;
 posted numeric;
 after_balance numeric;
 after_remainder numeric;
BEGIN
 FOR r IN SELECT * FROM supplier.supplier_usage_records ORDER BY supplier_id,upstream_instance_id,upstream_usage_id LOOP
  INSERT INTO supplier.supplier_balances(supplier_id) VALUES(r.supplier_id) ON CONFLICT DO NOTHING;
  SELECT unsettled_cents,accrual_remainder INTO STRICT before_balance,before_remainder
  FROM supplier.supplier_balances WHERE supplier_id=r.supplier_id FOR UPDATE;
  exact_amount:=before_remainder+r.total_cost;
  posted:=floor(exact_amount*100);
  after_balance:=before_balance::numeric+posted;
  after_remainder:=exact_amount-posted/100;
  IF after_balance>9223372036854775807 OR posted>9223372036854775807 THEN
   RAISE EXCEPTION 'accrual would exceed available balance bounds' USING ERRCODE='23514';
  END IF;
  INSERT INTO supplier.supplier_accrual_events(upstream_instance_id,upstream_usage_id,source_version,supplier_id,account_id,rule_revision,origin,previous_cost,standard_cost,exact_delta,posted_cents,before_cents,after_cents,remainder_before,remainder_after)
  VALUES(r.upstream_instance_id,r.upstream_usage_id,r.version,r.supplier_id,r.account_id,'official_usd_v1','backfill',0,r.total_cost,r.total_cost,posted,before_balance,after_balance,before_remainder,after_remainder);
  UPDATE supplier.supplier_balances SET unsettled_cents=after_balance,accrual_remainder=after_remainder,version=version+1 WHERE supplier_id=r.supplier_id;
 END LOOP;
END $$;
