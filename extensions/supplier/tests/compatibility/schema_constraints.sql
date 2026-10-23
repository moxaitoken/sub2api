-- Run only after phase-1-schema.sql in a disposable PostgreSQL 18 database.
-- Synthetic fixtures only. Every fixture is rolled back at the end.
\set ON_ERROR_STOP on
SET search_path = supplier, pg_catalog;

DO $$
BEGIN
    IF (SELECT count(*) FROM pg_tables WHERE schemaname = 'supplier') <> 11 THEN
        RAISE EXCEPTION 'expected exactly 11 reference tables';
    END IF;
END $$;
SELECT 'PASS: reference schema has 11 tables' AS result;

BEGIN;
CREATE FUNCTION pg_temp.expect_failure(statement text, expected_state text, label text)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE = expected_state THEN
            RAISE NOTICE 'PASS: % (SQLSTATE %)', label, SQLSTATE;
            RETURN;
        END IF;
        RAISE EXCEPTION 'expected SQLSTATE % but got % for %: %',
            expected_state, SQLSTATE, label, SQLERRM;
    END;
    RAISE EXCEPTION 'expected SQLSTATE % for %', expected_state, label;
END $$;

INSERT INTO upstream_instances(id, code, source_sha) VALUES
('10000000-0000-0000-0000-000000000001','fixture-main', repeat('a',40)),
('10000000-0000-0000-0000-000000000002','fixture-other', repeat('b',40));
INSERT INTO suppliers(id,name) VALUES
('20000000-0000-0000-0000-000000000001','Fixture A'),
('20000000-0000-0000-0000-000000000002','Fixture B');
-- Keep only a single private login mapping, never a product role hierarchy.
INSERT INTO supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES
('70000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',1,'owner');
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES
('70000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',2,'owner')
$sql$,'23505','one supplier accepts only one login identity');
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_members(id,supplier_id,upstream_instance_id,upstream_user_id,role) VALUES
('70000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',1,'owner')
$sql$,'23505','one upstream login cannot map to another supplier');
SELECT pg_temp.expect_failure($sql$
UPDATE supplier_members SET role='viewer' WHERE id='70000000-0000-0000-0000-000000000001'
$sql$,'23514','legacy role column is fixed rather than configurable');

INSERT INTO supplier_proxies(id,supplier_id,upstream_instance_id,upstream_proxy_id,name) VALUES
('30000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',10,'Fixture proxy A'),
('30000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',20,'Fixture proxy B'),
('30000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002',10,'Fixture proxy other instance');
INSERT INTO supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,identity_fingerprint,identity_key_id,name,proxy_id) VALUES
('40000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',101,decode(repeat('aa',32),'hex'),'fixture-hmac-v1','Fixture account A','30000000-0000-0000-0000-000000000001'),
('40000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',102,decode(repeat('bb',32),'hex'),'fixture-hmac-v1','Fixture account B','30000000-0000-0000-0000-000000000002');

SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name)
VALUES ('40000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',101,'Duplicate upstream account')
$sql$,'23505','upstream account has a single supplier owner');
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_accounts(id,supplier_id,upstream_instance_id,identity_fingerprint,identity_key_id,name)
VALUES ('40000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',decode(repeat('aa',32),'hex'),'fixture-hmac-v1','Duplicate identity')
$sql$,'23505','same identity HMAC cannot belong to another supplier');
SELECT pg_temp.expect_failure($sql$
UPDATE supplier_accounts SET proxy_id='30000000-0000-0000-0000-000000000002'
WHERE id='40000000-0000-0000-0000-000000000001'
$sql$,'23503','cross-supplier proxy binding rejected');
SELECT pg_temp.expect_failure($sql$
UPDATE supplier_accounts SET proxy_id='30000000-0000-0000-0000-000000000003'
WHERE id='40000000-0000-0000-0000-000000000001'
$sql$,'23503','cross-instance proxy binding rejected');
SELECT pg_temp.expect_failure($sql$
UPDATE supplier_accounts SET desired_state='running'
WHERE id='40000000-0000-0000-0000-000000000001'
$sql$,'23514','unapproved account cannot have desired running state');
SELECT pg_temp.expect_failure($sql$
UPDATE supplier_accounts SET platform='openai'
WHERE id='40000000-0000-0000-0000-000000000001'
$sql$,'23514','non-Claude platform rejected');

