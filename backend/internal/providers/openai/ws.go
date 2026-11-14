package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var (
	ErrInvalidWSPreparation = errors.New("invalid native WS preparation")
	ErrWSAttemptOpened      = errors.New("native WS preparation was already opened")
	ErrWSWriteStarted       = errors.New("native WS frame write was already started")
)

// WSWriter is implemented by the existing exclusively owned connection. It
// acquires its actual message writer before calling WSWrite.Begin. Denial must
// emit no data frame, including empty frames, and release or retire that writer.
// It neither dials, retries, refreshes credentials nor creates a dispatcher.
type WSWriter interface {
	WriteWS(context.Context, *WSWrite) (model.SendState, error)
}

// WSTarget describes the successful handshake actually used by this socket.
// HandshakeDigest is supplied by its owner, never inferred from a connection ID.
type WSTarget struct {
	URL, ProxyURL   string
	HandshakeDigest [32]byte
}

// WSSpec accepts final, already bounded bytes from ingress/normalization. It
// does not read an unbounded stream or impose HTTP body limits on WS messages.
type WSSpec struct {
	// MessageType is the actual text (1) or binary (2) opcode sent upstream.
	MessageType int
	Identity    model.PreparedIdentity
	Target      WSTarget
	Payload     []byte
	// InternalPreparation is supplied only by the root's bounded prewarm path.
	// It does not authorize a different budget; the root/dispatcher must also
	// approve that purpose. Client generate:false never sets this field.
	InternalPreparation bool
}

// PreparedWS owns immutable final bytes. Copies share the one-shot state. The
// protocol/reader/lane and billing owners stay with the existing WS session.
type PreparedWS struct{ *preparedWSState }
type preparedWSState struct {
	identity    model.PreparedIdentity
	messageType int
	ctx         context.Context
	payload     []byte
	writer      WSWriter
	opened      atomic.Bool
}

func nilWSWriter(writer WSWriter) bool {
	if writer == nil {
		return true
	}
	switch v := reflect.ValueOf(writer); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func PrepareWS(ctx context.Context, spec WSSpec, writer WSWriter) (*PreparedWS, error) {
	if ctx == nil || nilWSWriter(writer) || (spec.MessageType != 1 && spec.MessageType != 2) ||
		spec.Identity.Transport != "ws" || spec.Identity.BodyKind != model.BodyReplayable ||
		spec.Identity.BodyDigest != ([32]byte{}) || spec.Identity.BodyHandle != ([16]byte{}) ||
		spec.Identity.TargetDigest != ([32]byte{}) || spec.Identity.WireDigest != ([32]byte{}) ||
		spec.Target.HandshakeDigest == ([32]byte{}) {
		return nil, ErrInvalidWSPreparation
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	target, err := url.Parse(spec.Target.URL)
	if err != nil || target.Host == "" || (target.Scheme != "ws" && target.Scheme != "wss") || target.User != nil || target.Fragment != "" {
		return nil, ErrInvalidWSPreparation
	}
	if spec.InternalPreparation != (spec.Identity.Purpose == model.DispatchInternalPreparation) {
		return nil, ErrInvalidWSPreparation
	}
	if !validWSCreate(spec.Payload, spec.InternalPreparation) {
		return nil, ErrInvalidWSPreparation
	}
	payload := bytes.Clone(spec.Payload)
	id := spec.Identity
	raw, err := json.Marshal(spec.Target)
	if err != nil {
		return nil, err
	}
	id.TargetDigest = sha256.Sum256(raw)
	id.BodyDigest = sha256.Sum256(payload)
	// Text opcode and root purpose are sealed along with the exact encoded body.
	raw, err = json.Marshal([]any{"native-ws-frame-v1", id, spec.MessageType, spec.InternalPreparation})
	if err != nil {
		return nil, err
	}
	id.WireDigest = sha256.Sum256(raw)
	if !id.Valid() {
		return nil, ErrInvalidWSPreparation
	}
	return &PreparedWS{&preparedWSState{identity: id, messageType: spec.MessageType, ctx: ctx, payload: payload, writer: writer}}, nil
}

// Ambiguous duplicate type/generate fields and the local stream envelope must
// be rejected before sealing. Otherwise one parser could authorize generate:false
// while the upstream parser executes a later duplicate generate:true.
func validWSCreate(payload []byte, preparation bool) bool {
	if !json.Valid(payload) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	var eventType string
	var seenType, seenGenerate, generateFalse bool
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return false
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return false
		}
		switch key {
		case "stream_id":
			return false
		case "type":
			if seenType || json.Unmarshal(raw, &eventType) != nil {
				return false
			}
			seenType = true
		case "generate":
			if seenGenerate {
				return false
			}
			seenGenerate = true
			generateFalse = bytes.Equal(bytes.TrimSpace(raw), []byte("false"))
		}
	}
	return eventType == "response.create" && (!preparation || (seenGenerate && generateFalse))
}

