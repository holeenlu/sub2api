package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
		return &copy
	}
	event.UsageLog = cloneLog(cmd.UsageLog)
	event.UsageLogs = make([]*service.UsageLog, len(cmd.UsageLogs))
	for i, log := range cmd.UsageLogs {
		event.UsageLogs[i] = cloneLog(log)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(payload) > 1<<20 {
		return errors.New("usage settlement snapshot exceeds size limit")
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

func (r *usageBillingRepository) retryUsageBillingEvent(ctx context.Context, cmd *service.UsageBillingCommand, cause error) {
	status := "failed"
	if cmd.Pricing != nil && cmd.Pricing.Pending {
		status = "pending_pricing"
	}
	message := cause.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE usage_billing_events SET status=$3,last_error=$4,
 available_at=NOW()+make_interval(secs=>LEAST(60,POWER(2,LEAST(attempts,6)))::double precision),updated_at=NOW()
 WHERE request_id=$1 AND api_key_id=$2 AND status NOT IN ('applied','settled')`, cmd.RequestID, cmd.APIKeyID, status, message)
}

// Leases survive process death. The existing debit deduplication remains the
// authority when a commit succeeds but its acknowledgement is lost.
func (r *usageBillingRepository) RecoverPendingUsageBilling(ctx context.Context, limit int, reprice func(context.Context, *service.UsageBillingCommand) error) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	limit = min(limit, 64)
	rows, err := r.db.QueryContext(ctx, `WITH candidates AS (
  SELECT request_id,api_key_id FROM usage_billing_events WHERE status<>'applied' AND available_at<=NOW()
  ORDER BY available_at,created_at LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE usage_billing_events e SET available_at=NOW()+INTERVAL '30 seconds',attempts=e.attempts+1,updated_at=NOW()
 FROM candidates c WHERE e.request_id=c.request_id AND e.api_key_id=c.api_key_id
 RETURNING e.request_id,e.api_key_id,e.payload`, limit)
	if err != nil {
		return 0, err
	}
	type item struct {
		id      string
		key     int64
		payload []byte
	}
	batch := make([]item, 0, limit)
	for rows.Next() {
		var x item
		if err = rows.Scan(&x.id, &x.key, &x.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	// The authoritative dedup/archive tables retain idempotency; keep only a bounded event tombstone history.
	_, _ = r.db.ExecContext(ctx, `DELETE FROM usage_billing_events WHERE (request_id,api_key_id) IN
        (SELECT request_id,api_key_id FROM usage_billing_events WHERE status='applied' AND updated_at<NOW()-INTERVAL '30 days' ORDER BY updated_at LIMIT 500)`)
	completed := 0
	for _, x := range batch {
		var cmd service.UsageBillingCommand
		if err = json.Unmarshal(x.payload, &cmd); err != nil {
			r.retryUsageBillingEvent(ctx, &service.UsageBillingCommand{RequestID: x.id, APIKeyID: x.key}, errors.New("invalid persisted settlement snapshot"))
			continue
		}
		if cmd.PayloadVersion != 1 || cmd.RequestID != x.id || cmd.APIKeyID != x.key || cmd.UsageLog == nil {
			r.retryUsageBillingEvent(ctx, &service.UsageBillingCommand{RequestID: x.id, APIKeyID: x.key}, errors.New("settlement identity mismatch"))
			continue
		}
		if cmd.Pricing != nil && cmd.Pricing.Pending {
			if reprice == nil {
				reprice = r.reprice
			}
			if reprice == nil {
				err = service.ErrUsagePricingPending
			} else {
				err = reprice(ctx, &cmd)
			}
			if err != nil {
				r.retryUsageBillingEvent(ctx, &cmd, err)
				continue
			}
			frozen, marshalErr := json.Marshal(&cmd)
			if marshalErr != nil {
				r.retryUsageBillingEvent(ctx, &cmd, marshalErr)
				continue
			}
			// Only the first successful repair wins; concurrent/restarted workers load that same decision.
			var saved []byte
			err = r.db.QueryRowContext(ctx, `UPDATE usage_billing_events SET payload=CASE WHEN status='pending_pricing' THEN $3::jsonb ELSE payload END,
                status=CASE WHEN status='pending_pricing' THEN 'pending' ELSE status END WHERE request_id=$1 AND api_key_id=$2 RETURNING payload`, cmd.RequestID, cmd.APIKeyID, string(frozen)).Scan(&saved)
			if err != nil {
				continue
			}
			if len(saved) == 0 {
				continue
			}
			if err = json.Unmarshal(saved, &cmd); err != nil {
				continue
			}
		}
		if _, err = r.applyUsageBillingTransaction(ctx, &cmd); err != nil {
			r.retryUsageBillingEvent(ctx, &cmd, err)
			continue
		}
		r.finishUsageBillingEvent(ctx, &cmd)
		completed++
	}
	return completed, nil
}

// Compile-time contract: the same repository serves live and recovered debits.
var _ service.UsageBillingRecoveryRepository = (*usageBillingRepository)(nil)

func (r *usageBillingRepository) ConfigureUsageBillingRecovery(reprice func(context.Context, *service.UsageBillingCommand) error, invalidate func(context.Context, *service.UsageBillingCommand) error) {
	r.reprice, r.invalidate = reprice, invalidate
}

// Replaying invalidations is safe after an uncertain commit. Retain the payload until acknowledged.
func (r *usageBillingRepository) finishUsageBillingEvent(ctx context.Context, cmd *service.UsageBillingCommand) {
	if r.invalidate == nil {
		return
	}
	if err := r.invalidate(ctx, cmd); err != nil {
		return
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE usage_billing_events SET status='applied',payload=NULL,last_error=NULL,updated_at=NOW() WHERE request_id=$1 AND api_key_id=$2 AND status='settled'`, cmd.RequestID, cmd.APIKeyID)
}
