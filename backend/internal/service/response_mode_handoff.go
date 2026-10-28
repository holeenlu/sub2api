package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/tidwall/gjson"
)

// ResponseModeHandoff is the trusted Cloud command envelope. Request retains
// ORIGINAL model/IP/key digest and operation constraints; mapped owner.Model
// is not a substitute. Capacity names existing Redis members, not local lease
// pointers or a claimed deadline. It must never be exposed to client JSON/logs.
type ResponseModeHandoff struct {
	Execution execution.OperationTransfer
	Request   NativeDispatchRequest
	Capacity  [2]routing.CapacityRequest // user, then selected account
	// These are the only request facts nativeSchedulingConcurrency consumes.
	// They preserve the original admission-policy selection without serializing
	// another copy of the potentially large prompt or a mutable context object.
	CapacityQuotaChecked bool
	CapacityModel        string
}

func (h ResponseModeHandoff) contextDigest() [32]byte {
	data, err := json.Marshal(struct {
		Request      NativeDispatchRequest
		Capacity     [2]routing.CapacityRequest
		QuotaChecked bool
		Model        string
	}{h.Request, h.Capacity, h.CapacityQuotaChecked, h.CapacityModel})
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}

func (h ResponseModeHandoff) valid() bool {
	r, input := h.Request, h.Execution.Operation
	if input.Principal == model.PrincipalAdministratorProbe {
		return h.Execution.Mode == UsageAuthorizationModeCloud && h.Execution.ContextDigest == h.contextDigest() &&
			model.ValidExecutionPrincipal(input.Principal, input.UserID, input.APIKeyID, input.GroupID, input.GroupBound, input.ProbeID, input.ProbeClaimDigest) &&
			r.OperationID == input.OperationID && r.UserID == input.UserID && r.APIKeyID == 0 && r.GroupID == 0 && !r.GroupBound &&
			h.Capacity[0].User && h.Capacity[0].ID == r.UserID && !h.Capacity[1].User && h.Capacity[1].ID > 0
	}
	return h.Execution.Mode == UsageAuthorizationModeCloud && h.Execution.ContextDigest == h.contextDigest() &&
		r.OperationID == input.OperationID && r.UserID == input.UserID && r.APIKeyID == input.APIKeyID && r.GroupID == input.GroupID && r.GroupBound == input.GroupBound &&
		r.APIKeyDigest != ([32]byte{}) && r.RequestedModel != "" && r.ClientIP != "" && (r.Transport == "http" || r.Transport == "ws") &&
		h.Capacity[0].User && h.Capacity[0].ID == r.UserID && !h.Capacity[1].User && h.Capacity[1].ID > 0 &&
		(h.Capacity[0].Limit <= 0 || h.Capacity[0].Token != "") && (h.Capacity[1].Limit <= 0 || h.Capacity[1].Token != "")
}

// TransferCloudResponseExecution runs before enqueue and BEFORE the old
// TakeOpenAITurnAttempt. The source retains capacity renewal/release while it
// awaits the owner, relays cancellation, and releases only at its existing
// closeout. The remote owner checks the same Redis tokens; it never reacquires
// missing capacity or treats a missing request as unlimited admission.
func TransferCloudResponseExecution(ctx context.Context, commandID string, accountID int64) (ResponseModeHandoff, error) {
	var handoff ResponseModeHandoff
	owner := nativeHTTPFromContext(ctx)
	if ctx == nil || owner == nil || owner.core == nil || owner.dispatch == nil || owner.dispatch.authority == nil || accountID <= 0 {
		return handoff, execution.ErrDispatchUnavailable
	}
	capacity, ok := owner.dispatch.authority.leases.(*nativeDispatchCapacity)
	if !ok || capacity == nil {
		return handoff, ErrNativeDispatchLeaseUnproven
	}
	r := owner.dispatch.request
	identity := model.PreparedIdentity{ResourceID: accountID, UserID: r.UserID}
	if _, err := capacity.RevalidateNativeDispatchLeases(ctx, identity); err != nil {
		return handoff, err
	}
	user, account, err := capacity.acquisitions(identity)
	if err != nil {
		return handoff, err
	}
	handoff.Request = r
	if risk, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest); risk != nil {
		handoff.CapacityQuotaChecked = true
		handoff.CapacityModel = gjson.GetBytes(risk.body, "model").String()
	}
	for i, acquired := range []*routing.AcquireResult{user, account} {
		policy := acquired.Policy()
		ref := routing.CapacityRequest{ID: policy.ResourceID, User: policy.User, Limit: policy.Limit}
		if !acquired.Unlimited {
			if acquired.Lease == nil {
				return ResponseModeHandoff{}, ErrNativeDispatchLeaseUnproven
			}
			ref.Token = acquired.Lease.Snapshot().Identity.Token
		}
		handoff.Capacity[i] = ref
	}
	handoff.Execution, err = owner.core.Transfer(ctx, commandID, UsageAuthorizationModeCloud, handoff.contextDigest())
	return handoff, err
}

