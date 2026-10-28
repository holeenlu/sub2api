package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type nativeWSRegistrationKey struct{}
type nativeWSRegistration struct {
	purpose   model.DispatchPurpose
	operation *nativeDispatchOperation
	identity  model.PreparedIdentity
	handshake nativeWSHandshake
	live      func() bool
}

// revalidateWS is the existing operation's registered transport branch. An
// arbitrary caller-supplied ws identity cannot switch HTTP's transport/grant.
func (o *nativeDispatchOperation) revalidateWS(ctx context.Context, id model.PreparedIdentity) (execution.DispatchValidation, error) {
	if ctx == nil || o == nil {
		return execution.DispatchValidation{}, execution.ErrDispatchUnavailable
	}
	registration, ok := ctx.Value(nativeWSRegistrationKey{}).(*nativeWSRegistration)
	if !ok || registration == nil || registration.operation != o || registration.identity != id {
		return execution.DispatchValidation{}, execution.ErrInvalidPreparedIdentity
	}
	validation, err := o.authority.revalidateWS(ctx, id, registration.handshake, registration.live, o.preparedResourceCheck(id))
	if err != nil {
		return validation, nativeDispatchError("held WS grant", err)
	}
	validation.Purpose = registration.purpose
	return validation, nil
}

type nativeWSOwnedConnection interface {
	native.WSWriter
	nativeHandshake() (nativeWSHandshake, bool)
	setNativeAttempt(*nativeWSAttempt)
}

func (l *openAIWSConnLease) nativeHandshake() (nativeWSHandshake, bool) {
	conn, err := l.activeConn()
	if err != nil || conn.isClosed() || conn.unusable.Load() || !conn.isLeased() {
		return nativeWSHandshake{}, false
	}
	return conn.nativeHandshake, conn.nativeHandshake.succeeded
}

// checkNativeWSPreparation replaces the old early generation debit. Admission
// spends once at the final sealed frame. Failed dial retries use the SAME
// operation's existing TakeTurnPreparationRetry allowance at their retry sites.
func checkNativeWSPreparation(ctx context.Context, account *Account) error {
	op := nativeDispatchFromContext(ctx)
	if op == nil || op.authority == nil || account == nil {
		return execution.ErrDispatchUnavailable
	}
	// Repository and lease checks share the original first-output clock too;
	// bounding only the later writer would leave preparation waits unbounded.
	ctx, stopWait := OpenAITurnWaitContext(ctx)
	defer stopWait()
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if _, _, _, err := op.authority.loadKey(ctx); err != nil {
		return err
	}
	if _, _, err := op.authority.loadResource(ctx, account.ID); err != nil {
		return err
	}
	op.budget.mu.Lock()
	control, output, recovery := op.budget.control, op.budget.firstOutput, op.budget.recovery
	op.budget.mu.Unlock()
	if control == nil || output == nil {
		return execution.ErrDispatchUnavailable
	}
	if err := control.Err(); err != nil {
		return context.Cause(control)
	}
	snapshot := output.Snapshot()
	if !snapshot.Deadline.IsZero() && !time.Now().Before(snapshot.Deadline) {
		return execution.ErrDispatchExpired
	}
	if snapshot.Committed {
		return execution.ErrDispatchOutputCommitted
	}
	if recovery != nil {
		return recovery.beforeSend(ctx, account)
	}
	return nil
}

func (s *OpenAIGatewayService) sendNativeWS(ctx context.Context, account *Account, connection nativeWSOwnedConnection, kind coderws.MessageType, payload []byte) error {
	return s.sendNativeWSFrame(ctx, account, connection, kind, payload, false)
}

// Only the existing internal prewarm entry can select this purpose. Client
// generate:false always enters sendNativeWS and spends generation allowance.
func (s *OpenAIGatewayService) sendNativeWSPreparation(ctx context.Context, account *Account, connection nativeWSOwnedConnection, payload []byte) error {
	return s.sendNativeWSFrame(ctx, account, connection, coderws.MessageText, payload, true)
}

