package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

type cloudResponseSource struct {
	core   *execution.Operation
	native *nativeHTTPOperation
	budget *OpenAITurnBudget
	probe  *ResponseModeHandoff
	// A source only transports commands. Its native authority resumes only
	// after the existing recovery policy and an atomic retired-owner handback.
	authority      execution.DispatchAuthority
	pending        *execution.OperationReturn
	pendingStore   OpenAICloudExecutionStore
	pendingSession string
	pendingEvents  OpenAICloudSessionStore
	pendingEventID string
	transferred    bool
}

func responseCloudFromContext(ctx context.Context) *cloudResponseSource {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil {
		return nil
	}
	owner.modeMu.Lock()
	defer owner.modeMu.Unlock()
	return owner.cloud
}
func (s *OpenAIGatewayService) bindResponseCloud(ctx context.Context) (*cloudResponseSource, error) {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil || owner.dispatch == nil || owner.core == nil || s.cloudExecutionKeys() == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	if _, ok := s.cache.(OpenAICloudExecutionStore); !ok {
		return nil, execution.ErrDispatchUnavailable
	}
	owner.modeMu.Lock()
	defer owner.modeMu.Unlock()
	if owner.cloud == nil {
		owner.cloud = &cloudResponseSource{core: owner.core, native: owner, budget: owner.dispatch.budget, authority: owner.dispatch}
	}
	owner.cycleMode, owner.cycleCloud = UsageAuthorizationModeCloud, owner.cloud
	owner.delegated = false
	return owner.cloud, nil
}

type cloudSourceAuthority struct{}

func (cloudSourceAuthority) RevalidateDispatch(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
	return execution.DispatchValidation{}, execution.ErrOperationTransferred
}

func (s *OpenAIGatewayService) newCloudProbeSource(ctx context.Context, request OpenAICloudRequest) (*cloudResponseSource, error) {
	if !request.Owner.Probe || request.Owner.UserID <= 0 || request.Owner.APIKeyID != 0 || s.userRepo == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	admin, err := s.userRepo.GetByID(ctx, request.Owner.UserID)
	if err != nil {
		return nil, err
	}
	if admin == nil || !admin.IsStaff() || admin.Status != StatusActive || admin.DeletedAt != nil {
		return nil, ErrNativeDispatchScopeChanged
	}
	operationID := uuid.NewString()
	budget := NewOpenAITurnBudget(s.cfg)
	ctx = WithOpenAITurnBudget(ctx, budget)
	dispatch, err := execution.NewDispatcher(budget.control, operationID, budget.attemptBudget, budget.firstOutput, cloudSourceAuthority{})
	if err != nil {
		return nil, err
	}
	input := model.OperationResult{OperationID: operationID, Principal: model.PrincipalAdministratorProbe, ProbeID: operationID, ProbeClaimDigest: cloudExecutionHash([]any{"cloud-admin-probe", operationID, admin.ID, request.Owner.AccountID}), UserID: admin.ID, Protocol: model.ProtocolResponses, OriginalWireDigest: sha256.Sum256(request.Body)}
	core, err := execution.NewOperation(dispatch, input)
	if err != nil {
		return nil, err
	}
	if err = core.Begin(ctx); err != nil {
		return nil, err
	}
	handoff := &ResponseModeHandoff{Request: NativeDispatchRequest{OperationID: operationID, UserID: admin.ID, RequestedModel: request.Owner.Model, Transport: "ws"}, Capacity: [2]routing.CapacityRequest{{ID: admin.ID, User: true}, {ID: request.Owner.AccountID}}}
	return &cloudResponseSource{core: core, budget: budget, probe: handoff, authority: cloudSourceAuthority{}}, nil
}

func (s *cloudResponseSource) transfer(ctx context.Context, command string, account int64) (ResponseModeHandoff, error) {
	if s.probe == nil {
		h, err := TransferCloudResponseExecution(ctx, command, account)
		if err == nil {
			s.transferred = true
		}
		return h, err
	}
	h := *s.probe
	var err error
	h.Execution, err = s.core.Transfer(ctx, command, UsageAuthorizationModeCloud, h.contextDigest())
	if err == nil {
		s.transferred = true
	}
	return h, err
}

type cloudReturnAuthority struct {
	source  *cloudResponseSource
	store   OpenAICloudExecutionStore
	session string
}

