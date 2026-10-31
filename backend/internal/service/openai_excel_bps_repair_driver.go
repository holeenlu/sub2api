package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// excelBPSRepairDriver owns the application side of withheld tool correction.
// It uses the original request context and the existing qualified send path;
// it never selects an account, refreshes credentials or creates a turn budget.
// Send authorization and attempt accounting stay in doExcelBPSRequest.
type excelBPSRepairDriver struct {
	service   *OpenAIGatewayService
	gateway   *gin.Context
	account   *Account
	bridge    *basispoints.Bridge
	scope     string
	token     string
	accountID string
	model     string
	prepared  []byte

	// Written by this driver, read by forwardExcelBPSAttempt only after Close
	// joins it. Keep diagnostic origin separate from the initial billing ID.
	diagnosticResponse **http.Response
	diagnosticBody     *[]byte
	diagnosticStage    *string
	observeOwnership   func(*basispoints.UpstreamFailure) bool
}

// Each accepted body has one close owner even when cancellation races a yield.
type excelBPSRepairInput struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *excelBPSRepairInput) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

type excelBPSRepairStream struct {
	cancel  context.CancelFunc
	done    chan struct{}
	events  chan excelBPSResponseEvent
	mu      sync.Mutex // active only; never held while closing or reading a body
	active  *excelBPSRepairInput
	usage   map[string]any
	failure *basispoints.UpstreamFailure
	cause   error
	readErr error                 // producer-owned, published by closing events/done
	frame   excelBPSResponseEvent // consumer-owned
	err     error                 // consumer-owned
}

type excelBPSResponseEvent struct {
	kind    string
	payload []byte
}

// Keep typed provenance out of the client text without severing errors.Is/As.
type excelBPSRepairConnectionError struct {
	message string
	cause   error
}

func (e *excelBPSRepairConnectionError) Error() string { return e.message }
func (e *excelBPSRepairConnectionError) Unwrap() error { return e.cause }

func (b *excelBPSRepairStream) closeActive() error {
	b.mu.Lock()
	active := b.active
	b.mu.Unlock()
	if active == nil {
		return nil
	}
	return active.Close()
}

func (b *excelBPSRepairStream) accept(ctx context.Context, input io.ReadCloser) *excelBPSRepairInput {
	active := &excelBPSRepairInput{ReadCloser: input}
	b.mu.Lock()
	b.active = active
	b.mu.Unlock()
	if ctx.Err() != nil {
		_ = active.Close()
	}
	return active
}

func (b *excelBPSRepairStream) Close() error {
	b.cancel()
	upstreamErr := b.closeActive()
	<-b.done
	return upstreamErr
}

// Next consumes the authoritative converter's event, without an internal wire
// encoding or parser. Heartbeats run on the output owner; no new deadline or
// send budget is created here. Close cancels and joins a blocked producer.
func (b *excelBPSRepairStream) Next(ctx context.Context, heartbeatCh <-chan time.Time, heartbeat func()) bool {
	if err := ctx.Err(); err != nil {
		b.err = err
		return false
	}
	for {
		select {
		case event, ok := <-b.events:
			if !ok {
				b.err = b.readErr
				return false
			}
			b.frame = event
			return true
		case <-ctx.Done():
			b.err = ctx.Err()
			return false
		case <-heartbeatCh:
			if heartbeat != nil {
				heartbeat()
			}
		}
	}
}

func (b *excelBPSRepairStream) Event() excelBPSResponseEvent { return b.frame }
func (b *excelBPSRepairStream) Err() error                   { return b.err }

func (b *excelBPSRepairStream) ObservedUsage() map[string]any {
	<-b.done
	return b.usage
}

func (b *excelBPSRepairStream) ObservedFailure() *basispoints.UpstreamFailure {
	<-b.done
	return b.failure
}

func (b *excelBPSRepairStream) ObservedCause() error {
	<-b.done
	return b.cause
}

