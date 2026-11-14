package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/tidwall/gjson"
)

type cloudRPCExecutionKey struct{}
type cloudRPCCleanupKey struct{}
type cloudRPCSendTraceKey struct{}
type cloudRPCSendTrace struct{ attempt *ResponseModeAttempt }

// Immutable encoded frames are sealed after real RPC IDs are assigned. One
// tool-output batch owns one permit; every frame revalidates it after the write
// mutex wait without taking another generation allowance.
type cloudRPCPrepared struct {
	rpc      *cloudRPC
	identity model.PreparedIdentity
	target   model.AttemptTarget
	frames   []json.RawMessage
	mu       sync.Mutex
	closed   bool
}

func (p *cloudRPCPrepared) Identity() model.PreparedIdentity { return p.identity }
func (p *cloudRPCPrepared) Target() model.AttemptTarget      { return p.target }
func (p *cloudRPCPrepared) Reprepare(context.Context) (execution.PreparedAttempt, error) {
	return nil, execution.ErrDispatchUnavailable
}
func (p *cloudRPCPrepared) Close() error {
	p.mu.Lock()
	p.closed = true
	p.frames = nil
	p.mu.Unlock()
	return nil
}
func (p *cloudRPCPrepared) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	state := model.SendNotSent
	p.mu.Lock()
	frames := p.frames
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.frames = nil; p.mu.Unlock() }()
	for index, raw := range frames {
		writeCtx, cancel := context.WithTimeout(ctx, p.rpc.writeTimeout)
		if err := p.rpc.write.Lock(writeCtx); err != nil {
			cancel()
			return state, execution.PreRequestFailure{}, err
		}
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		err := p.rpc.Err()
		if err == nil && closed {
			err = execution.ErrOperationConsumed
		}
		if err == nil {
			if index == 0 {
				err = permit.Consume(writeCtx, p.identity)
			} else {
				err = permit.RevalidateConsumed(writeCtx, p.identity)
			}
		}
		if err == nil {
			err = writeCtx.Err()
		}
		if err == nil {
			err = p.rpc.conn.WriteJSON(writeCtx, json.RawMessage(raw))
			if err != nil {
				state = model.SendAmbiguous
				err = errors.New("Cloud RPC write failed; acceptance is unknown")
				p.rpc.fail(err)
			} else {
				state = model.SendSent
			}
		}
		p.rpc.write.Unlock()
		cancel()
		if err != nil {
			return state, execution.PreRequestFailure{}, err
		}
	}
	return state, execution.PreRequestFailure{}, nil
}

func cloudRPCFrameMethod(raw json.RawMessage) (string, error) {
	if !json.Valid(raw) {
		return "", errCloudRPCProtocol
	}
	if method := gjson.GetBytes(raw, "method").String(); method != "" {
		return method, nil
	}
	if gjson.GetBytes(raw, "result").Exists() {
		if _, ok := cloudRPCIDKey(json.RawMessage(gjson.GetBytes(raw, "id").Raw)); ok {
			return "tool/reply", nil
		}
	}
	if gjson.GetBytes(raw, "error").Exists() && !gjson.GetBytes(raw, "result").Exists() {
		if _, ok := cloudRPCIDKey(json.RawMessage(gjson.GetBytes(raw, "id").Raw)); ok {
			return "server/reject", nil
		}
	}
	return "", errCloudRPCProtocol
}