func (a *cloudReturnAuthority) ClaimOperationReturn(ctx context.Context, transfer execution.OperationTransfer, returned execution.OperationReturn) (execution.ReturnAuthorization, error) {
	if err := a.store.ClaimCloudExecutionReturn(ctx, a.session, transfer.CommandID, transfer.Digest(), returned.Digest()); err != nil {
		return execution.ReturnAuthorization{}, err
	}
	return execution.ReturnAuthorization{CommandID: transfer.CommandID, TransferDigest: transfer.Digest(), ReturnDigest: returned.Digest(), Claim: cloudExecutionHash([]any{a.session, transfer.CommandID, returned.Digest()}), Authority: a.source.authority}, nil
}

func (s *OpenAICloudSessions) cloudExecutionStore() (OpenAICloudExecutionStore, error) {
	if s.executionGateway == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	store, ok := s.executionGateway.cache.(OpenAICloudExecutionStore)
	if !ok || nativeDispatchNil(store) {
		return nil, execution.ErrDispatchUnavailable
	}
	return store, nil
}

func (s *cloudResponseSource) accept(ctx context.Context, store OpenAICloudExecutionStore, session string, h ResponseModeHandoff, event OpenAICloudStreamEvent, request OpenAICloudRequest, history bool) error {
	returned, err := store.LoadCloudExecutionReturn(ctx, session, h.Execution.CommandID, h.Execution.Digest())
	if err != nil {
		return err
	}
	if returned == nil {
		return execution.ErrOperationTransferred
	}
	if event.Execution != nil && event.Execution.Digest() != returned.Digest() {
		return execution.ErrOperationFacts
	}
	// The existing gateway still decides actual candidate failover. This only
	// returns execution to it after a proved pre-inference owner rejection.
	canRecover := (history && event.NotStarted) || (request.binding == nil && (event.NotStarted || cloudResultProvesNoInference(event.Result, request.RootRequestID)))
	if canRecover && history {
		err = s.core.RecoverTransferred(ctx, *returned, &cloudReturnAuthority{source: s, store: store, session: session})
		if err == nil {
			s.transferred = false
		}
	} else if canRecover {
		// The gateway's existing failure classifier must choose recovery first.
		// Merely receiving rejection evidence does not authorize another send.
		s.pending, s.pendingStore, s.pendingSession = returned, store, session
	} else {
		err = s.core.CompleteTransfer(*returned)
		if err == nil {
			s.transferred = false
		}
	}
	if err == nil && s.native != nil {
		s.native.delegated = false
		for i := len(returned.Result.Attempts) - 1; i >= 0; i-- {
			if returned.Result.Attempts[i].Identity.Purpose == model.DispatchGeneration {
				s.native.active.Store(returned.Result.Attempts[i].Sequence)
				break
			}
		}
	}
	return err
}

// A cancellation tombstone closes the subscriber before the owner can publish
// its terminal event. Read only that command's persisted checkpoint, with the
// same bounded time used for owner return persistence. Cancellation is never a
// recovery decision: even a proved-unsent checkpoint can only CompleteTransfer.
// A missing/failed acknowledgement leaves the source transferred and the owner
// responsible for its existing durable facts; no result or usage is invented.
func (s *cloudResponseSource) completeClosedRequest(ctx context.Context, store OpenAICloudExecutionStore, session string, h ResponseModeHandoff) error {
	if s == nil || ctx == nil || nativeDispatchNil(store) {
		return execution.ErrOperationTransferred
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := readCtx.Err(); err != nil {
			return errors.Join(execution.ErrOperationTransferred, context.Cause(readCtx))
		}
		returned, err := store.LoadCloudExecutionReturn(readCtx, session, h.Execution.CommandID, h.Execution.Digest())
		if err != nil {
			return errors.Join(execution.ErrOperationTransferred, err)
		}
		if returned != nil {
			if err := s.core.CompleteTransfer(*returned); err != nil {
				return errors.Join(execution.ErrOperationTransferred, err)
			}
			s.transferred = false
			if s.native != nil {
				s.native.delegated = false
			}
			return nil
		}
		select {
		case <-readCtx.Done():
			return errors.Join(execution.ErrOperationTransferred, context.Cause(readCtx))
		case <-ticker.C:
		}
	}
}

func (s *cloudResponseSource) recover(ctx context.Context) error {
	if s == nil || s.pending == nil {
		return execution.ErrOperationTransferred
	}
	if err := s.core.RecoverTransferred(ctx, *s.pending, &cloudReturnAuthority{source: s, store: s.pendingStore, session: s.pendingSession}); err != nil {
		return err
	}
	s.pending = nil
	s.transferred = false
	s.disposePendingEvents()
	if s.native != nil {
		s.native.modeMu.Lock()
		if s.native.cloud == s {
			s.native.cloud = nil
		}
		s.native.modeMu.Unlock()
		// The next selected native/BPS candidate owns its own real billing
		// finalizer on this SAME core; retaining a Cloud marker would skip it.
		s.native.active.Store(0)
	}
	return nil
}