func (d *excelBPSRepairDriver) stream(ctx context.Context, upstream io.ReadCloser, window []any, compactUsage map[string]any) *excelBPSRepairStream {
	ctx, cancel := context.WithCancel(ctx)
	body := &excelBPSRepairStream{cancel: cancel, done: make(chan struct{}), events: make(chan excelBPSResponseEvent)}
	input := body.accept(ctx, upstream)
	conversion := d.bridge.NewStreamConversion(func(kind string, payload []byte) error {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		// The unbuffered handoff and converter's event-size limit bound retained
		// output even when the client stalls. No per-event goroutines or copies.
		select {
		case body.events <- excelBPSResponseEvent{kind: kind, payload: payload}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, basispoints.RepairPolicy{ToolTransport: true, UnknownTarget: true})
	conversion.SetCompactedWindow(window, compactUsage)
	go func() {
		defer close(body.done)
		defer close(body.events)
		defer cancel()
		stop := context.AfterFunc(ctx, func() {
			_ = body.closeActive()
		})
		defer stop()
		demand, err := conversion.Convert(input)
		for demand != nil && err == nil {
			// Release the preceding HTTP concurrency lease before another send,
			// including accounts whose concurrency is one.
			_ = body.closeActive()
			var corrected map[string]any
			var repairErr error
			if repairErr = context.Cause(ctx); repairErr == nil {
				corrected, repairErr = d.repair(ctx, body, demand)
			}
			if repairErr == nil {
				repairErr = context.Cause(ctx)
			}
			if ctx.Err() != nil {
				repairErr = &excelBPSRepairConnectionError{
					message: "excel BPS correction canceled",
					cause:   errors.Join(ctx.Err(), context.Cause(ctx)),
				}
			}
			// Always return observed usage, even when cancellation interrupts a
			// correction. The converter decides validation, never whether to send.
			demand, err = conversion.ResolveRepair(corrected, repairErr)
		}
		if err == nil {
			err = conversion.Deliver(d.bridge.CommitReplays(conversion.Replays()))
		}
		body.usage, body.failure = conversion.Usage(), conversion.Failure()
		body.cause = conversion.Cause()
		_ = body.closeActive()
		body.readErr = err
	}()
	return body
}

func (d *excelBPSRepairDriver) repair(ctx context.Context, stream *excelBPSRepairStream, demand *basispoints.RepairDemand) (map[string]any, error) {
	var wire []byte
	var err error
	stage := "tool_repair"
	switch demand.Kind {
	case basispoints.RepairToolTransport, basispoints.RepairUnknownContinuation:
		wire, err = basispoints.BuildToolRepairRequest(d.prepared, demand.FailedResponse, demand.Validation)
	case basispoints.RepairUnknownTarget:
		stage = "tool_regeneration"
		wire, err = basispoints.RepairRequest(d.prepared)
	default:
		return nil, errors.New("invalid excel BPS repair demand")
	}
	if err != nil {
		return nil, err
	}
	// A second formatting correction includes the first executed=false feedback.
	d.prepared = wire
	*d.diagnosticBody, *d.diagnosticStage = wire, stage
	resp, _, err := d.service.doExcelBPSRequest(ctx, d.gateway, d.account, d.scope, wire, d.token, d.accountID)
	if err != nil {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		message := "excel BPS correction connection failed"
		if demand.Kind == basispoints.RepairUnknownTarget {
			message = "excel BPS tool correction transport failed"
		}
		return nil, &excelBPSRepairConnectionError{message: message, cause: err}
	}
	if resp != nil {
		*d.diagnosticResponse = resp
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("excel BPS correction returned no response body")
	}
	input := stream.accept(ctx, resp.Body)
	defer func() { _ = input.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(input, 512<<10))
		observeBPSTestResponse(ctx, resp, d.model, d.token, gjson.GetBytes(raw, "error.code").String())
		if IsUpstreamContentRefusal(raw) {
			// The application must see the original refusal before normalization;
			// no account-auth/cooldown mutation is justified by a content refusal.
			if d.bridge.OnUpstreamEvent != nil {
				d.bridge.OnUpstreamEvent(raw)
			}
			if failure := basispoints.ParseHTTPFailure(resp.StatusCode, raw); failure != nil {
				return nil, failure
			}
			return nil, &excelBPSForwardError{code: "content_policy_violation", status: excelBPSUpstreamAuthClientStatus(resp.StatusCode)}
		}
		cyber, _, _ := detectOpenAICyberPolicy(raw)
		if failure := basispoints.ParseHTTPFailure(resp.StatusCode, raw); failure != nil && (d.observeOwnership(failure) || cyber) {
			return nil, failure
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if err := d.service.observeExcelBPSRateLimit(ctx, d.account, resp.StatusCode, resp.Header, raw, d.model, basispoints.ResponsesURL); err != nil {
				return nil, newExcelBPSStateUnavailable(err, false)
			}
		}
		d.service.handleExcelBPSUnauthorized(ctx, d.account, resp.StatusCode, resp.Header, raw, d.token)
		modelDenied := isExcelBPSModelPermissionError(resp.StatusCode, gjson.GetBytes(raw, "error.code").String())
		if modelDenied {
			d.service.coolDownExcelBPSModel(d.account, d.model)
		}
		if excelBPSAccountForbidden(resp.StatusCode, raw) && !modelDenied {
			d.service.moveExcelBPSOn403(ctx, d.account)
			d.service.disableExcelBPSOn403(ctx, d.account, d.token)
		}
		if demand.Kind == basispoints.RepairUnknownTarget {
			return nil, fmt.Errorf("excel BPS tool correction returned HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("excel BPS correction returned HTTP %d", resp.StatusCode)
	}
	observeBPSTestResponse(ctx, resp, d.model, d.token, "")
	d.service.UpdateCodexUsageSnapshotFromHeaders(ctx, d.account.ID, resp.Header)
	if demand.Kind == basispoints.RepairUnknownTarget {
		return basispoints.ReadRepairResponse(input, d.bridge.OnUpstreamEvent)
	}
	return basispoints.ReadToolRepairResponse(input, d.bridge.OnUpstreamEvent)
}