// A negative response terminates an unsolicited server request; it cannot
// invoke inference. The actor consumes the actual frame from its own socket,
// fences any supplied thread/turn, and emits only the fixed rejection. Like
// interrupt, this cleanup remains available after output commitment/cancel.
func (a *cloudSessionActor) declineCloudServerRequest(event cloudRPCMessage) error {
	if a.rpc == nil || a.record == nil {
		return execution.ErrDispatchUnavailable
	}
	if _, ok := cloudRPCIDKey(event.ID); !ok {
		return errCloudRPCProtocol
	}
	thread := gjson.GetBytes(event.Params, "threadId")
	if thread.Exists() && (thread.Type != gjson.String || thread.String() == "" || thread.String() != a.record.ThreadID) {
		return execution.ErrInvalidPreparedIdentity
	}
	turn := gjson.GetBytes(event.Params, "turnId")
	if turn.Exists() && (turn.Type != gjson.String || turn.String() == "" || turn.String() != a.record.TurnID) {
		return execution.ErrInvalidPreparedIdentity
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), a.rpc.writeTimeout)
	defer cancel()
	return a.rpc.send(context.WithValue(ctx, cloudRPCCleanupKey{}, a.rpc), map[string]any{"id": append(json.RawMessage(nil), event.ID...), "error": map[string]any{"code": -32601, "message": "Sub2API does not authorize this server request"}})
}