func (s *cloudResponseSource) completePending() error {
	if s == nil || s.pending == nil {
		return nil
	}
	if err := s.core.CompleteTransfer(*s.pending); err != nil {
		return err
	}
	s.pending = nil
	s.transferred = false
	s.disposePendingEvents()
	return nil
}

// Normal event disposal writes the same closed tombstone used by cancellation.
// A pending owner handback retains its event stream until its atomic reclaim or
// final completion has been consumed; disposing it earlier revokes that reclaim.
func (s *cloudResponseSource) disposePendingEvents() {
	if s == nil || s.pendingEvents == nil || s.pendingEventID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.budget.control), 2*time.Second)
	defer cancel()
	_ = s.pendingEvents.DeleteCloudEvents(ctx, s.pendingEventID)
	s.pendingEvents, s.pendingEventID = nil, ""
}

// Shared HTTP/WS roots call this before their final Finish. Pending explicit
// refusals can complete without replay; a lost owner reply must never become
// a successful source completion. Public error classification stays with the
// actual forward return because errors are deliberately absent from Redis DTOs.
func responseCloudCompletionError(ctx context.Context) error {
	source := nativeCloudCycle(ctx)
	if source == nil {
		return nil
	}
	if err := source.completePending(); err != nil {
		return err
	}
	if source.transferred {
		return execution.ErrOperationTransferred
	}
	return source.core.Snapshot().Failure
}

type cloudResponseExecution struct {
	actor              *cloudSessionActor
	authority          *cloudResponseAuthority
	controller         *ResponseModeExecution
	ctx                context.Context
	active             *ResponseModeAttempt
	lastControl        uint64
	preparationStage   int
	cacheWriteObserved bool
	reasoningObserved  bool
	baseline           *OpenAICloudUsage
	baselineUnknown    bool // Fixed at send; later totals cannot prove a missing baseline.
	rootStarted        bool
	stored             bool
	storedSequence     uint64
	returned           *execution.OperationReturn
	monitorCancel      context.CancelFunc
	monitorDone        chan struct{}
	stopOnce           sync.Once
	failureSignal      chan struct{}
	failureMu          sync.Mutex
	failure            error
}

func (a *cloudSessionActor) beginCloudExecution(command OpenAICloudCommand) error {
	if a.service.executionGateway == nil {
		if command.Execution != nil {
			return execution.ErrDispatchUnavailable
		}
		return nil // Explicit low-level protocol fixtures have no gateway host.
	}
	if command.Execution == nil || command.Request.executionSource != nil {
		return execution.ErrDispatchUnavailable
	}
	if a.execution != nil && a.execution.returned == nil {
		return execution.ErrOperationConsumed
	}
	store, err := a.service.cloudExecutionStore()
	if err != nil {
		return err
	}
	h := *command.Execution
	if h.Execution.CommandID != command.RequestID || h.Request.UserID != a.record.UserID || h.Request.APIKeyID != a.record.APIKeyID || h.Request.GroupID != a.record.GroupID || h.Capacity[1].ID != a.record.AccountID || command.Request.Owner != a.record.OpenAICloudOwner {
		return execution.ErrDispatchChanged
	}
	root := command.Request.RootRequestID
	activeRoot := a.record.Status == "running" || a.record.Status == "awaiting_tools"
	if activeRoot {
		root = a.record.RootRequestID
	}
	binding := model.ModeBinding{BillingRoot: root, SessionID: a.record.SessionID, OwnerEpoch: sha256.Sum256([]byte(a.record.OwnerToken))}
	if a.record.Probe {
		binding.BillingRoot = ""
		binding.ProbeID = h.Execution.Operation.ProbeID
	}
	gate := &cloudResponseAuthority{gateway: a.service.executionGateway, store: store, claim: CloudExecutionClaim{SessionID: a.record.SessionID, OwnerToken: a.record.OwnerToken, CommandID: command.RequestID, OperationID: h.Execution.Operation.OperationID, TransferDigest: h.Execution.Digest()}, owner: a.record.OpenAICloudOwner, handoff: h, binding: binding, prepared: make(map[model.PreparedIdentity]bool)}
	ctx, controller, err := ResumeCloudResponseExecution(a.ctx, h, gate)
	if err != nil {
		return err
	}
	e := &cloudResponseExecution{actor: a, authority: gate, controller: controller, ctx: ctx, rootStarted: !activeRoot, monitorDone: make(chan struct{}), failureSignal: make(chan struct{})}
	if a.record.Usage != nil {
		copy := *a.record.Usage
		e.baseline = &copy
	}
	a.execution = e
	if a.rpc != nil {
		a.rpc.execution.Store(e)
	}
	if err := gate.revalidateCommand(ctx); err != nil {
		close(e.monitorDone)
		e.signalFailure(err)
		return err
	}
	monitor, cancel := context.WithCancel(a.ctx)
	e.monitorCancel = cancel
	a.children.Add(1)
	go func() {
		defer a.children.Done()
		defer close(e.monitorDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-monitor.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(monitor, 2*time.Second)
				err := gate.revalidateCommand(check)
				stop()
				if err != nil {
					if monitor.Err() != nil {
						return
					}
					e.signalFailure(err)
					return
				}
			}
		}
	}()
	return nil
}

