package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Persistence happens before the first debit. Requests do not return leaving
// unjournaled closures in a process-local worker queue.
func (r *usageBillingRepository) persistUsageBillingEvent(ctx context.Context, cmd *service.UsageBillingCommand) error {
	if r == nil || r.db == nil {
		return errors.New("usage billing database unavailable")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return service.ErrUsageBillingRequestIDRequired
	}
	payload, err := usageBillingEventPayload(cmd)
	if err != nil {
		return err
	}
	status := "pending"
	if cmd.Pricing != nil && cmd.Pricing.Pending {
		status = "pending_pricing"
	}
	var fingerprint string
	// A bounded retry handles a short connection interruption. No successful
	// persistence is claimed if every attempt fails; caller retains the error.
	for attempt := 0; attempt < 3; attempt++ {
		err = r.db.QueryRowContext(ctx, `INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status)
   VALUES($1,$2,$3,$4,$5) ON CONFLICT(request_id,api_key_id)
   DO UPDATE SET updated_at=usage_billing_events.updated_at RETURNING request_fingerprint`,
			cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, string(payload), status).Scan(&fingerprint)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if err != nil {
		return fmt.Errorf("persist usage settlement: %w", err)
	}
	if fingerprint != cmd.RequestFingerprint {
		return service.ErrUsageBillingRequestConflict
	}
	return nil
}

// usageBillingEventPayload is the single V1 serializer for live, recovered and Native evidence settlement.
func usageBillingEventPayload(cmd *service.UsageBillingCommand) ([]byte, error) {
	event := *cmd
	event.PayloadVersion = 1
	cloneLog := func(log *service.UsageLog) *service.UsageLog {
		if log == nil {
			return nil
		}
		copy := *log
		copy.User = nil
		copy.APIKey = nil
		copy.Account = nil
		copy.Group = nil
		copy.Subscription = nil
		// UsageLowerBound is intentionally not a public JSON field. Preserve
		// this confirmed fact in the existing V1 billing_status field so a
		// process crash cannot relabel partial usage as complete settlement.
		if copy.UsageLowerBound {
			copy.BillingStatus = "lower_bound"
		}
		return &copy
	}
	event.UsageLog = cloneLog(cmd.UsageLog)
	event.UsageLogs = make([]*service.UsageLog, len(cmd.UsageLogs))
	for i, log := range cmd.UsageLogs {
		event.UsageLogs[i] = cloneLog(log)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if len(payload) > 1<<20 {
		return nil, errors.New("usage settlement snapshot exceeds size limit")
	}
	return payload, nil
}

// usageBillingLedger implements storage/pricing ports for billing.Ledger. It
// has one call owner; result is the live SQL result, never queued recovery state.
// V1 serialization and PostgreSQL transactions remain in this storage adapter.
type usageBillingLedger struct {
	repo    *usageBillingRepository
	reprice func(context.Context, *service.UsageBillingCommand) error
	result  *service.UsageBillingApplyResult
}

func concreteBillingCommand(cmd billing.Command) (*service.UsageBillingCommand, error) {
	concrete, ok := cmd.(*service.UsageBillingCommand)
	if !ok || concrete == nil {
		return nil, errors.New("invalid usage settlement command")
	}
	return concrete, nil
}

func (a *usageBillingLedger) Record(ctx context.Context, command billing.Command) error {
	cmd, err := concreteBillingCommand(command)
	if err != nil {
		return err
	}
	return a.repo.persistUsageBillingEvent(ctx, cmd)
}

func (a *usageBillingLedger) Commit(ctx context.Context, command billing.Command) error {
	cmd, err := concreteBillingCommand(command)
	if err != nil {
		return err
	}
	a.result, err = a.repo.applyUsageBillingTransaction(ctx, cmd)
	return err
}

func (a *usageBillingLedger) Retry(ctx context.Context, id billing.Identity, pending bool, cause error) {
	status := "failed"
	if pending {
		status = "pending_pricing"
	}
	message := cause.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	_, _ = a.repo.db.ExecContext(ctx, `UPDATE usage_billing_events SET status=$3,last_error=$4,
 available_at=NOW()+make_interval(secs=>LEAST(60,POWER(2,LEAST(attempts,6)))::double precision),updated_at=NOW()
 WHERE event_type='money' AND request_id=$1 AND api_key_id=$2 AND payload IS NOT NULL AND status IN ('pending','pending_pricing','failed')`, id.RequestID, id.APIKeyID, status, message)
}

// Claim retains the existing thirty-second durable lease and SKIP LOCKED claim.
func (a *usageBillingLedger) Claim(ctx context.Context, limit int) ([]billing.Event, error) {
	if a.repo == nil || a.repo.db == nil {
		return nil, errors.New("usage billing database unavailable")
	}
	rows, err := a.repo.db.QueryContext(ctx, `WITH candidates AS (
  SELECT journal_event_id FROM usage_billing_events WHERE available_at<=NOW() AND (
   (event_type='money' AND request_id IS NOT NULL AND status IN ('pending','pending_pricing','settled','failed') AND payload IS NOT NULL)
   OR (event_type='native_intent' AND request_id IS NULL AND payload IS NULL AND status='pending_preparation'
    AND native_evidence IS NOT NULL AND proposed_request_id IS NOT NULL
    AND (($2 AND COALESCE(native_intent->'Request'->>'Protocol','')=''
      AND COALESCE(jsonb_typeof(native_evidence->'SystemOne'),'null')='null')
      OR ($3 AND native_intent->'Request'->>'Protocol'='systemone'
       AND jsonb_typeof(native_evidence->'SystemOne')='object'
       AND native_evidence->'Evidence'->>'Protocol'='systemone'))
    AND native_evidence->>'Reason' IN ('','builder_unavailable')
    AND (last_error IS NULL OR last_error='builder_unavailable')))
  ORDER BY available_at,created_at LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE usage_billing_events e SET available_at=NOW()+INTERVAL '30 seconds',attempts=e.attempts+1,updated_at=NOW()
 FROM candidates c WHERE e.journal_event_id=c.journal_event_id
 RETURNING e.event_type,e.request_id,e.operation_id,e.api_key_id,
 CASE WHEN e.event_type='money' THEN e.payload ELSE e.native_evidence END`, limit, a.repo.nativePreparation.Load() != nil, a.repo.systemOnePreparation.Load() != nil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batch := make([]billing.Event, 0, limit)
	for rows.Next() {
		var event billing.Event
		var kind string
		var requestID, operationID sql.NullString
		var keyID int64
		if err := rows.Scan(&kind, &requestID, &operationID, &keyID, &event.Payload); err != nil {
			return nil, err
		}
		switch {
		case kind == "money" && requestID.Valid && requestID.String != "" && !operationID.Valid && keyID > 0:
			event.Identity = billing.Identity{RequestID: requestID.String, APIKeyID: keyID}
		case kind == "native_intent" && !requestID.Valid && operationID.Valid && operationID.String != "" && keyID > 0:
			event.Kind = billing.NativePreparationEvent
			event.IntentKey = billing.NativeIntentKey{OperationID: operationID.String, APIKeyID: keyID}
		default:
			return nil, billing.ErrNativeIntentConflict
		}
		batch = append(batch, event)
	}
	return batch, rows.Err()
}

func (a *usageBillingLedger) Decode(event billing.Event) (billing.Command, error) {
	if event.Kind != billing.MoneyEvent || event.IntentKey != (billing.NativeIntentKey{}) {
		return nil, billing.ErrNativeIntentInvalid
	}
	var cmd service.UsageBillingCommand
	if err := json.Unmarshal(event.Payload, &cmd); err != nil {
		return nil, errors.New("invalid persisted settlement snapshot")
	}
	if cmd.PayloadVersion != 1 || cmd.BillingIdentity() != event.Identity || cmd.UsageLog == nil {
		return nil, errors.New("settlement identity mismatch")
	}
	// Restore only an explicit persisted fact; never infer completion from
	// nonzero usage or amounts. Pending pricing remains cmd.Pricing's state.
	restore := func(log *service.UsageLog) {
		if log != nil && log.BillingStatus == "lower_bound" {
			log.UsageLowerBound = true
		}
	}
	restore(cmd.UsageLog)
	for _, log := range cmd.UsageLogs {
		restore(log)
	}
	return &cmd, nil
}

func (a *usageBillingLedger) Repair(ctx context.Context, command billing.Command) error {
	cmd, err := concreteBillingCommand(command)
	if err != nil {
		return err
	}
	if a.reprice == nil {
		return billing.ErrPricingPending
	}
	return a.reprice(ctx, cmd)
}

func (a *usageBillingLedger) FreezePrice(ctx context.Context, command billing.Command) (billing.Command, error) {
	cmd, err := concreteBillingCommand(command)
	if err != nil {
		return nil, err
	}
	frozen, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	// First successful repair wins, including after a lease expires. Use the saved
	// snapshot rather than the losing worker's newly calculated price.
	var saved []byte
	err = a.repo.db.QueryRowContext(ctx, `UPDATE usage_billing_events SET payload=CASE WHEN status='pending_pricing' THEN $3::jsonb ELSE payload END,
                status=CASE WHEN status='pending_pricing' THEN 'pending' ELSE status END WHERE event_type='money' AND request_id=$1 AND api_key_id=$2 RETURNING payload`, cmd.RequestID, cmd.APIKeyID, string(frozen)).Scan(&saved)
	if err != nil || len(saved) == 0 {
		return nil, err
	}
	return a.Decode(billing.Event{Identity: cmd.BillingIdentity(), Payload: saved})
}

func (a *usageBillingLedger) Invalidate(ctx context.Context, command billing.Command) error {
	cmd, err := concreteBillingCommand(command)
	if err != nil {
		return err
	}
	return a.repo.invalidate(ctx, cmd)
}

func (a *usageBillingLedger) Acknowledge(ctx context.Context, id billing.Identity) {
	_, _ = a.repo.db.ExecContext(ctx, `UPDATE usage_billing_events SET status='applied',payload=NULL,last_error=NULL,updated_at=NOW() WHERE event_type='money' AND request_id=$1 AND api_key_id=$2 AND status='settled'`, id.RequestID, id.APIKeyID)
}

func (a *usageBillingLedger) Compact(ctx context.Context) {
	// Only completed monetary tombstones or positively proven NotSent intents
	// expire. A newly bound observation keeps its old canonical parent alive.
	// Unknown/partial/preparation-pending and ambiguous usage_recorded facts
	// never become completed merely because thirty days have passed.
	_, _ = a.repo.db.ExecContext(ctx, `WITH candidates AS (
 SELECT e.journal_event_id FROM usage_billing_events e
 WHERE e.updated_at<NOW()-INTERVAL '30 days' AND (
  (e.event_type='money' AND e.request_id IS NOT NULL AND e.status='applied'
   AND NOT EXISTS (SELECT 1 FROM usage_billing_events bps
    WHERE bps.bps_request_id=e.request_id AND bps.api_key_id=e.api_key_id
     AND (bps.status<>'usage_recorded' OR bps.updated_at>=NOW()-INTERVAL '30 days'))
   AND NOT EXISTS (SELECT 1 FROM usage_billing_events child
    WHERE child.canonical_request_id=e.request_id AND child.api_key_id=e.api_key_id
     AND (child.status<>'intent_bound' OR child.updated_at>=NOW()-INTERVAL '30 days')))
  OR (e.event_type='native_intent' AND e.status='intent_not_sent'
   AND e.native_intent_closure IS NOT NULL AND e.native_intent_closure_fingerprint IS NOT NULL)
  OR (e.event_type='bps_usage' AND e.status='not_required' AND e.bps_request_id IS NULL))
 ORDER BY e.updated_at,e.journal_event_id LIMIT 500 FOR UPDATE OF e SKIP LOCKED
 ) DELETE FROM usage_billing_events e USING candidates c WHERE e.journal_event_id=c.journal_event_id`)
}

// RecoverPendingUsageBilling is the existing host-facing boundary. The complete
// recovery workflow lives in billing.Ledger; this method only supplies its ports.
func (r *usageBillingRepository) RecoverPendingUsageBilling(ctx context.Context, limit int, reprice func(context.Context, *service.UsageBillingCommand) error) (int, error) {
	limit = min(limit, 64)
	if limit <= 0 {
		return 0, nil
	}
	if ctx == nil || r == nil || r.db == nil {
		return 0, service.ErrResponseBPSJournalUnavailable
	}
	// Share the existing bounded batch; BPS observations never create another
	// recovery worker or multiply the host's per-tick money batch allowance.
	bpsLimit := limit / 2
	bpsCompleted, bpsErr := r.recoverBPSUsageEvidence(ctx, bpsLimit)
	adapter := &usageBillingLedger{repo: r, reprice: reprice}
	if r != nil && adapter.reprice == nil {
		adapter.reprice = r.reprice
	}
	ledger := billing.Ledger{Journal: adapter, Transaction: adapter, Pricing: adapter}
	if r != nil && r.invalidate != nil {
		ledger.Invalidator = adapter
	}
	completed, err := ledger.Recover(ctx, limit-bpsLimit)
	// A bad BPS observation cannot suppress the independent monetary batch.
	// Both owners retain their failed records; report errors after valid work.
	return bpsCompleted + completed, errors.Join(bpsErr, err)
}

// Configure hooks before starting host recovery or accepting live requests.
func (r *usageBillingRepository) ConfigureUsageBillingRecovery(reprice func(context.Context, *service.UsageBillingCommand) error, invalidate func(context.Context, *service.UsageBillingCommand) error) {
	r.reprice, r.invalidate = reprice, invalidate
}

var _ service.UsageBillingRecoveryRepository = (*usageBillingRepository)(nil)
var _ billing.Journal = (*usageBillingLedger)(nil)
var _ billing.Transactor = (*usageBillingLedger)(nil)
var _ billing.Repricer = (*usageBillingLedger)(nil)
var _ billing.Invalidator = (*usageBillingLedger)(nil)