// ResponseModeOwnerAuthority must be implemented by the real owner host. Claim
// atomically verifies session owner token/lease, command ID+digest and current
// cancellation, then consumes it once. RevalidateDispatch rechecks the original
// principal (APIKeyService), mode grant, membership, both real capacity tokens
// and current owner lease at every Admit/Consume/write-wait boundary. Neither
// validateCloudSessionOwner nor a successful claim alone satisfies this port.
type ResponseModeOwnerAuthority interface {
	execution.DispatchAuthority
	ClaimResponseModeOperation(context.Context, ResponseModeHandoff) (execution.TransferClaim, error)
}

type responseModeTransferAuthority struct {
	handoff ResponseModeHandoff
	owner   ResponseModeOwnerAuthority
}

func (a *responseModeTransferAuthority) ClaimOperationTransfer(ctx context.Context, transfer execution.OperationTransfer) (execution.TransferClaim, error) {
	if transfer.Digest() != a.handoff.Execution.Digest() {
		return execution.TransferClaim{}, execution.ErrDispatchChanged
	}
	return a.owner.ClaimResponseModeOperation(ctx, a.handoff)
}
func (a *responseModeTransferAuthority) RevalidateDispatch(ctx context.Context, identity model.PreparedIdentity) (execution.DispatchValidation, error) {
	if identity.ResourceID != a.handoff.Capacity[1].ID {
		return execution.DispatchValidation{}, execution.ErrInvalidPreparedIdentity
	}
	return a.owner.RevalidateDispatch(ctx, identity)
}

// ResumeCloudResponseExecution is called once after command validation by the
// socket owner. Duplicate commands must reuse its existing controller/outcome;
// the mandatory claim rejects another resume. The same transferred budget and
// clock back both the core and OpenAITurnBudget; no new defaults are created.
func ResumeCloudResponseExecution(ctx context.Context, handoff ResponseModeHandoff, authority ResponseModeOwnerAuthority) (context.Context, *ResponseModeExecution, error) {
	if ctx == nil || !handoff.valid() || nativeDispatchNil(authority) || ResponseModeExecutionFromContext(ctx) != nil {
		return nil, nil, execution.ErrDispatchUnavailable
	}
	control, cancel := context.WithCancelCause(ctx)
	core, attempts, output, err := execution.ResumeTransferredOperation(control, handoff.Execution, &responseModeTransferAuthority{handoff, authority})
	if err != nil {
		cancel(err)
		return nil, nil, err
	}
	budget := &OpenAITurnBudget{control: control, controlCancel: cancel, attemptBudget: attempts, firstOutput: output}
	bound := WithOpenAITurnBudget(control, budget)
	controller := &ResponseModeExecution{core: core, budget: budget, mode: UsageAuthorizationModeCloud}
	return context.WithValue(bound, responseModeExecutionKey{}, controller), controller, nil
}

// VerifyCapacity checks transferred members against Redis, never the sender's
// saved TTL. The owner supplies CURRENT effective limits from the existing user
// and scheduling policy. Renewal/release remain with the source; dependency
// failure or an absent member rejects the send. Owner-session proof is separate
// and MUST be combined with this result by the required dispatch authority.
func (h ResponseModeHandoff) VerifyCapacity(ctx context.Context, store routing.CapacityLeaseStore, userLimit, accountLimit int) (NativeDispatchLeaseProof, error) {
	var denied NativeDispatchLeaseProof
	if ctx == nil || !h.valid() {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	var deadline time.Time
	for i, current := range []int{userLimit, accountLimit} {
		request := h.Capacity[i]
		if request.Limit != current {
			return denied, routing.ErrCapacityPolicyChanged
		}
		if current <= 0 {
			if request.Token != "" {
				return denied, ErrNativeDispatchLeaseUnproven
			}
			continue
		}
		if nativeDispatchNil(store) {
			return denied, ErrNativeDispatchLeaseUnproven
		}
		started := time.Now()
		remaining, err := store.CheckCapacityLease(ctx, request)
		if err != nil {
			return denied, errors.Join(ErrNativeDispatchLeaseUnproven, err)
		}
		until := started.Add(remaining)
		if remaining <= 0 || !time.Now().Before(until) {
			return denied, routing.ErrCapacityLeaseLost
		}
		deadline = nativeDispatchEarlier(deadline, until)
	}
	if err := ctx.Err(); err != nil {
		return denied, context.Cause(ctx)
	}
	data, err := json.Marshal(h.Capacity)
	if err != nil {
		return denied, err
	}
	return NativeDispatchLeaseProof{Fingerprint: sha256.Sum256(append([]byte("response-mode-owned-capacity-v1:"), data...)), ValidUntil: deadline}, nil
}
