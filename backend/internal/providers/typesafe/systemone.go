// Package typesafe implements one sealed System One attempt. Selection,
// recovery, authorization and settlement belong to the operation owner.
package typesafe

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	protocol "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	httpattempt "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// Spec contains the already encoded, destination-validated request. Shared
// HTTP sealing and the existing controlled single-send transport are reused;
// this adapter does not call the legacy service Forward/HTTPUpstream.Do path.
type Spec struct {
	HTTP   httpattempt.Spec
	Target model.AttemptTarget
}

type Attempt struct {
	prepared *httpattempt.PreparedHTTP
	target   model.AttemptTarget
	result   *httpattempt.Result
	read     bool
	mu       sync.Mutex
}

var _ execution.PreparedAttempt = (*Attempt)(nil)

func Prepare(ctx context.Context, spec Spec, transport httpattempt.Transport) (*Attempt, error) {
	if spec.HTTP.Request == nil || spec.HTTP.Request.Method != http.MethodPost ||
		spec.HTTP.Identity.Mode != "native" || spec.HTTP.Identity.Purpose != model.DispatchGeneration ||
		spec.HTTP.Identity.Transport != "http" || spec.HTTP.Transport.Profile != httpattempt.ProfileDefault ||
		spec.Target.Owner != spec.HTTP.Identity.Owner || spec.Target.Model != protocol.JevLatestModel {
		if spec.HTTP.Request != nil && spec.HTTP.Request.Body != nil {
			_ = spec.HTTP.Request.Body.Close()
		}
		return nil, execution.ErrInvalidPreparedIdentity
	}
	prepared, err := httpattempt.PrepareHTTP(ctx, spec.HTTP, transport)
	if err != nil {
		return nil, err
	}
	return &Attempt{prepared: prepared, target: spec.Target}, nil
}

func (a *Attempt) Identity() model.PreparedIdentity { return a.prepared.Identity() }
func (a *Attempt) Target() model.AttemptTarget      { return a.target }

func (a *Attempt) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	result, err := httpattempt.Open(ctx, a.prepared, permit)
	a.mu.Lock()
	a.result = result
	a.mu.Unlock()
	if result == nil {
		return model.SendUnknown, execution.PreRequestFailure{}, err
	}
	return result.SendState, result.PreRequest, err
}

// Reprepare is an explicit execution request after proven safe recovery. It
// does not send, refresh credentials, select a resource or reset any budget.
func (a *Attempt) Reprepare(ctx context.Context) (execution.PreparedAttempt, error) {
	prepared, err := httpattempt.ReprepareHTTP(ctx, a.prepared)
	if err != nil {
		return nil, err
	}
	return &Attempt{prepared: prepared, target: a.target}, nil
}

func (a *Attempt) Cancel(cause error) { a.prepared.Cancel(cause) }
func (a *Attempt) Close() error       { return a.prepared.Close() }

// Response is a single authoritative decode of the original upstream bytes.
// Non-2xx bodies are bounded error evidence, never automatically replayable.
type Response struct {
	StatusCode  int
	Headers     http.Header
	ContentType string
	Body        []byte
	Model       string
	Usage       protocol.SystemOneUsage
}

func (a *Attempt) Receive(ctx context.Context) (*Response, error) {
	a.mu.Lock()
	if a.read || a.result == nil || a.result.Response == nil || a.result.Response.Body == nil {
		a.mu.Unlock()
		return nil, execution.ErrOperationFacts
	}
	a.read = true
	response := a.result.Response
	a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	out := &Response{StatusCode: response.StatusCode, Headers: response.Header.Clone(), ContentType: JSONContentType(response.Header.Get("Content-Type"))}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, err := io.ReadAll(io.LimitReader(response.Body, protocol.MaxSystemOneResponseBytes+1))
		if len(body) > protocol.MaxSystemOneResponseBytes {
			return out, protocol.ErrSystemOneResponseTooLarge
		}
		out.Body = body
		return out, err
	}
	decoded, err := protocol.DecodeSystemOneResponse(response.Body)
	if err != nil {
		return out, err
	}
	out.Body, out.Model, out.Usage = decoded.Body, decoded.Model, decoded.ObservedUsage
	return out, nil
}

// Facts preserves unknown/partial counters and does not turn a received HTTP
// status or malformed body into a completed generation or billable zero.
func (r *Response) Facts(decodeErr error) model.FactDelta {
	if r == nil {
		return model.FactDelta{}
	}
	// x-request-id is transport correlation, not a generated response ID.
	// The root records it via Operation.RequestID; this protocol has no proven
	// response/turn identity that may be manufactured from that header.
	f := model.FactDelta{ReturnedModel: r.Model}
	if decodeErr != nil || r.StatusCode < 200 || r.StatusCode >= 300 {
		return f
	}
	f.Terminal, f.Generation, f.EndOfStream = model.TerminalCompleted, model.GenerationStarted, true
	u := &model.UsageFact{Confidence: model.UsagePartial, TerminalConfirmed: true, ReturnedModel: r.Model}
	if r.Usage.InputTokens != nil {
		u.Input = &model.TokenMeasurement{Value: *r.Usage.InputTokens}
	}
	if r.Usage.OutputTokens != nil {
		u.Output = &model.TokenMeasurement{Value: *r.Usage.OutputTokens}
	}
	if r.Usage.Complete() {
		u.Confidence = model.UsageFinal
	}
	if u.Input != nil || u.Output != nil {
		f.Usage = u
	}
	return f
}

func JSONContentType(raw string) string {
	media, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err == nil && (media == "application/json" || strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")) {
		return strings.TrimSpace(raw)
	}
	return "application/json"
}