func (e *cloudResponseExecution) signalFailure(err error) {
	e.controller.Cancel(err)
	e.failureMu.Lock()
	defer e.failureMu.Unlock()
	if e.failure == nil {
		e.failure = err
		close(e.failureSignal)
	}
}

func (e *cloudResponseExecution) failureCause() error {
	e.failureMu.Lock()
	defer e.failureMu.Unlock()
	return e.failure
}

func (e *cloudResponseExecution) stopMonitor() {
	if e != nil {
		e.stopOnce.Do(func() {
			if e.monitorCancel != nil {
				e.monitorCancel()
				<-e.monitorDone
			}
		})
	}
}

// Nonterminal measurements are cumulative within this attempt. Earlier client
// operations and completed sampling segments are subtracted only when their
// baseline was actually observed. No missing baseline is replaced by zero.
func (e *cloudResponseExecution) observeCurrentUsage(event *cloudRPCMessage) {
	if e == nil || e.active == nil || e.actor.record.Usage == nil || e.baselineUnknown || (!e.rootStarted && e.baseline == nil) {
		return
	}
	if event != nil && event.Method == "thread/tokenUsage/updated" && gjson.GetBytes(event.Params, "threadId").String() == e.actor.record.ThreadID && gjson.GetBytes(event.Params, "turnId").String() == e.actor.record.TurnID {
		raw := json.RawMessage(gjson.GetBytes(event.Params, "tokenUsage.total").Raw)
		if _, err := cloudParseUsage(raw); err == nil {
			e.cacheWriteObserved = gjson.GetBytes(raw, "cacheWriteInputTokens").Exists()
			e.reasoningObserved = gjson.GetBytes(raw, "reasoningOutputTokens").Exists()
		}
	}
	delta, err := cloudUsageDifference(e.actor.record.Usage, e.baseline)
	if err != nil || delta == nil {
		return
	}
	u := e.usageFact(delta, model.UsagePartial, false)
	_ = e.active.Observe(model.FactDelta{Usage: u})
}

func (e *cloudResponseExecution) usageFact(delta *OpenAICloudUsage, confidence model.UsageConfidence, confirmed bool) *model.UsageFact {
	if delta == nil || e.baselineUnknown || (!e.rootStarted && e.baseline == nil) {
		return nil
	}
	measure := func(n int) *model.TokenMeasurement { return &model.TokenMeasurement{Value: int64(n)} }
	// owner.Model is the selected target. The verified thread/start model is
	// not retained as scoped evidence here, so the core's returned model stays
	// unknown; this does not alter Cloud response projection or frozen pricing.
	u := &model.UsageFact{Input: measure(delta.InputTokens), Output: measure(delta.OutputTokens), CacheRead: measure(delta.CachedInputTokens), Total: measure(delta.TotalTokens), Confidence: confidence, TerminalConfirmed: confirmed}
	// Legacy normalized counters do not preserve optional raw-field presence.
	// Positive counts prove measurement; an explicit zero requires observing
	// its actual field and an independently known initial baseline.
	if delta.CacheWriteInputTokens > 0 || (e.rootStarted && e.baseline == nil && e.cacheWriteObserved) {
		u.CacheWrite = measure(delta.CacheWriteInputTokens)
	}
	if delta.ReasoningOutputTokens > 0 || (e.rootStarted && e.baseline == nil && e.reasoningObserved) {
		u.Reasoning = measure(delta.ReasoningOutputTokens)
	}
	return u
}

