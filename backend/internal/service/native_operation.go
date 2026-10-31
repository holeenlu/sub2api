package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type nativeHTTPOperationKey struct{}
type nativeHTTPOperation struct {
	core           *execution.Operation
	dispatch       *nativeDispatchOperation
	protocol       model.ClientProtocol
	maxBody        int64
	maxLine        int
	active         atomic.Uint64
	cycleErr       error
	delegated      bool
	finalized      bool
	finalizedCloud bool
	finalizeErr    error
	billingID      billing.Identity
	intent         *nativeIntentRuntime
	modeMu         sync.Mutex // mode binding survives per-cycle request contexts
	bps            *responseBPSExecution
	cloud          *cloudResponseSource
	cycleMode      string
	cycleCloud     *cloudResponseSource // retained through an authorized handback, cleared for the next forward
}

// BindNativeHTTPOperation joins the real ingress identity to the existing
// dispatcher. Non-OpenAI roots have no native dispatcher and remain delegated.
func (s *OpenAIGatewayService) BindNativeHTTPOperation(ctx context.Context, protocol model.ClientProtocol, wire [32]byte) (context.Context, error) {
	d := nativeDispatchFromContext(ctx)
	if d == nil {
		return ctx, nil
	}
	if wire == ([32]byte{}) {
		return nil, execution.ErrOperationFacts
	}
	if nativeHTTPFromContext(ctx) != nil {
		return nil, execution.ErrOperationConsumed
	}
	input := model.OperationResult{OperationID: d.request.OperationID, UserID: d.request.UserID, APIKeyID: d.request.APIKeyID, GroupID: d.request.GroupID, GroupBound: d.request.GroupBound, Protocol: protocol, OriginalWireDigest: wire}
	core, err := execution.NewOperation(d.dispatcher, input)
	if err != nil {
		return nil, err
	}
	maxLine := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLine = s.cfg.Gateway.MaxLineSize
	}
	op := &nativeHTTPOperation{core: core, dispatch: d, protocol: protocol, maxBody: resolveUpstreamResponseReadLimit(s.cfg), maxLine: maxLine}
	return context.WithValue(ctx, nativeHTTPOperationKey{}, op), nil
}

func nativeHTTPFromContext(ctx context.Context) *nativeHTTPOperation {
	if ctx == nil {
		return nil
	}
	op, _ := ctx.Value(nativeHTTPOperationKey{}).(*nativeHTTPOperation)
	return op
}

// NativeHTTPOperationBound selects the existing hold-until-return release
// policy while this operation drains usage and completes bounded finalization.
func NativeHTTPOperationBound(ctx context.Context) bool {
	return nativeHTTPFromContext(ctx) != nil
}

type NativeOperationStep = execution.CycleResult

func NativeOperationContinue() NativeOperationStep { return NativeOperationStep{Retry: true} }
func NativeOperationDone() NativeOperationStep     { return NativeOperationStep{} }