func (e *cloudResponseExecution) send(ctx context.Context, rpc *cloudRPC, frames []json.RawMessage) (*ResponseModeAttempt, error) {
	bound, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	defer func() { stop(); cancel() }()
	if e.ctx.Err() != nil {
		cancel()
	}
	ctx = bound
	if len(frames) == 0 || e.controller == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	method, err := cloudRPCFrameMethod(frames[0])
	if err != nil {
		return nil, err
	}
	role := model.AttemptControl
	switch method {
	case "turn/start":
		role = model.AttemptGenerate
		if e.actor.compacting {
			role = model.AttemptCompact
		} else if e.actor.historyRecoveries > 0 {
			role = model.AttemptRepair
		}
	case "tool/reply":
		role = model.AttemptResume
	case "initialize", "initialized", "thread/start", "thread/inject_items", "thread/archive", "thread/compact/start":
		if method == "thread/compact/start" {
			role = model.AttemptCompact
		}
		if method == "thread/start" && e.actor.historyRecoveries > e.preparationStage && role == model.AttemptControl {
			role = model.AttemptPreparationRetry
		}
	default:
		return nil, execution.ErrInvalidPreparedIdentity
	}
	// Both callers transfer freshly encoded, privately owned frames. Hash
	// length-delimited bytes incrementally; do not re-encode/copy a 64MiB batch.
	sealed := append([]json.RawMessage(nil), frames...)
	hash := sha256.New()
	var length [8]byte
	total := 0
	for _, raw := range sealed {
		actual, err := cloudRPCFrameMethod(raw)
		if err != nil || actual != method || len(raw) > OpenAICloudMaxRPCBytes {
			return nil, execution.ErrInvalidPreparedIdentity
		}
		if len(raw) > OpenAICloudMaxRetainedBytes-total {
			return nil, execution.ErrInvalidPreparedIdentity
		}
		total += len(raw)
		binary.BigEndian.PutUint64(length[:], uint64(len(raw)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(raw)
	}
	if len(sealed) > 1 && method != "tool/reply" {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	if total > OpenAICloudMaxRetainedBytes-e.actor.retainedBytes {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	var wireDigest [32]byte
	copy(wireDigest[:], hash.Sum(nil))
	input := e.controller.Snapshot()
	owner := e.authority.owner
	account, err := e.authority.gateway.accountRepo.GetByID(ctx, owner.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	identity := model.PreparedIdentity{OperationID: input.OperationID, UserID: input.UserID, APIKeyID: input.APIKeyID, GroupID: input.GroupID, GroupBound: input.GroupBound, Principal: input.Principal, ProbeID: input.ProbeID, ProbeClaimDigest: input.ProbeClaimDigest, ResourceID: owner.AccountID, Owner: recoveryOwner(account), Mode: UsageAuthorizationModeCloud, Transport: "ws", Role: role, Binding: e.authority.binding, GrantDigest: cloudExecutionGrant(owner), BodyKind: model.BodyReplayable, BodyDigest: wireDigest, WireDigest: wireDigest, TargetDigest: cloudExecutionHash([]any{openAICloudWSURL, method, e.authority.claim.SessionID})}
	identity.Purpose, _ = role.Purpose()
	targetCtx := withTurnRecoveryTarget(e.ctx, account, UsageAuthorizationModeCloud, method, cloudAccountProxy(account), owner.GrantGeneration, identity.BodyDigest[:])
	target, _ := targetCtx.Value(turnRecoveryTargetKey{}).(model.AttemptTarget)
	prepared := &cloudRPCPrepared{rpc: rpc, identity: identity, frames: sealed, target: target}
	e.authority.mu.Lock()
	e.authority.prepared[identity] = true
	e.authority.mu.Unlock()
	attempt, err := e.controller.Register(prepared)
	if err != nil {
		return nil, err
	}
	if role == model.AttemptPreparationRetry {
		e.preparationStage = e.actor.historyRecoveries
	}
	var nextBaseline *OpenAICloudUsage
	if e.actor.record.Usage != nil {
		copy := *e.actor.record.Usage
		nextBaseline = &copy
	}
	// Only the first generation of a fresh root has a known initial zero.
	// A repair/compact/resume replacement without a measured watermark cannot
	// attribute a later whole-root cumulative total to itself.
	nextBaselineUnknown := nextBaseline == nil && (e.active != nil || !e.rootStarted)
	if identity.Purpose == model.DispatchGeneration && e.active != nil {
		e.observeCurrentUsage(nil)
	}
	err = attempt.Start(ctx)
	if identity.Purpose == model.DispatchGeneration && attempt.Sequence() != 0 {
		if e.active != nil {
			_ = e.active.Close()
		}
		e.active, e.baseline = attempt, nextBaseline
		e.baselineUnknown = nextBaselineUnknown
		e.cacheWriteObserved, e.reasoningObserved = false, false
	}
	if err == nil && role == model.AttemptResume && e.actor.record.TurnID != "" {
		_ = attempt.Observe(model.FactDelta{ProviderTurnID: e.actor.record.TurnID})
	}
	if identity.Purpose != model.DispatchGeneration && attempt.Sequence() != 0 {
		e.lastControl = attempt.Sequence()
	}
	if identity.Purpose != model.DispatchGeneration || err != nil {
		_ = attempt.Close()
	}
	return attempt, err
}

func (e *cloudResponseExecution) rpcResult(attempt *ResponseModeAttempt, method string, result json.RawMessage, err error) {
	if attempt == nil {
		return
	}
	if err != nil {
		if method == "turn/start" && e.actor.encryptedHistoryRejectedBeforeInference(err, method) {
			_ = attempt.Observe(model.FactDelta{Generation: model.GenerationRejected, RejectionEvidenceID: "cloud-rpc-rejection:" + method, Terminal: model.TerminalFailed})
		}
		_ = attempt.Close()
		return
	}
	if method == "turn/start" {
		var response struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(result, &response) == nil && response.Turn.ID != "" {
			_ = attempt.Observe(model.FactDelta{ProviderTurnID: response.Turn.ID})
		}
	}
}

func (a *cloudSessionActor) replyCloudBatch(ctx context.Context, frames []json.RawMessage) error {
	if a.execution == nil {
		if a.service != nil && a.service.executionGateway != nil {
			return execution.ErrDispatchUnavailable
		}
		for _, raw := range frames {
			var f struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(raw, &f) != nil {
				return errCloudRPCProtocol
			}
			if err := a.rpc.Reply(ctx, f.ID, f.Result); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := a.execution.send(ctx, a.rpc, frames)
	return err
}

func (a *cloudSessionActor) cleanupCloudCall(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if a.rpc == nil || (method != "turn/interrupt" && method != "thread/archive") || params["threadId"] != a.record.ThreadID {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	if method == "turn/interrupt" && params["turnId"] != a.record.TurnID {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	return a.rpc.Call(context.WithValue(ctx, cloudRPCCleanupKey{}, a.rpc), method, params)
}
