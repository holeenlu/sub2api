-- Keep original accruals for audit and gross per-invitee lifetime caps.
-- Only the remaining amount may thaw after a proportional refund reversal.
ALTER TABLE user_affiliate_ledger ADD COLUMN IF NOT EXISTS reversed_amount NUMERIC(20,8) NOT NULL DEFAULT 0;
ALTER TABLE user_affiliate_ledger ADD CONSTRAINT affiliate_reversed_amount_valid CHECK (reversed_amount >= 0 AND (action <> 'accrue' OR reversed_amount <= amount));

-- Preserve historical unknown orders for reconciliation, reject new/changed
-- unknown types. Service fulfillment also rejects legacy unknown types.
ALTER TABLE payment_orders ADD CONSTRAINT payment_order_known_type CHECK (order_type IN ('balance','subscription')) NOT VALID;

-- Reconcile already-finalized refunds as well as future service-path refunds.
-- Retain original amounts for audit/caps; recover transferred/withdrawn rewards
-- through balance debt without discarding already-consumed liabilities.
DO $$
DECLARE entry RECORD; target NUMERIC(20,8); delta NUMERIC(20,8); available NUMERIC(20,8);
BEGIN
  FOR entry IN
    SELECT l.id, l.user_id, l.source_user_id, l.source_order_id, l.amount,
           l.reversed_amount, l.frozen_until, po.refund_amount, po.amount AS order_amount
    FROM user_affiliate_ledger l JOIN payment_orders po ON po.id=l.source_order_id
    WHERE l.action='accrue' AND po.status IN ('REFUNDED','PARTIALLY_REFUNDED')
      AND po.amount>0 AND po.refund_amount>0
    ORDER BY l.user_id, l.id
  LOOP
    target := ROUND(entry.amount * LEAST(entry.refund_amount / entry.order_amount, 1), 8);
    delta := target - entry.reversed_amount;
    IF delta <= 0 THEN CONTINUE; END IF;
    SELECT GREATEST(aff_quota,0) INTO available FROM user_affiliates WHERE user_id=entry.user_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing affiliate profile for refund recovery %', entry.user_id; END IF;
    IF entry.frozen_until IS NOT NULL THEN
      UPDATE user_affiliates SET aff_frozen_quota=aff_frozen_quota-delta,updated_at=NOW() WHERE user_id=entry.user_id;
    ELSE
      available := LEAST(available,delta);
      UPDATE user_affiliates SET aff_quota=aff_quota-available,updated_at=NOW() WHERE user_id=entry.user_id;
      UPDATE users SET balance=balance-(delta-available),updated_at=NOW() WHERE id=entry.user_id;
    END IF;
    UPDATE user_affiliate_ledger SET reversed_amount=target,updated_at=NOW() WHERE id=entry.id;
    INSERT INTO user_affiliate_ledger(user_id,action,amount,source_user_id,source_order_id,created_at,updated_at)
      VALUES(entry.user_id,'refund_reversal',-delta,entry.source_user_id,entry.source_order_id,NOW(),NOW());
  END LOOP;
END $$;

-- Upgrade identifiable legacy balance-refund jobs. Old pending handling restored
-- their collateral, so recapture it (including debt if already consumed) before
-- the new reconciliation worker can finish them. Ambiguous/missing audits and
-- legacy subscription jobs require explicit accounting reconciliation; never
-- infer permission to debit from missing data or resubmit their gateway refund.
DO $$
DECLARE pending RECORD; detail JSONB; desired NUMERIC(20,8); captured NUMERIC(20,8);
BEGIN
  FOR pending IN
    SELECT po.*, audit.detail AS pending_detail
    FROM payment_orders po
    JOIN payment_audit_logs audit ON audit.order_id=po.id::text AND audit.action='REFUND_PENDING'
    WHERE po.status='REFUND_PENDING' AND po.order_type='balance' AND po.amount>0
      AND NOT EXISTS(SELECT 1 FROM payment_audit_logs job WHERE job.order_id=po.id::text AND job.action='REFUND_RESERVATION')
    ORDER BY po.id
    FOR UPDATE OF po
  LOOP
    detail := pending.pending_detail::jsonb;
    IF NOT (detail ? 'balanceRolledBack' AND detail ? 'deductionRollbackOK')
      OR (detail->>'refundAmount')::numeric IS DISTINCT FROM pending.refund_amount
      OR (detail->>'force')::boolean IS DISTINCT FROM pending.force_refund THEN CONTINUE; END IF;
    desired := (detail->>'balanceRolledBack')::numeric;
    IF desired IS NULL OR desired<0 OR (desired=0 AND NOT pending.force_refund) THEN CONTINUE; END IF;
    IF NOT pending.force_refund THEN desired := pending.refund_amount; END IF;
    captured := CASE WHEN (detail->>'deductionRollbackOK')::boolean THEN 0 ELSE COALESCE((detail->>'balanceDeducted')::numeric,0) END;
    IF captured<0 OR captured>desired THEN CONTINUE; END IF;
    UPDATE users SET balance=balance-(desired-captured),updated_at=NOW() WHERE id=pending.user_id;
    INSERT INTO payment_audit_logs(order_id,action,detail,operator,created_at)
    VALUES(pending.id::text,'REFUND_RESERVATION',jsonb_build_object(
      'state','held','refund_id',COALESCE(detail->>'refundID',''),
      'plan',jsonb_build_object('ReservationID','legacy-'||pending.id::text,'OrderID',pending.id,
        'RefundAmount',pending.refund_amount,'GatewayAmount',pending.pay_amount*pending.refund_amount/pending.amount,
        'Reason',COALESCE(pending.refund_reason,'legacy pending refund'),'Force',pending.force_refund,
        'DeductBalance',true,'DeductionType','balance','BalanceToDeduct',desired,'SubDaysToDeduct',0,'SubscriptionID',0)
    )::text,'security-migration',NOW());
  END LOOP;
END $$;