// RunNativeHTTPOperation owns selection/recovery for an HTTP operation. Each
// callback executes only one selection/forward/finalization cycle.
func RunNativeHTTPOperation(ctx context.Context, c *gin.Context, cycle func() NativeOperationStep) error {
	op := nativeHTTPFromContext(ctx)
	if op == nil {
		// Other platforms retain their established outer-loop policy. They are
		// deliberately not represented as a completed Native operation.
		for ctx.Err() == nil {
			if step := cycle(); !step.Retry {
				return step.Err
			}
		}
		return context.Cause(ctx)
	}
	// Install the existing bounded guard before observing it, so an actual
	// socket FlushError is not lost through Gin's void Flush method. Nested
	// protocol consumers reuse this guard and its cancellation owner.
	finishWrites := GuardSSEWrites(c)
	defer finishWrites()
	original := c.Writer
	c.Writer = &nativeOperationWriter{ResponseWriter: original, owner: op}
	defer func() { c.Writer = original }()
	err := op.core.Execute(ctx, func() execution.CycleResult {
		op.active.Store(0)
		op.cycleErr = nil
		op.delegated = false
		op.finalized = false
		op.finalizedCloud = false
		op.finalizeErr = nil
		op.resetForwardMode()
		step := cycle()
		cloudErr := responseCloudCompletionError(ctx)
		if cloudErr != nil {
			step.Retry = false
		}
		if nativeCloudCycle(ctx) != nil {
			op.delegated = false
		}
		step.Delegated = op.delegated
		// A retry proposal cannot discard unconfirmed usage persistence or a
		// failed finalization. Stop before Execute can select/prepare/send again.
		if op.finalizeErr != nil {
			step.Retry = false
		}
		if !step.Retry {
			step.Err = errors.Join(step.Err, op.cycleErr, op.finalizeErr, cloudErr)
			if step.Err == nil && c.Writer.Status() >= 400 {
				step.Err = fmt.Errorf("native HTTP operation returned status %d", c.Writer.Status())
			}
		}
		return step
	})
	err = errors.Join(err, op.closeNativeIntent(ctx))
	if bps := responseBPSFromContext(ctx); bps != nil {
		err = errors.Join(err, bps.controller.Close())
	}
	if err != nil {
		op.cycleErr = err
	}
	return err
}

// TrackNativeHTTPForward observes the actual consumer return, not a guessed
// usage zero or a second execution. Existing protocol converters stay in place.
func TrackNativeHTTPForward(ctx context.Context, forward func() (*OpenAIForwardResult, error)) (*OpenAIForwardResult, error) {
	if op := nativeHTTPFromContext(ctx); op != nil {
		op.resetForwardMode()
	}
	result, err := forward()
	if op := nativeHTTPFromContext(ctx); op != nil {
		op.cycleErr = err
		if responseBPSFromContext(ctx) != nil || nativeCloudCycle(ctx) != nil {
			op.delegated = false
		} else if nativeHTTPDelegatedResult(result) {
			op.delegated = true
		}
	}
	return result, err
}

func nativeHTTPDelegatedResult(result *OpenAIForwardResult) bool {
	if result == nil {
		return false
	}
	// OpenAIWSMode describes the upstream transport, not a billing owner.
	// HTTP-to-Native-WS attempts stay on this core and are finalized by the
	// HTTP root; only the actual Cloud/BPS accounting paths are delegated.
	if result.CloudManagedBilling || result.AuthorizationMode == UsageAuthorizationModeBPS || result.AuthorizationMode == UsageAuthorizationModeCloud {
		return true
	}
	for _, prior := range result.PriorAuthorizationResults {
		if nativeHTTPDelegatedResult(prior) {
			return true
		}
	}
	return false
}

// TrackNativeHTTPAuthorization observes the actual forwarding mode recorded by
// the provider path, including failures that return no ForwardResult. It cannot
// grant permission or select a mode/account.
func TrackNativeHTTPAuthorization(ctx context.Context, authorizationMode string) {
	if op := nativeHTTPFromContext(ctx); op != nil {
		op.modeMu.Lock()
		op.cycleMode = authorizationMode
		if authorizationMode == UsageAuthorizationModeCloud && op.cycleCloud == nil {
			op.cycleCloud = op.cloud
		}
		op.modeMu.Unlock()
		switch authorizationMode {
		case UsageAuthorizationModeBPS:
			op.delegated = responseBPSFromContext(ctx) == nil
		case UsageAuthorizationModeCloud:
			op.delegated = nativeCloudCycle(ctx) == nil
		}
	}
}

func (o *nativeHTTPOperation) resetForwardMode() {
	o.modeMu.Lock()
	o.cycleMode, o.cycleCloud = "", nil
	o.modeMu.Unlock()
}

