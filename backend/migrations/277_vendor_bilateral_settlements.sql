-- Two independent purchasing edges on the existing accounting ledger.
-- Old evidence remains platform -> its recorded vendor. Never infer a second
-- historical payable from today's hierarchy or account ownership.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS vendor_root_id_snapshot BIGINT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS vendor_parent_id_snapshot BIGINT;
UPDATE usage_logs SET vendor_root_id_snapshot=vendor_id_snapshot
 WHERE vendor_id_snapshot IS NOT NULL AND vendor_root_id_snapshot IS NULL;
CREATE INDEX IF NOT EXISTS idx_usage_logs_vendor_root_time ON usage_logs(vendor_root_id_snapshot,created_at,id) WHERE vendor_root_id_snapshot IS NOT NULL;

-- The producer already captures the selected direct owner, including live-call
-- completion. Resolve ONLY that owner's immutable hierarchy (migration 276),
-- never the account's current owner. Store it once in the same usage INSERT.
CREATE OR REPLACE FUNCTION capture_vendor_usage_hierarchy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE root_id BIGINT; parent_id BIGINT;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.vendor_id_snapshot IS DISTINCT FROM OLD.vendor_id_snapshot
   OR NEW.vendor_root_id_snapshot IS DISTINCT FROM OLD.vendor_root_id_snapshot
   OR NEW.vendor_parent_id_snapshot IS DISTINCT FROM OLD.vendor_parent_id_snapshot THEN
   RAISE EXCEPTION 'usage supplier attribution is immutable';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.vendor_id_snapshot IS NULL THEN
  IF NEW.vendor_root_id_snapshot IS NOT NULL OR NEW.vendor_parent_id_snapshot IS NOT NULL THEN
   RAISE EXCEPTION 'usage hierarchy requires a captured direct owner';
  END IF;
  RETURN NEW;
 END IF;
 SELECT CASE WHEN tier=1 THEN id ELSE parent_vendor_id END,parent_vendor_id
 INTO STRICT root_id,parent_id FROM vendors WHERE id=NEW.vendor_id_snapshot;
 IF (NEW.vendor_root_id_snapshot IS NOT NULL AND NEW.vendor_root_id_snapshot<>root_id)
  OR (NEW.vendor_parent_id_snapshot IS NOT NULL AND NEW.vendor_parent_id_snapshot IS DISTINCT FROM parent_id) THEN
  RAISE EXCEPTION 'usage hierarchy does not match immutable direct owner';
 END IF;
 NEW.vendor_root_id_snapshot=root_id;
 NEW.vendor_parent_id_snapshot=parent_id;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS usage_logs_vendor_hierarchy ON usage_logs;
CREATE TRIGGER usage_logs_vendor_hierarchy BEFORE INSERT OR UPDATE OF vendor_id_snapshot,vendor_root_id_snapshot,vendor_parent_id_snapshot
 ON usage_logs FOR EACH ROW EXECUTE FUNCTION capture_vendor_usage_hierarchy();

ALTER TABLE vendor_settlements ADD COLUMN IF NOT EXISTS payer_vendor_id BIGINT REFERENCES vendors(id) ON DELETE RESTRICT;
ALTER TABLE vendor_settlement_allocations ADD COLUMN IF NOT EXISTS payer_vendor_id BIGINT REFERENCES vendors(id) ON DELETE RESTRICT;
ALTER TABLE vendor_settlement_payments ADD COLUMN IF NOT EXISTS payer_vendor_id BIGINT REFERENCES vendors(id) ON DELETE RESTRICT;
ALTER TABLE vendor_settlements DROP CONSTRAINT IF EXISTS vendor_settlements_vendor_id_idempotency_key_key;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_settlements_platform_key_idx ON vendor_settlements(vendor_id,idempotency_key) WHERE payer_vendor_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_settlements_supplier_key_idx ON vendor_settlements(payer_vendor_id,vendor_id,idempotency_key) WHERE payer_vendor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS vendor_settlements_payer_period_idx ON vendor_settlements(payer_vendor_id,start_at,id) WHERE payer_vendor_id IS NOT NULL;