func (s *OpenAIGatewayService) sendNativeWSFrame(ctx context.Context, account *Account, connection nativeWSOwnedConnection, kind coderws.MessageType, payload []byte, internalPreparation bool) error {
	if err := checkNativeWSPreparation(ctx, account); err != nil {
		return &OpenAITransportAttemptError{Cause: nativeDispatchError("native WS preparation", err), SendState: model.SendNotSent}
	}
	op := nativeDispatchFromContext(ctx)
	ctx, stopWait := OpenAITurnWaitContext(ctx)
	defer stopWait()
	if connection == nil {
		return &OpenAITransportAttemptError{Cause: nativeDispatchError("held WS connection", ErrNativeDispatchGrantUnproven), SendState: model.SendNotSent}
	}
	handshake, ok := connection.nativeHandshake()
	if !ok || handshake.resourceID != account.ID {
		return &OpenAITransportAttemptError{Cause: nativeDispatchError("held WS connection", ErrNativeDispatchGrantUnproven), SendState: model.SendNotSent}
	}
	wire := payload
	// Keep the local envelope for routing/cancellation; seal only the final wire.
	if gjson.GetBytes(wire, "stream_id").Exists() {
		var err error
		wire, err = sjson.DeleteBytes(wire, "stream_id")
		if err != nil {
			return &OpenAITransportAttemptError{Cause: nativeDispatchError("native WS preparation", err), SendState: model.SendNotSent}
		}
	}
	r := op.request
	purpose := model.DispatchGeneration
	if internalPreparation {
		purpose = model.DispatchInternalPreparation
	}
	prepared, err := native.PrepareWS(ctx, native.WSSpec{MessageType: int(kind), InternalPreparation: internalPreparation, Identity: model.PreparedIdentity{Purpose: purpose, OperationID: r.OperationID, ResourceID: account.ID, Owner: handshake.owner, UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound, Mode: VendorOpenAIModeNative, Transport: "ws", GrantDigest: handshake.grant, BodyKind: model.BodyReplayable}, Target: handshake.target, Payload: wire}, connection)
	if err != nil {
		return &OpenAITransportAttemptError{Cause: nativeDispatchError("native WS preparation", err), SendState: model.SendNotSent}
	}
	id := prepared.Identity()
	registration := &nativeWSRegistration{purpose: purpose, operation: op, identity: id, handshake: handshake, live: func() bool { current, live := connection.nativeHandshake(); return live && current == handshake }}
	// Reuse the operation's actual policy registry; the per-attempt context holds
	// the connection reference only for this send, not a second process registry.
	op.mu.Lock()
	op.policies[id] = nativeDispatchWirePolicy(account)
	if op.pricingFacts == nil {
		op.pricingFacts = make(map[model.PreparedIdentity]NativeIntentModelFacts)
	}
	op.pricingFacts[id] = nativeIntentWSModelFacts(ctx, wire)
	op.mu.Unlock()
	defer func() { op.mu.Lock(); delete(op.policies, id); delete(op.pricingFacts, id); op.mu.Unlock() }()
	ctx = context.WithValue(ctx, nativeWSRegistrationKey{}, registration)
	binder, ok := op.authority.leases.(nativeDispatchLeaseBinder)
	if !ok {
		return &OpenAITransportAttemptError{Cause: ErrNativeDispatchLeaseUnproven, SendState: model.SendNotSent}
	}
	ctx, detach, err := binder.BindNativeDispatchLeases(ctx, id)
	if err != nil {
		return &OpenAITransportAttemptError{Cause: nativeDispatchError("native WS preparation", err), SendState: model.SendNotSent}
	}
	defer detach()
	ctx = withTurnRecoveryTarget(ctx, account, "websocket", "/v1/responses", handshake.target.ProxyURL, fmt.Sprintf("%x", handshake.grant), payload)
	target, _ := ctx.Value(turnRecoveryTargetKey{}).(model.AttemptTarget)
	owner := nativeHTTPFromContext(ctx)
	if owner == nil || owner.dispatch != op {
		return &OpenAITransportAttemptError{Cause: execution.ErrDispatchUnavailable, SendState: model.SendNotSent}
	}
	if err := prepareNativeWSAttempt(ctx); err != nil {
		return &OpenAITransportAttemptError{Cause: err, SendState: model.SendNotSent}
	}
	writeCtx, cancel := context.WithTimeout(ctx, s.openAIWSWriteTimeout())
	defer cancel()
	// Use the same final wire as PrepareWS. Model and billing/image decisions
	// remain producer facts; the wire slug cannot supply missing decisions.
	port := &nativeWSPreparedAttempt{owner: owner, prepared: prepared, connection: connection, target: target, state: model.SendNotSent, pricingFacts: nativeIntentWSModelFacts(ctx, wire)}
	retries := 0
	_, _, err = owner.core.OpenPrepared(writeCtx, port, &retries, func(context.Context) error { return execution.ErrDispatchUnavailable })
	if err != nil {
		return &OpenAITransportAttemptError{Cause: err, SendState: port.state}
	}
	return nil
}

// nativeWSDispatchFrameConn adds no reader or writer coordinator. It routes
// every create, including follow-up/client generate:false, through the original
// operation. Existing control frames keep their original transport owner.
type nativeWSDispatchFrameConn struct {
	inner       openaiwsv2.FrameConn
	writer      native.WSWriter
	handshake   nativeWSHandshake
	service     *OpenAIGatewayService
	account     *Account
	turnContext func(context.Context, []byte) context.Context
	closed      atomic.Bool
	attempt     atomic.Pointer[nativeWSAttempt]
}

