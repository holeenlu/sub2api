// Package billing owns settlement ordering and durable recovery. It does not
// perform protocol inference, choose a funding source, or issue provider calls.
package billing

import (
	"context"
	"errors"
)

// ErrPricingPending leaves durable usage waiting for a resolvable price.
var ErrPricingPending = errors.New("usage pricing is pending resolution")

// ErrUsageRequired rejects missing facts; absence is not explicit free usage.
var ErrUsageRequired = errors.New("usage billing record is required")

// Identity is the existing durable (request_id, api_key_id) pair. An operation,
// connection or retry must not substitute a different key for the same charge.
type Identity struct {
	RequestID string
	APIKeyID  int64
}

// Command supplies policy facts to the ledger without exposing service entities.
// The submitting adapter owns normalization and the frozen V1 payload. Commands
// are exclusively owned for one call; only Repricer may repair missing prices.
// HasUsageRecord does not infer usage from zero tokens: unknown Cloud evidence
// belongs to its existing durable archive, never to a zero-price command.
type Command interface {
	BillingIdentity() Identity
	NeedsPricing() bool
	HasUsageRecord() bool
}

// Event is a claimed persisted snapshot. Payload ownership transfers to the
// recovery call; it is never a closure or a reference to a request/credential.
type Event struct {
	Kind      EventKind
	IntentKey NativeIntentKey
	Identity  Identity
	Payload   []byte
}

type EventKind uint8

const (
	MoneyEvent EventKind = iota
	NativePreparationEvent
)

// Preparation is a persisted technical event. It never goes through the V1
// decoder or a Retry using an empty monetary request ID.
type NativePreparationReplayer interface {
	RecoverNativePreparation(context.Context, Event) (bool, error)
	RetryNativePreparation(context.Context, NativeIntentKey, error)
}

// Journal preserves the existing event table, lease and first-price-wins CAS.
// FreezePrice returns nil when another worker has already cleared the payload.
// Retry cannot downgrade settled/applied events. Acknowledge clears a payload
// only after the transaction and invalidation have both succeeded.
type Journal interface {
	Record(context.Context, Command) error
	Claim(context.Context, int) ([]Event, error)
	Decode(Event) (Command, error)
	FreezePrice(context.Context, Command) (Command, error)
	Retry(context.Context, Identity, bool, error)
	Acknowledge(context.Context, Identity)
	Compact(context.Context)
}

// Transactor atomically claims the existing dedup key, applies all money/quota
// effects, writes usage rows and marks the event settled. An uncertain commit
// must be retried with the same identity and fingerprint, never compensated here.
type Transactor interface {
	Commit(context.Context, Command) error
}

// Repricer repairs only missing prices under the frozen allocation policy.
// A nil error must clear NeedsPricing; it cannot turn unknown usage into zero.
type Repricer interface {
	Repair(context.Context, Command) error
}

// Invalidator acknowledges all required post-commit cache effects. Failure
// leaves a settled payload recoverable and must never cause a second debit.
type Invalidator interface {
	Invalidate(context.Context, Command) error
}

// Ledger owns live and recovered settlement ordering. Construction starts no
// goroutines. Ports must be usable implementations (not typed nils); callers
// own the context and give cancellation cleanup a bounded independent budget.
// Reserve/Release remain reservation.Handle's lifetime, not ledger refunds.
// Hosts retain that handle until settlement or durable preservation, then release
// the same holds; a request cancellation alone is not successful settlement.
type Ledger struct {
	Journal     Journal
	Transaction Transactor
	Pricing     Repricer
	Invalidator Invalidator
}

// Settle records billable evidence before the first debit. Pending pricing is
// durable but not settled. Every live command must carry a usage record;
// transaction-only money tests exercise the same private SQL transaction directly.
// Post-commit cleanup failure is recoverable and does not negate the transaction.
func (l Ledger) Settle(ctx context.Context, cmd Command) error {
	if cmd == nil || !cmd.HasUsageRecord() {
		return ErrUsageRequired
	}
	if err := l.Journal.Record(ctx, cmd); err != nil {
		return err
	}
	if cmd.NeedsPricing() {
		return ErrPricingPending
	}

	return l.settleRecorded(ctx, cmd)
}

func (l Ledger) settleRecorded(ctx context.Context, cmd Command) error {
	if err := l.Transaction.Commit(ctx, cmd); err != nil {
		l.Journal.Retry(ctx, cmd.BillingIdentity(), cmd.NeedsPricing(), err)
		return err
	}
	if l.Invalidator != nil {
		if err := l.Invalidator.Invalidate(ctx, cmd); err == nil {
			l.Journal.Acknowledge(ctx, cmd.BillingIdentity())
		}
	}
	return nil
}

// Recover claims at most 64 events using the existing durable lease. Its count
// means transactions completed (including deduplicated replays), not that cache
// cleanup finished. Invalid snapshots and unpriced facts remain durable. The
// caller owns a bounded host context; cancellation stops before the next event.
func (l Ledger) Recover(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	batch, err := l.Journal.Claim(ctx, min(limit, 64))
	if err != nil {
		return 0, err
	}
	l.Journal.Compact(ctx)
	completed := 0
	for _, event := range batch {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		if event.Kind == NativePreparationEvent {
			replayer, ok := l.Journal.(NativePreparationReplayer)
			if !ok {
				return completed, ErrNativeIntentUnavailable
			}
			done, err := replayer.RecoverNativePreparation(ctx, event)
			if err != nil {
				replayer.RetryNativePreparation(ctx, event.IntentKey, err)
			} else if done {
				completed++
			}
			continue
		}
		if event.Kind != MoneyEvent {
			return completed, ErrNativeIntentInvalid
		}
		cmd, err := l.Journal.Decode(event)
		if err != nil {
			l.Journal.Retry(ctx, event.Identity, false, err)
			continue
		}
		if cmd.NeedsPricing() {
			err = ErrPricingPending
			if l.Pricing != nil {
				err = l.Pricing.Repair(ctx, cmd)
				if err == nil && cmd.NeedsPricing() {
					err = ErrPricingPending
				}
			}
			if err != nil {
				l.Journal.Retry(ctx, event.Identity, cmd.NeedsPricing(), err)
				continue
			}
			cmd, err = l.Journal.FreezePrice(ctx, cmd)
			if err != nil || cmd == nil {
				// The lease expires for transient CAS errors; a cleared payload
				// belongs to an already acknowledged concurrent settlement.
				continue
			}
			if cmd.NeedsPricing() {
				l.Journal.Retry(ctx, event.Identity, true, ErrPricingPending)
				continue
			}
		}
		if err := l.settleRecorded(ctx, cmd); err == nil {
			completed++
		}
	}
	return completed, ctx.Err()
}