INSERT INTO supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash) VALUES
('50000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001',1,'10000000-0000-0000-0000-000000000001','pause','account','40000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000001',decode(repeat('ab',32),'hex'));
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash) VALUES
('50000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000001',1,'10000000-0000-0000-0000-000000000001','delete','account','40000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000002',decode(repeat('ab',32),'hex'))
$sql$,'23505','second in-flight operation on resource rejected');
UPDATE supplier_operations SET state='needs_attention' WHERE id='50000000-0000-0000-0000-000000000001';
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash) VALUES
('50000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000001',1,'10000000-0000-0000-0000-000000000001','delete','account','40000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000002',decode(repeat('ab',32),'hex'))
$sql$,'23505','needs-attention resource remains locked');
UPDATE supplier_operations SET state='succeeded' WHERE id='50000000-0000-0000-0000-000000000001';
INSERT INTO supplier_operations(id,supplier_id,actor_user_id,upstream_instance_id,action,resource_type,resource_id,idempotency_key,request_hash) VALUES
('50000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000001',1,'10000000-0000-0000-0000-000000000001','delete','account','40000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000002',decode(repeat('ab',32),'hex'));
SELECT 'PASS: succeeded operation releases per-resource in-flight uniqueness' AS result;

INSERT INTO supplier_usage_records(upstream_instance_id,upstream_usage_id,supplier_id,account_id,occurred_at,account_name_snapshot,requested_model,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_cost,account_stats_cost,account_rate_multiplier)
SELECT '10000000-0000-0000-0000-000000000001', fixture.id,
       '20000000-0000-0000-0000-000000000001','40000000-0000-0000-0000-000000000001',
       '2026-10-22T00:00:00+08:00','Fixture account A','fixture-claude',1,2,3,4,
       fixture.total_cost,fixture.stats_cost,fixture.multiplier
FROM (VALUES (1001,10::numeric,NULL::numeric,NULL::numeric),
             (1002,10,0,2),(1003,10,2,0),(1004,10,2,3),
             (1005,10,NULL,2),(1006,10,2,NULL))
AS fixture(id,total_cost,stats_cost,multiplier);
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (VALUES (1001,10::numeric),(1002,0),(1003,0),(1004,6),(1005,20),(1006,2)) AS expected(id,cost)
        JOIN supplier_usage_records actual ON actual.upstream_usage_id=expected.id
        WHERE actual.account_cost <> expected.cost
    ) OR (SELECT count(*) FROM supplier_usage_records) <> 6 THEN
        RAISE EXCEPTION 'account cost NULL/zero cases differ';
    END IF;
END $$;
SELECT upstream_usage_id, account_stats_cost, account_rate_multiplier, account_cost
FROM supplier_usage_records ORDER BY upstream_usage_id;
SELECT 'PASS: generated account cost preserves NULL fallback and explicit zero' AS result;

SELECT pg_temp.expect_failure($sql$
DELETE FROM supplier_accounts WHERE id='40000000-0000-0000-0000-000000000001'
$sql$,'23001','usage history blocks hard account deletion instead of cascading');
UPDATE supplier_accounts SET admission_state='deleted',deleted_at=now()
WHERE id='40000000-0000-0000-0000-000000000001';
SELECT pg_temp.expect_failure($sql$
INSERT INTO supplier_accounts(id,supplier_id,upstream_instance_id,upstream_account_id,name)
VALUES ('40000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',101,'Tombstone reassignment')
$sql$,'23505','deleted account tombstone prevents historical owner reassignment');
DO $$ BEGIN
    IF (SELECT count(*) FROM supplier_usage_records) <> 6 THEN
        RAISE EXCEPTION 'soft deletion lost usage history';
    END IF;
END $$;
SELECT 'PASS: soft deletion retains all usage history' AS result;
ROLLBACK;

DO $$ DECLARE table_name text; remaining bigint;
BEGIN
    FOR table_name IN SELECT tablename FROM pg_tables WHERE schemaname='supplier' LOOP
        EXECUTE format('SELECT count(*) FROM supplier.%I',table_name) INTO remaining;
        IF remaining <> 0 THEN RAISE EXCEPTION 'rollback leaked fixtures into %',table_name; END IF;
    END LOOP;
END $$;
SELECT 'PASS: fixture transaction rolled back without residual rows in all 11 tables' AS result;
