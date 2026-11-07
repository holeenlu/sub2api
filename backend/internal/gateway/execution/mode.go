package execution

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// BindModeAuthority is called by trusted composition after mode selection and
// before the first admitted send. It changes neither the operation, budget nor
// clock. The selection is irreversible: refreshing a grant, changing an account
// or disabling BPS cannot turn this operation into a new Native generation.
// The required authority must verify the sealed Role/Binding as well as current
// principal, resource, credential and owned leases. Providers cannot bind it.
func (o *Operation) BindModeAuthority(mode string, authority DispatchAuthority) error {
	if o == nil || (mode != "bps" && mode != "cloud") || nilDispatchAuthority(authority) {
		return ErrDispatchUnavailable
	}
	d := o.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mode != "" || (d.sequence != 0 && !d.recoverySelection) || d.stopped.Load() {
		return ErrDispatchChanged
	}
	if err := d.check(d.control); err != nil {
		return err
	}
	d.mode, d.authority = mode, authority
	d.recoverySelection = false
	d.authorityVersion++
	return nil
}

func (d *Dispatcher) dispatchAuthority(identity model.PreparedIdentity) (DispatchAuthority, uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mode != "" {
		purpose, valid := identity.Role.Purpose()
		if identity.Mode != d.mode || !valid || purpose != identity.Purpose || !identity.Binding.Valid(d.mode) {
			return nil, 0, ErrInvalidPreparedIdentity
		}
	}
	return d.authority, d.authorityVersion, nil
}

// OpenPreparedOnce records a real mode send through the same OpenPrepared
// machinery. Recovery remains an explicit decision of the operation owner;
// neither a callback nor a control RPC can silently retry within this call.
func (o *Operation) OpenPreparedOnce(ctx context.Context, prepared PreparedAttempt) (uint64, error) {
	if o == nil || prepared == nil {
		return 0, ErrDispatchUnavailable
	}
	retries := 1 // The existing TLS retry policy permits only count == 0.
	_, sequence, err := o.OpenPrepared(ctx, prepared, &retries, nil)
	return sequence, err
}

// RecordModeBilling binds an actual journal receipt to exact attempts. The
// journal chooses BPS's canonical ID after sending; Cloud's known root must
// match its seal. Internal execution scope cannot become a monetary identity.
func (o *Operation) RecordModeBilling(record model.ModeBillingReceipt) error {
	receipt := record.Receipt
	if o == nil || len(record.Attempts) == 0 || (receipt.Durable && receipt.EvidenceID == "") {
		return ErrOperationFacts
	}
	switch receipt.Disposition {
	case model.BillingNotRequired, model.BillingPendingUsage, model.BillingUsageRecorded,
		model.BillingPendingPricing, model.BillingSettled, model.BillingRecoveryPending:
	default:
		return ErrOperationFacts
	}
	if receipt.Disposition == model.BillingSettled && !receipt.Durable {
		return ErrOperationFacts
	}
	if record.ProbeID != "" && (record.RootRequestID != "" || record.APIKeyID != 0 || receipt.Disposition == model.BillingSettled || receipt.Disposition == model.BillingPendingPricing) {
		return ErrOperationFacts
	}
	if record.ProbeID == "" && (record.APIKeyID <= 0 || ((receipt.Disposition == model.BillingSettled || receipt.Disposition == model.BillingUsageRecorded) && record.RootRequestID == "")) {
		return ErrOperationFacts
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	seen := make(map[uint64]bool, len(record.Attempts))
	for _, sequence := range record.Attempts {
		a := o.attempt(sequence)
		if a == nil || seen[sequence] || !a.Identity.Binding.Valid(a.Identity.Mode) || a.Identity.APIKeyID != record.APIKeyID || a.Identity.Binding.ProbeID != record.ProbeID {
			return ErrOperationFacts
		}
		seen[sequence] = true
		if a.Identity.Mode == "cloud" && a.Identity.Binding.BillingRoot != record.RootRequestID {
			return ErrOperationFacts
		}
		if a.BillingRoot != "" && a.BillingRoot != record.RootRequestID {
			return ErrOperationFacts
		}
		// A delayed pending acknowledgement must not erase an actual settlement.
		if a.Billing.Disposition == model.BillingSettled && a.Billing != receipt {
			return ErrOperationFacts
		}
	}
	for sequence := range seen {
		a := o.attempt(sequence)
		a.Billing, a.BillingRoot = receipt, record.RootRequestID
	}
	o.result.Billing = modeBillingSummary(o.result.Attempts)
	return nil
}

func modeBillingSummary(attempts []model.AttemptFact) model.BillingReceipt {
	result := model.BillingReceipt{Disposition: model.BillingNotRequired}
	for _, a := range attempts {
		if a.Identity.Role == "" {
			continue
		}
		r := a.Billing
		if r.Disposition == "" {
			if (a.Send == model.SendNotSent || a.Identity.Purpose == model.DispatchControl || a.Identity.Purpose == model.DispatchInternalPreparation) && a.Usage == nil {
				continue
			}
			r.Disposition = model.BillingPendingUsage
		}
		if r.Disposition == model.BillingNotRequired {
			continue
		}
		if result.Disposition == model.BillingNotRequired {
			result = r
			continue
		}
		// A single aggregate receipt cannot name two different journal records.
		if result.EvidenceID != r.EvidenceID {
			result.EvidenceID = ""
		}
		result.Durable = result.Durable && r.Durable
		if result.Disposition == model.BillingSettled || r.Disposition == model.BillingPendingUsage || r.Disposition == model.BillingRecoveryPending {
			result.Disposition = r.Disposition
		}
	}
	return result
}

// Suspend is a terminal client-operation state, not a new provider turn or a
// new billing root. The host must first join producers and record its actual
// receipts. A later tool submission is independently admitted by its ingress.
func (o *Operation) Suspend(ctx context.Context) error {
	if o == nil || ctx == nil {
		return ErrDispatchUnavailable
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.running || o.finished || o.transfer != nil {
		return errors.Join(ErrOperationConsumed, o.result.Failure)
	}
	if err := o.dispatcher.control.Err(); err != nil {
		return context.Cause(o.dispatcher.control)
	}
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	o.dispatcher.stop()
	o.result.State, o.finished = model.OperationSuspended, true
	return nil
}
