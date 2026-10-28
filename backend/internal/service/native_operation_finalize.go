package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var (
	ErrNativeOperationBillingUnavailable = errors.New("native operation billing finalizer is unavailable")
	ErrNativeOperationBillingReceipt     = errors.New("native operation billing receipt is unconfirmed")
)

// NativeOperationFinalization supplies actual observations and the unchanged
// billing request/key identity to the existing journal/builder adapter. It is
// not a second pricing calculator or permission to settle unknown usage.
type NativeOperationFinalization struct {
	Identity         billing.Identity
	Intent           billing.NativeIntentReference
	Operation        model.OperationResult
	CurrentSequences []uint64
	UsageInput       *OpenAIRecordUsageInput
}

// NativeOperationBilling is implemented by the existing billing application
// adapter. The supplied context is already detached and bounded; implementations
// must honor it and must return only the journal's actual acknowledgement.
type NativeOperationBilling interface {
	RecordNativeOperationUsage(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error)
}

// FinalizeNativeHTTPUsage is consumed synchronously by all three HTTP roots,
// before releasing their selected account handle, including HTTP-to-Native-WS
// attempts on the same core. Bound BPS/Cloud retain their original monetary
// owners; an imported Cloud receipt is never fed into native intent pricing.
func (s *OpenAIGatewayService) FinalizeNativeHTTPUsage(ctx context.Context, input *OpenAIRecordUsageInput) (bool, error) {
	if s == nil {
		return finalizeNativeHTTPUsage(ctx, input, nil, 0)
	}
	// The existing gateway now implements the typed adapter directly. Its
	// repository/journal supplies the acknowledgement; no optional capability
	// fallback or additional Wire dependency is involved.
	return finalizeNativeHTTPUsage(ctx, input, s, usageRecordPoolOptionsFromConfig(s.cfg).TaskTimeout)
}