func (c *nativeWSDispatchFrameConn) nativeHandshake() (nativeWSHandshake, bool) {
	return c.handshake, !c.closed.Load() && c.handshake.succeeded
}
func (c *nativeWSDispatchFrameConn) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	if c.closed.Load() || c.writer == nil {
		return model.SendNotSent, ErrNativeDispatchGrantUnproven
	}
	state, err := c.writer.WriteWS(ctx, write)
	if err != nil {
		c.closed.Store(true)
	}
	return state, err
}
func (c *nativeWSDispatchFrameConn) setNativeAttempt(a *nativeWSAttempt) {
	if previous := c.attempt.Swap(a); previous != nil {
		previous.close(nil)
	}
}
func (c *nativeWSDispatchFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	kind, body, err := c.inner.ReadFrame(ctx)
	err = c.attempt.Load().observe(body, err)
	if err != nil {
		c.closed.Store(true)
	}
	return kind, body, err
}
func (c *nativeWSDispatchFrameConn) WriteFrame(ctx context.Context, kind coderws.MessageType, body []byte) error {
	create, err := nativeWSIsCreate(body)
	if err != nil {
		return err
	}
	if !create {
		return c.inner.WriteFrame(ctx, kind, body)
	}
	if c.turnContext == nil {
		return execution.ErrDispatchUnavailable
	}
	turnCtx := c.turnContext(ctx, body)
	if turnCtx == nil || ctx == nil {
		return execution.ErrDispatchUnavailable
	}
	// The root context carries current leases; the relay caller can independently
	// cancel its write. Preserve BOTH lifetimes without recreating either budget.
	sendCtx, cancel := context.WithCancelCause(turnCtx)
	defer cancel(nil)
	stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	defer stop()
	if ctx.Err() != nil {
		cancel(context.Cause(ctx))
	}
	if deadline, ok := ctx.Deadline(); ok {
		bounded, end := context.WithDeadline(sendCtx, deadline)
		defer end()
		sendCtx = bounded
	}
	return c.service.sendNativeWS(sendCtx, c.account, c, kind, body)
}
func (c *nativeWSDispatchFrameConn) Close() error {
	c.closed.Store(true)
	err := c.inner.Close()
	c.attempt.Swap(nil).close(err)
	return err
}

// Reject ambiguous duplicate event types before any raw control forwarding.
// Escaped JSON property names are decoded by the same decoder as values.
func nativeWSIsCreate(body []byte) (bool, error) {
	if !json.Valid(body) {
		return false, errors.New("invalid websocket JSON payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false, native.ErrInvalidWSPreparation
	}
	var event string
	seen := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return false, err
		}
		if key == "type" {
			if seen {
				return false, native.ErrInvalidWSPreparation
			}
			seen = true
			if json.Unmarshal(value, &event) != nil {
				return false, native.ErrInvalidWSPreparation
			}
		}
	}
	return event == "response.create", nil
}

// PublishNativeWSTurn makes the authenticated root's new turn context visible
// to BOTH handler and service before capacity acquisition. The caller builds it
// from the connection/lane parent, never from the preceding canceled operation.
// Same-turn retry retains the same operation and actual budget.
type nativeWSTurnContext struct {
	ctx    context.Context
	parent context.Context
	budget *OpenAITurnBudget
}

func (s *OpenAIWSDownstream) PublishNativeWSTurn(parent, ctx context.Context) error {
	if s == nil || ctx == nil || parent == nil || nativeDispatchFromContext(parent) != nil {
		return execution.ErrDispatchUnavailable
	}
	op := nativeDispatchFromContext(ctx)
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if op == nil || budget == nil || op.budget != budget || s.budget.Load() != budget {
		return execution.ErrDispatchChanged
	}
	if previous := s.nativeTurn.Load(); previous != nil && previous.budget == budget && nativeDispatchFromContext(previous.ctx) != op {
		return execution.ErrDispatchChanged
	}
	s.nativeTurn.Store(&nativeWSTurnContext{ctx: ctx, parent: parent, budget: budget})
	return nil
}

// Local admission/contract failures are not upstream health failures. Keep
// proven-unsent transport failures distinguishable for the existing root retry
// policy; Sent/Ambiguous never authorize a replacement generation here.
func nativeWSFailureStage(stage string, err error) string {
	if IsNativeDispatchDenied(err) || errors.Is(err, native.ErrInvalidWSPreparation) || errors.Is(err, native.ErrWSAttemptOpened) || errors.Is(err, native.ErrWSWriteStarted) {
		return "before_upstream_write"
	}
	return stage
}