func (e *cloudResponseExecution) finish(event OpenAICloudStreamEvent) (*execution.OperationReturn, error) {
	if e.returned != nil {
		return e.returned, nil
	}
	e.stopMonitor()
	var cause error
	if event.Error != "" {
		cause = errors.New(event.Error)
	}
	result := event.Result
	if result != nil && gjson.GetBytes(result.Response, "status").String() == "failed" {
		cause = errors.New("Cloud operation failed")
	}
	if e.active != nil {
		var sent model.SendState
		var rejectionEvidence string
		for _, a := range e.controller.Snapshot().Attempts {
			if a.Sequence == e.active.Sequence() {
				sent = a.Send
				rejectionEvidence = a.RejectionEvidenceID
			}
		}
		facts := model.FactDelta{EndOfStream: sent != model.SendNotSent}
		if result != nil {
			facts.ProviderTurnID = result.TurnID
			if result.Terminal {
				if cause != nil {
					facts.Terminal = model.TerminalFailed
				} else {
					facts.Terminal = model.TerminalCompleted
				}
			}
			if result.RejectedBeforeInference {
				facts.Generation = model.GenerationRejected
				facts.RejectionEvidenceID = fmt.Sprintf("cloud-terminal:%s:%d", e.authority.claim.SessionID, e.storedSequence)
				if rejectionEvidence != "" {
					facts.RejectionEvidenceID = rejectionEvidence
				}
			}
			// A continuation with unknown earlier usage cannot attribute the whole
			// root to this attempt. Unknown remains nil, even if the root is metered.
			if result.Usage != nil && !e.baselineUnknown && (e.rootStarted || e.baseline != nil) && !result.RejectedBeforeInference {
				delta, err := cloudUsageDifference(result.Usage, e.baseline)
				if err == nil && delta != nil {
					confidence := model.UsagePartial
					if result.UsageCredible && result.Terminal {
						confidence = model.UsageFinal
					} else if e.actor.lowerBound && result.Terminal {
						confidence = model.UsageConfirmedLowerBound
					}
					facts.Usage = e.usageFact(delta, confidence, result.Terminal && !e.actor.uncertain)
				}
			}
		}
		if sent == model.SendNotSent {
			facts = model.FactDelta{}
		}
		if err := e.active.Observe(facts); err != nil {
			return nil, err
		}
	}
	if e.stored {
		var attempts []uint64
		for _, a := range e.controller.Snapshot().Attempts {
			if a.Identity.Mode == UsageAuthorizationModeCloud && a.Identity.Binding == e.authority.binding && a.Identity.Purpose == model.DispatchGeneration {
				attempts = append(attempts, a.Sequence)
			}
		}
		if len(attempts) > 0 {
			disposition := model.BillingPendingUsage
			if result != nil && result.Usage != nil {
				disposition = model.BillingUsageRecorded
			}
			receipt := model.ModeBillingReceipt{RootRequestID: e.authority.binding.BillingRoot, APIKeyID: e.authority.owner.APIKeyID, ProbeID: e.authority.binding.ProbeID, Attempts: attempts, Receipt: model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: fmt.Sprintf("cloud-session:%s:%d", e.authority.claim.SessionID, e.storedSequence)}}
			if err := e.controller.RecordBilling(receipt); err != nil {
				return nil, err
			}
		}
	}
	suspended := result != nil && result.AwaitingTools
	_ = e.controller.Finish(e.ctx, cause, suspended)
	returned, err := e.controller.core.ExportReturn()
	if err != nil {
		return nil, err
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), 3*time.Second)
	defer cancel()
	if err := e.authority.store.SaveCloudExecutionReturn(cleanup, e.authority.claim, returned); err != nil {
		return nil, err
	}
	e.returned = &returned
	return e.returned, nil
}

func (a *cloudSessionActor) publishExecutionEvent(ctx context.Context, id string, event OpenAICloudStreamEvent) error {
	e := a.execution
	if e == nil || e.authority.claim.CommandID != id {
		return a.service.store.PublishCloudEvent(ctx, id, event)
	}
	if event.Result != nil || event.Error != "" {
		returned, err := e.finish(event)
		if err != nil {
			return err
		}
		event.Execution = returned
	}
	err := a.service.store.PublishCloudEvent(ctx, id, event)
	if len(event.Event) > 0 {
		kind := gjson.GetBytes(event.Event, "type").String()
		if kind != "response.created" && kind != "response.in_progress" && kind != "response.failed" && kind != "error" && nativeClientBytesCommit(append([]byte("data: "), event.Event...), "text/event-stream") {
			// A subscriber may already have written it, even on an uncertain
			// publish acknowledgement. This is explicitly NOT a flushed receipt.
			sequence := uint64(0)
			if e.active != nil {
				sequence = e.active.Sequence()
			} else {
				sequence = e.lastControl
			}
			e.controller.core.Delivered(sequence, model.DeliveryReceipt{PossiblyCommitted: true})
			if sequence == 0 {
				e.controller.budget.firstOutput.Commit()
			}
		}
	}
	return err
}