// nativeCloudCycle identifies the actual owner of THIS forward, including a
// proved Cloud refusal which has handed execution back. An old Cloud pointer
// cannot exempt a later native/BPS attempt from its own monetary finalizer.
func nativeCloudCycle(ctx context.Context) *cloudResponseSource {
	op := nativeHTTPFromContext(ctx)
	if op == nil {
		return nil
	}
	op.modeMu.Lock()
	mode, current, bound := op.cycleMode, op.cycleCloud, op.cloud
	op.modeMu.Unlock()
	if mode != "" && mode != UsageAuthorizationModeCloud {
		return nil
	}
	if current != nil {
		if bound != current && !current.transferred {
			attempts := op.core.CycleResult().Attempts
			if len(attempts) > 0 && attempts[len(attempts)-1].Identity.Mode != UsageAuthorizationModeCloud {
				return nil
			}
		}
		return current
	}
	if bound == nil {
		return nil
	}
	if mode == "" {
		attempts := op.core.CycleResult().Attempts
		if len(attempts) > 0 && attempts[len(attempts)-1].Identity.Mode != UsageAuthorizationModeCloud {
			return nil
		}
	}
	return bound
}

// NativeHTTPFinalizesUsage identifies the real current attempt owner. An HTTP
// binding without any native attempt is not evidence to take over accounting.
func NativeHTTPFinalizesUsage(ctx context.Context) bool {
	op := nativeHTTPFromContext(ctx)
	if op == nil || nativeCloudCycle(ctx) != nil || op.delegated {
		return false
	}
	current := op.core.CycleResult()
	if responseBPSFromContext(ctx) != nil {
		return len(current.Attempts) > 0
	}
	for _, attempt := range current.Attempts {
		if attempt.Identity.Mode == UsageAuthorizationModeNative {
			return true
		}
	}
	return false
}

// NativeHTTPFinalizationError lets ingress stop before an existing recovery
// wait. Execute still enforces the same failure gate before another cycle.
func NativeHTTPFinalizationError(ctx context.Context) error {
	if op := nativeHTTPFromContext(ctx); op != nil {
		return op.finalizeErr
	}
	return nil
}

func NativeHTTPOutputCommitted(ctx context.Context) bool {
	if op := nativeHTTPFromContext(ctx); op != nil {
		return op.core.Committed()
	}
	return false
}

// NativeHTTPRelease retains the existing acquired handle through actual usage
// finalization. It creates no lease/renewal owner and never releases twice.
type NativeHTTPRelease struct {
	once    sync.Once
	release func()
}

func RetainNativeHTTPRelease(release func()) *NativeHTTPRelease {
	return &NativeHTTPRelease{release: release}
}
func (r *NativeHTTPRelease) Release() {
	if r != nil {
		r.once.Do(func() {
			if r.release != nil {
				r.release()
			}
		})
	}
}

type nativeProtocolReprepare struct{ reason string }

func (e *nativeProtocolReprepare) Error() string     { return "native HTTP reprepare: " + e.reason }
func requestNativeHTTPReprepare(reason string) error { return &nativeProtocolReprepare{reason: reason} }

// runNativeHTTPProtocolAttempts adapts the existing protocol decisions to the
// execution owner. It never creates another budget or issues a send permit.
func runNativeHTTPProtocolAttempts(ctx context.Context, attempt func() (*OpenAIForwardResult, error)) (*OpenAIForwardResult, error) {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil && nativeDispatchFromContext(ctx) != nil {
		return nil, execution.ErrDispatchUnavailable
	}
	if owner != nil {
		var result *OpenAIForwardResult
		err := owner.core.RunProtocolAttempts(ctx, func() execution.ProtocolAttemptResult {
			var err error
			result, err = attempt()
			var retry *nativeProtocolReprepare
			if errors.As(err, &retry) {
				return execution.ProtocolAttemptResult{ReprepareReason: retry.reason}
			}
			return execution.ProtocolAttemptResult{Err: err}
		})
		return result, err
	}
	// Non-Native/platform adapters retain their existing policy; this is not
	// represented as execution of a qualified Native HTTP operation.
	for {
		result, err := attempt()
		var retry *nativeProtocolReprepare
		if !errors.As(err, &retry) {
			return result, err
		}
		if ctx.Err() != nil {
			return result, context.Cause(ctx)
		}
	}
}