func (p *PreparedWS) Identity() model.PreparedIdentity {
	if p == nil || p.preparedWSState == nil {
		return model.PreparedIdentity{}
	}
	return p.identity
}

type WSResult struct {
	SendState       model.SendState
	AttemptSequence uint64
}

// OpenWS is a single send, not a session lifetime or a recovery loop. Both the
// original operation and caller cancellation bound writer acquisition and send.
func OpenWS(ctx context.Context, p *PreparedWS, permit *execution.SendPermit) (WSResult, error) {
	result := WSResult{SendState: model.SendNotSent}
	if ctx == nil || p == nil || p.preparedWSState == nil {
		return result, ErrInvalidWSPreparation
	}
	if !p.opened.CompareAndSwap(false, true) {
		return result, ErrWSAttemptOpened
	}
	writeCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := context.AfterFunc(p.ctx, func() { cancel(context.Cause(p.ctx)) })
	defer stop()
	if err := p.ctx.Err(); err != nil {
		return result, context.Cause(p.ctx)
	}
	if err := permit.Consume(writeCtx, p.identity); err != nil {
		return result, err
	}
	result.AttemptSequence = permit.AttemptSequence()
	if deadline := permit.SendDeadline(); !deadline.IsZero() {
		var end context.CancelFunc
		writeCtx, end = context.WithDeadline(writeCtx, deadline)
		defer end()
	}
	guard := &sendDeadlineGuard{cancel: cancel}
	guard.tighten(permit.SendDeadline())
	defer guard.stop()
	write := &WSWrite{&wsWriteState{identity: p.identity, messageType: p.messageType, payload: p.payload, permit: permit, original: p.ctx, guard: guard}}
	state, err := p.writer.WriteWS(writeCtx, write)
	result.SendState = state
	// A transport without evidence may not claim success. Unknown stays unknown.
	if err == nil && (state != model.SendSent || !write.authorized.Load()) {
		return result, ErrInvalidWSPreparation
	}
	return result, err
}

// WSWrite is created only by OpenWS. Its zero value cannot write. Begin must be
// called once, after all message-writer waits and immediately before Write.
type WSWrite struct{ *wsWriteState }

type wsWriteState struct {
	messageType int
	guard       *sendDeadlineGuard
	identity    model.PreparedIdentity
	payload     []byte
	permit      *execution.SendPermit
	original    context.Context
	begun       atomic.Bool
	authorized  atomic.Bool
}

func (w *WSWrite) MessageType() int {
	if w == nil || w.wsWriteState == nil {
		return 0
	}
	return w.messageType
}

func (w *WSWrite) Deadline() time.Time {
	if w == nil || w.wsWriteState == nil {
		return time.Time{}
	}
	return w.permit.SendDeadline()
}

func (w *WSWrite) Begin(ctx context.Context) ([]byte, error) {
	if w == nil || w.wsWriteState == nil || w.original == nil || ctx == nil {
		return nil, ErrInvalidWSPreparation
	}
	if !w.begun.CompareAndSwap(false, true) {
		return nil, ErrWSWriteStarted
	}
	if err := w.revalidate(ctx); err != nil {
		return nil, err
	}
	w.authorized.Store(true)
	return bytes.Clone(w.payload), nil
}

// RevalidateWrite is the same consumed attempt's final physical write fence.
// It grants no additional message. The controlled transport calls it under its
// real frame-write lock, after any data/control-frame wait and before bytes.
func (w *WSWrite) RevalidateWrite(ctx context.Context) error {
	if w == nil || w.wsWriteState == nil || !w.authorized.Load() {
		return ErrInvalidWSPreparation
	}
	return w.revalidate(ctx)
}

func (w *WSWrite) revalidate(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidWSPreparation
	}
	if err := w.original.Err(); err != nil {
		return context.Cause(w.original)
	}
	if err := w.permit.RevalidateConsumed(ctx, w.identity); err != nil {
		return err
	}
	w.guard.tighten(w.permit.SendDeadline())
	if err := w.original.Err(); err != nil {
		return context.Cause(w.original)
	}
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	return nil
}