func finalizeNativeHTTPUsage(ctx context.Context, input *OpenAIRecordUsageInput, finalizer NativeOperationBilling, timeout time.Duration) (bool, error) {
	op := nativeHTTPFromContext(ctx)
	if op != nil && op.finalizedCloud && op.finalizeErr == nil && nativeCloudCycle(ctx) == nil {
		// A returned Cloud checkpoint only finalized that cycle. A real later
		// native/BPS candidate must execute its own unchanged finalizer.
		op.finalized, op.finalizedCloud = false, false
	}
	if bps := responseBPSFromContext(ctx); bps != nil {
		return finalizeResponseBPSUsage(ctx, input, finalizer, timeout)
	}
	if nativeCloudCycle(ctx) != nil {
		op := nativeHTTPFromContext(ctx)
		op.delegated = false
		op.finalized = true
		op.finalizedCloud = true
		op.finalizeErr = errors.Join(op.finalizeErr, responseCloudCompletionError(ctx))
		return true, op.finalizeErr
	}
	if !NativeHTTPFinalizesUsage(ctx) {
		return false, nil
	}
	current := nativeBillingOperation(op.core.CycleResult())
	if op.finalized {
		return true, op.finalizeErr
	}
	op.finalized = true
	snapshot := nativeBillingOperation(op.core.Snapshot())
	needsEvidence := false
	var liveGeneration *model.UsageFact
	sequences := make([]uint64, 0, len(current.Attempts))
	for _, attempt := range current.Attempts {
		sequences = append(sequences, attempt.Sequence)
		if attempt.Identity.Purpose == model.DispatchGeneration {
			liveGeneration = attempt.Usage
		}
	}
	for _, attempt := range snapshot.Attempts {
		needsEvidence = needsEvidence || attempt.Send != model.SendNotSent
	}
	if !needsEvidence {
		op.core.Billed(model.BillingReceipt{Disposition: model.BillingNotRequired})
		return true, nil
	}
	op.core.Billed(model.BillingReceipt{Disposition: model.BillingPendingUsage})
	var releaseReservation func()
	if reservation := InflightReservationFromContext(ctx); reservation != nil {
		releaseReservation = reservation.Acquire()
	}
	// On an unconfirmed write, retain this existing reservation reference until
	// its existing cache TTL. No new renewal worker or durable receipt is invented.
	if finalizer == nil || timeout <= 0 {
		op.finalizeErr = ErrNativeOperationBillingUnavailable
		return true, op.finalizeErr
	}
	snapshot.Failure = op.cycleErr
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	// This is the actual, once-per-cycle completion boundary. The financial
	// adapter is also a recovery consumer and therefore performs no live health
	// observation when it prepares or retries a bill.
	if gateway, ok := finalizer.(*OpenAIGatewayService); ok && liveGeneration != nil &&
		liveGeneration.Confidence == model.UsageFinal && liveGeneration.TerminalConfirmed {
		gateway.observeOpenAILiveUsage(cleanup, input)
	}
	if op.intent == nil {
		op.finalizeErr = ErrNativeOperationBillingUnavailable
		return true, op.finalizeErr
	}
	if err := op.intent.reconcileNativeIntent(cleanup); err != nil {
		op.finalizeErr = err
		return true, err
	}
	ref, _, err := nativeIntentSelectedScope(op.intent.record, snapshot, sequences)
	if err != nil {
		op.finalizeErr = err
		return true, err
	}
	if op.billingID.RequestID == "" {
		requestID := ""
		if known := op.intent.request.Scope.Billing.KnownCanonical; known != nil {
			requestID = known.RequestID
		} else if input != nil && input.Result != nil {
			// A true upstream/per-turn money ID may be learned at completion.
			// No operation ID, connection ID or generated fallback substitutes it.
			requestID = nativeIntentCanonicalID(ctx, input.Result.RequestID, op.dispatch.request.Transport == "ws")
		}
		op.billingID = billing.Identity{RequestID: requestID, APIKeyID: op.dispatch.request.APIKeyID}
	}
	result, err := finalizer.RecordNativeOperationUsage(cleanup, NativeOperationFinalization{
		Identity: op.billingID, Intent: ref, Operation: snapshot, CurrentSequences: sequences, UsageInput: input,
	})
	if cleanup.Err() != nil {
		err = errors.Join(err, context.Cause(cleanup))
	}
	if !result.Intent.Durable() || result.Intent.NativeIntentReference != ref {
		op.finalizeErr = errors.Join(err, ErrNativeOperationBillingReceipt)
		return true, op.finalizeErr
	}
	disposition := model.BillingPendingUsage
	if result.Intent.Disposition == billing.NativeIntentPendingPreparation {
		disposition = model.BillingRecoveryPending
	}
	evidenceID := result.Intent.FactsFingerprint
	if canonical := result.Canonical; canonical != nil {
		receipt := canonical.Receipt
		if !receipt.Durable() || op.billingID.RequestID == "" || receipt.Identity != op.billingID || result.Intent.Disposition != billing.NativeIntentBound {
			op.finalizeErr = errors.Join(err, ErrNativeOperationBillingReceipt)
			return true, op.finalizeErr
		}
		evidenceID = receipt.EvidenceFingerprint
		switch receipt.Disposition {
		case billing.DurablePendingUsage:
			disposition = model.BillingPendingUsage
		case billing.UsageRecorded:
			disposition = model.BillingUsageRecorded
		case billing.SettlementPending:
			disposition = model.BillingRecoveryPending
		case billing.NativePendingPricing:
			disposition = model.BillingPendingPricing
		case billing.NativeSettled:
			disposition = model.BillingSettled
		}
	} else if result.Intent.Disposition == billing.NativeIntentBound {
		// A binding ACK alone never proves the canonical transaction receipt.
		op.finalizeErr = errors.Join(err, ErrNativeOperationBillingReceipt)
		return true, op.finalizeErr
	}
	op.core.Billed(model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: evidenceID})
	if releaseReservation != nil {
		releaseReservation()
	}
	op.finalizeErr = err
	return true, err
}

// nativeBillingOperation projects the complete native journal input while
// retaining every original sequence and identity. Mode attempts stay on core
// and with their original amount owners; they are not native pricing evidence.
func nativeBillingOperation(snapshot model.OperationResult) model.OperationResult {
	attempts := make([]model.AttemptFact, 0, len(snapshot.Attempts))
	for _, attempt := range snapshot.Attempts {
		if attempt.Identity.Mode == UsageAuthorizationModeNative {
			attempts = append(attempts, attempt)
		}
	}
	snapshot.Attempts = attempts
	return snapshot
}