DROP INDEX IF EXISTS vendor_settlement_allocations_active_usage_idx;
DROP INDEX IF EXISTS vendor_settlement_allocations_active_source_idx;
-- NULL represents the platform; partial indexes give it real uniqueness.
-- Payee is deliberately absent: cleanup/retries must not charge the same payer
-- again merely because a replay was routed to another supplier.
CREATE UNIQUE INDEX IF NOT EXISTS vendor_allocations_platform_usage_idx ON vendor_settlement_allocations(usage_log_id) WHERE active AND payer_vendor_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_allocations_supplier_usage_idx ON vendor_settlement_allocations(payer_vendor_id,usage_log_id) WHERE active AND payer_vendor_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_allocations_platform_source_idx ON vendor_settlement_allocations(source_fingerprint) WHERE active AND payer_vendor_id IS NULL AND source_fingerprint IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_allocations_supplier_source_idx ON vendor_settlement_allocations(payer_vendor_id,source_fingerprint) WHERE active AND payer_vendor_id IS NOT NULL AND source_fingerprint IS NOT NULL;

DROP INDEX IF EXISTS vendor_settlement_payments_receipt_idx;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_payments_platform_receipt_idx ON vendor_settlement_payments(channel,reference) WHERE reversal_of_id IS NULL AND payer_vendor_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_payments_supplier_receipt_idx ON vendor_settlement_payments(payer_vendor_id,channel,reference) WHERE reversal_of_id IS NULL AND payer_vendor_id IS NOT NULL;

CREATE OR REPLACE FUNCTION guard_vendor_settlement_edge() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE st_vendor BIGINT; st_payer BIGINT;
BEGIN
 IF TG_TABLE_NAME='vendor_settlements' THEN
  IF TG_OP='UPDATE' THEN
   IF NEW.vendor_id IS DISTINCT FROM OLD.vendor_id OR NEW.payer_vendor_id IS DISTINCT FROM OLD.payer_vendor_id THEN
    RAISE EXCEPTION 'settlement edge is immutable';
   END IF;
  ELSE
   PERFORM 1 FROM vendors WHERE id=NEW.vendor_id AND
    ((NEW.payer_vendor_id IS NULL AND tier=1 AND parent_vendor_id IS NULL) OR
     (NEW.payer_vendor_id IS NOT NULL AND tier=2 AND parent_vendor_id=NEW.payer_vendor_id));
   IF NOT FOUND THEN RAISE EXCEPTION 'invalid settlement edge'; END IF;
  END IF;
  IF NEW.parent_statement_id IS NOT NULL THEN
   SELECT vendor_id,payer_vendor_id INTO STRICT st_vendor,st_payer FROM vendor_settlements WHERE id=NEW.parent_statement_id;
   IF st_vendor<>NEW.vendor_id OR st_payer IS DISTINCT FROM NEW.payer_vendor_id THEN RAISE EXCEPTION 'adjustment edge mismatch'; END IF;
  END IF;
 ELSE
  SELECT vendor_id,payer_vendor_id INTO STRICT st_vendor,st_payer FROM vendor_settlements WHERE id=NEW.statement_id;
  IF NEW.payer_vendor_id IS DISTINCT FROM st_payer THEN RAISE EXCEPTION 'ledger payer mismatch'; END IF;
  IF TG_TABLE_NAME='vendor_settlement_allocations' THEN
   IF NEW.vendor_id<>st_vendor THEN RAISE EXCEPTION 'allocation payee mismatch'; END IF;
  END IF;
  IF TG_OP='UPDATE' AND (NEW.statement_id IS DISTINCT FROM OLD.statement_id OR NEW.payer_vendor_id IS DISTINCT FROM OLD.payer_vendor_id) THEN
   RAISE EXCEPTION 'ledger edge is immutable';
  END IF;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS vendor_settlements_edge ON vendor_settlements;
CREATE TRIGGER vendor_settlements_edge BEFORE INSERT OR UPDATE OF vendor_id,payer_vendor_id,parent_statement_id ON vendor_settlements FOR EACH ROW EXECUTE FUNCTION guard_vendor_settlement_edge();
DROP TRIGGER IF EXISTS vendor_allocations_edge ON vendor_settlement_allocations;
CREATE TRIGGER vendor_allocations_edge BEFORE INSERT OR UPDATE OF statement_id,vendor_id,payer_vendor_id ON vendor_settlement_allocations FOR EACH ROW EXECUTE FUNCTION guard_vendor_settlement_edge();
DROP TRIGGER IF EXISTS vendor_payments_edge ON vendor_settlement_payments;
CREATE TRIGGER vendor_payments_edge BEFORE INSERT OR UPDATE OF statement_id,payer_vendor_id ON vendor_settlement_payments FOR EACH ROW EXECUTE FUNCTION guard_vendor_settlement_edge();
