package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Next reads the original response once, preserving bytes and extensions.
type nativeOperationStream struct {
	body      io.ReadCloser
	decode    nativeOperationDecoder
	facts     []model.FactDelta
	wire      []byte
	readErr   error
	closeOnce sync.Once
	closeErr  error
}

func (s *nativeOperationStream) Next(ctx context.Context) (model.WireFrame, model.FactDelta, error) {
	// A queued observation came from bytes already read. Drain it even after
	// cancellation; do not discard received usage or initiate another read.
	if len(s.facts) > 0 {
		return s.nextFact()
	}
	if err := ctx.Err(); err != nil {
		return model.WireFrame{}, model.FactDelta{}, context.Cause(ctx)
	}
	buf := make([]byte, 32*1024)
	n, err := s.body.Read(buf)
	buf = buf[:n]
	facts := s.decode.feed(buf, err == io.EOF)
	if len(facts) == 0 {
		return model.WireFrame{Data: buf}, model.FactDelta{}, err
	}
	// Only one 32-KiB read is pending. Completed SSE events are bounded by
	// that read plus one retained line/event; concatenated documents reuse
	// their existing count/byte cap. No read is made until this queue drains.
	s.facts, s.wire, s.readErr = facts, buf, err
	return s.nextFact()
}

func (s *nativeOperationStream) nextFact() (model.WireFrame, model.FactDelta, error) {
	fact := s.facts[0]
	s.facts[0] = model.FactDelta{}
	s.facts = s.facts[1:]
	if len(s.facts) > 0 {
		return model.WireFrame{}, fact, nil
	}
	frame, err := model.WireFrame{Data: s.wire}, s.readErr
	s.facts, s.wire, s.readErr = nil, nil, nil
	return frame, fact, err
}
func (s *nativeOperationStream) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.body.Close() })
	return s.closeErr
}

// Existing protocol consumers pull through Next; there is no second read pump.
type nativeOperationBody struct {
	stream  execution.AttemptStream
	source  *nativeOperationStream
	ctx     context.Context
	buffer  []byte
	pending error
}

func (b *nativeOperationBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.buffer) == 0 {
		if b.pending != nil {
			return 0, b.pending
		}
		frame, _, err := b.stream.Next(b.ctx)
		if errors.Is(err, execution.ErrOperationFacts) {
			// ObserveStream validates before returning, but may return the last
			// wire alongside its fact error. Never expose that invalid event to
			// the legacy converter before reporting the ownership failure.
			b.pending = err
			return 0, err
		}
		b.buffer = frame.Data
		b.pending = err
	}
	n := copy(p, b.buffer)
	b.buffer = b.buffer[n:]
	return n, nil
}
func (b *nativeOperationBody) Close() error { return b.stream.Close() }
func (b *nativeOperationBody) MarkComplete() {
	if m, ok := b.source.body.(interface{ MarkComplete() }); ok {
		m.MarkComplete()
	}
}

func nativeOperationResponseBody(ctx context.Context, owner *nativeHTTPOperation, sequence uint64, response *http.Response, family nativeUpstreamFamily) io.ReadCloser {
	s := &nativeOperationStream{body: response.Body, decode: nativeOperationDecoder{kind: family, limit: owner.maxBody, lineLimit: owner.maxLine, sse: isEventStreamResponse(response.Header)}}
	if s.decode.limit <= 0 {
		s.decode.limit = int64(defaultMaxLineSize)
	}
	if s.decode.lineLimit <= 0 {
		s.decode.lineLimit = defaultMaxLineSize
	}
	return &nativeOperationBody{stream: owner.core.ObserveStream(sequence, s), source: s, ctx: ctx}
}

type nativeOperationDecoder struct {
	imageCounter                               *openAIImageOutputCounter
	chatTerminal                               openAIRawStreamTerminalState
	chatEOF                                    bool
	chatUsageProofIncomplete                   bool
	finalUsage                                 bool
	eventKind                                  string
	skipEvent                                  bool
	limit                                      int64
	lineLimit                                  int
	sse, detected, overflow, skipLine, afterCR bool
	probe, body, line                          []byte
	frame                                      sseEventBuffer
	kind                                       nativeUpstreamFamily
	usage                                      *model.UsageFact
	terminal                                   model.TerminalState
	responseID, returnedModel                  string
}

func (d *nativeOperationDecoder) feed(raw []byte, eof bool) []model.FactDelta {
	var facts []model.FactDelta
	if !d.detected {
		d.probe = append(d.probe, raw...)
		prefix := bytes.TrimSpace(d.probe)
		if len(prefix) < 6 && !eof && !d.sse {
			return facts
		}
		d.sse = d.sse || bytes.HasPrefix(prefix, []byte("data:")) || bytes.HasPrefix(prefix, []byte("event:")) || bytes.HasPrefix(prefix, []byte(":"))
		d.detected = true
		raw = d.probe
		d.probe = nil
	}
	if !d.sse {
		if !d.overflow && int64(len(d.body)+len(raw)) <= d.limit {
			d.body = append(d.body, raw...)
		} else {
			d.overflow = true
			d.body = nil
		}
		if eof && !d.overflow {
			facts = append(facts, d.observePayload(d.body))
			d.body = nil
		}
		return facts
	}
	// CR, LF and CRLF do not change bytes returned to the established decoder.
	for len(raw) > 0 {
		if d.afterCR {
			d.afterCR = false
			if raw[0] == '\n' {
				raw = raw[1:]
				continue
			}
		}
		i := bytes.IndexAny(raw, "\r\n")
		if i < 0 {
			if !d.skipLine && len(d.line)+len(raw) <= d.lineLimit {
				d.line = append(d.line, raw...)
			} else {
				d.skipLine = true
				d.line = nil
				d.frame.reset()
				d.skipEvent = true
			}
			break
		}
		if !d.skipLine && len(d.line)+i <= d.lineLimit {
			d.line = append(d.line, raw[:i]...)
			if f, ok := d.sseLine(d.line); ok {
				facts = append(facts, f...)
			}
		} else {
			d.frame.reset()
			d.skipEvent = true
		}
		d.line = nil
		d.skipLine = false
		d.afterCR = raw[i] == '\r'
		raw = raw[i+1:]
	}
	if eof {
		// Raw Chat's existing consumer accepts a final data line at EOF. Reuse
		// its terminal proof (DONE, usage-only trailer, all observed choices),
		// without applying that exception to a truncated Responses event.
		pending := parseSSEEvent(d.frame.lines).data
		if field, value, ok := parseSSELine(string(d.line)); ok && field == "data" {
			if pending != "" {
				pending += "\n"
			}
			pending += value
		}
		root := gjson.Parse(pending)
		chatPayload := root.Get("choices").IsArray() || strings.HasPrefix(root.Get("object").String(), "chat.completion")
		chatControl := d.kind == "chat" && (strings.TrimSpace(pending) == "[DONE]" || root.Get("error").Exists() || root.Get("type").String() == "error")
		if d.kind == nativeUpstreamChat && !d.skipEvent && !d.skipLine && (chatPayload || chatControl) {
			if len(d.line) > 0 {
				_, _ = d.sseLine(d.line)
			}
			event := parseSSEEvent(d.frame.lines)
			if !d.skipEvent && event.hasData {
				d.eventKind = event.eventName
				facts = append(facts, d.eventFacts([]byte(event.data))...)
			}
		}
		d.chatEOF = true
		// Match the Responses consumers: only a blank-line-delimited event
		// is authoritative. A trailing JSON-looking terminal at EOF is still
		// an incomplete frame and cannot promote usage into a final receipt.
		d.line, d.eventKind = nil, ""
		d.frame.reset()
		if d.kind == "chat" {
			facts = append(facts, d.facts())
		}
	}
	return facts
}
func (d *nativeOperationDecoder) sseLine(line []byte) ([]model.FactDelta, bool) {
	line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
	if len(line) == 0 {
		if d.skipEvent || len(d.frame.lines) == 0 {
			d.frame.reset()
			d.eventKind, d.skipEvent = "", false
			return nil, false
		}
		// Use the same bounded frame and last-event-name/data-line semantics
		// as the real downstream consumers. A partial frame never enters here.
		event := parseSSEEvent(d.frame.lines)
		var facts []model.FactDelta
		if event.hasData {
			d.eventKind = event.eventName
			facts = d.eventFacts([]byte(event.data))
		}
		d.frame.reset()
		d.eventKind = ""
		return facts, event.hasData
	}
	if !d.skipEvent {
		if err := d.frame.addLine(string(line), d.lineLimit); err != nil {
			d.frame.reset()
			d.skipEvent = true
		}
	}
	return nil, false
}

func (d *nativeOperationDecoder) eventFacts(raw []byte) []model.FactDelta {
	if documents, ok := splitOpenAIConcatenatedJSONDocuments(raw); ok {
		facts := make([]model.FactDelta, 0, len(documents))
		for _, document := range documents {
			facts = append(facts, d.observePayload(document))
		}
		return facts
	}
	return []model.FactDelta{d.observePayload(raw)}
}

// payload is the existing direct sealed-Responses-WS observation entry. Its
// only production caller is nativeWSAttempt.observe. HTTP feed never calls it:
// HTTP must use the family explicitly carried by its actual request encoder.
func (d *nativeOperationDecoder) payload(raw []byte) model.FactDelta {
	if d.kind == nativeUpstreamUnknown {
		d.kind = nativeUpstreamResponses
	}
	if d.kind != nativeUpstreamResponses {
		return model.FactDelta{}
	}
	return d.observePayload(raw)
}

func (d *nativeOperationDecoder) observePayload(raw []byte) model.FactDelta {
	if d.kind == nativeUpstreamUnknown {
		return model.FactDelta{}
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("[DONE]")) {
		if d.kind == "chat" {
			d.chatTerminal.ObserveDataLine("[DONE]")
			return d.facts()
		}
		return model.FactDelta{}
	}
	if !gjson.ValidBytes(raw) {
		return model.FactDelta{}
	}
	root := gjson.ParseBytes(raw)
	kind := effectiveOpenAISSEEventType(raw, "")
	if d.sse {
		kind = effectiveOpenAISSEEventType(raw, d.eventKind)
	}
	r := root
	protocol := nativeUpstreamUnknown
	usageEvent := false
	switch {
	case nativeResponsesObservationEvent(kind):
		protocol = "responses"
		if response := root.Get("response"); response.IsObject() {
			r = response
		}
		// Only response lifecycle snapshots carry cumulative token usage.
		// Output events can validate their known owner fields, but do not gain
		// cumulative-usage authority from a coincidental usage key.
		usageEvent = responseswire.IsTerminalEvent(kind) || kind == "response.created" || kind == "response.queued" || kind == "response.in_progress"
	case kind == "" && (root.Get("object").String() == "response" || root.Get("object").String() == "response.compaction"):
		protocol, usageEvent = "responses", true
	case kind == "" && (strings.HasPrefix(root.Get("object").String(), "chat.completion") || root.Get("choices").IsArray()):
		protocol, usageEvent = "chat", true
	case kind == "message_start":
		r = root.Get("message")
		protocol, usageEvent = "anthropic", true
	case kind == "message_delta", kind == "message_stop", kind == "message":
		protocol, usageEvent = "anthropic", true
	case kind == "error" || (kind == "" && root.Get("error").IsObject()):
		protocol, usageEvent = d.kind, true
	default:
		return model.FactDelta{}
	}
	if protocol != d.kind {
		return model.FactDelta{}
	}
	id := ""
	if r.Get("object").Exists() || r.Get("status").Exists() || root.Get("response").IsObject() || root.Get("choices").IsArray() || kind == "message_start" || kind == "message" {
		id = r.Get("id").String()
	}
	if explicit := root.Get("response_id").String(); explicit != "" {
		id = explicit
	}
	terminal := model.TerminalUnknown
	terminalEvent := responseswire.NormalizeTerminalEvent(kind)
	if terminalEvent == "" && !d.sse && protocol == "responses" {
		terminalEvent = responseswire.NormalizeTerminalEvent("response." + r.Get("status").String())
		if r.Get("object").String() == "response.compaction" {
			terminalEvent = "response.completed"
		}
	}
	switch terminalEvent {
	case "response.completed", "response.done":
		terminal = model.TerminalCompleted
	case "response.failed":
		terminal = model.TerminalFailed
	case "response.incomplete":
		terminal = model.TerminalIncomplete
	case "response.cancelled":
		terminal = model.TerminalCanceled
	}
	if kind == "message_stop" || (!d.sse && kind == "message" && root.Get("stop_reason").Type == gjson.String && root.Get("stop_reason").String() != "") {
		terminal = model.TerminalCompleted
	}
	if !d.sse && protocol == "chat" {
		choices := root.Get("choices")
		complete := choices.IsArray() && len(choices.Array()) > 0
		for _, v := range choices.Array() {
			complete = complete && v.Get("finish_reason").Type == gjson.String && v.Get("finish_reason").String() != ""
		}
		if complete {
			terminal = model.TerminalCompleted
		}
	}
	if d.terminal != model.TerminalUnknown || (d.kind == "chat" && d.chatTerminal.sawDone) {
		// Keep validating explicit response ownership/conflicting terminals,
		// without allowing later records to rewrite the accepted snapshot.
		return model.FactDelta{ResponseID: id, Terminal: terminal}
	}
	if d.sse && d.kind == "chat" {
		d.chatTerminal.ObserveDataLine(string(raw))
	}
	if id != "" {
		d.responseID = id
	}
	if value := r.Get("model").String(); value != "" && usageEvent {
		d.returnedModel = value
	}
	var current *model.UsageFact
	auxiliaryOnly := false
	if usageEvent {
		u := r.Get("usage")
		if !u.IsObject() {
			u = root.Get("usage")
		}
		imageUsage := r.Get("tool_usage.image_gen")
		if !imageUsage.IsObject() {
			imageUsage = root.Get("tool_usage.image_gen")
		}
		current = projectNativeUsage(u, imageUsage)
		if d.kind == nativeUpstreamChat && current == nil && u.IsObject() {
			auxiliaryOnly = firstObservedOpenAIUsage(u, "total_tokens") != nil || projectOpenAIReasoning(u).observed() != nil
		}
	}
	covered := nativeUsageProjectionCovers(d.usage, current)
	if d.kind == nativeUpstreamChat {
		if current != nil {
			hasProof := current.Total != nil || current.Reasoning != nil ||
				current.HostedImageInput != nil || current.HostedImageOutput != nil ||
				(d.usage != nil && (d.usage.Total != nil || d.usage.Reasoning != nil ||
					d.usage.HostedImageInput != nil || d.usage.HostedImageOutput != nil))
			// Keep Chat's existing terminal contract, but do not certify
			// normalization using an older, incomplete measurement snapshot.
			d.chatUsageProofIncomplete = hasProof && !covered
		} else if auxiliaryOnly {
			d.chatUsageProofIncomplete = true
		}
	}
	if current != nil {
		if d.usage == nil {
			d.usage = &model.UsageFact{}
		}
		for _, pair := range []struct {
			dst      **model.TokenMeasurement
			observed *model.TokenMeasurement
		}{
			{&d.usage.Total, current.Total}, {&d.usage.Reasoning, current.Reasoning},
			{&d.usage.Input, current.Input}, {&d.usage.Output, current.Output}, {&d.usage.CacheRead, current.CacheRead},
			{&d.usage.CacheWrite, current.CacheWrite}, {&d.usage.ImageInput, current.ImageInput}, {&d.usage.ImageOutput, current.ImageOutput},
			{&d.usage.HostedImageInput, current.HostedImageInput}, {&d.usage.HostedImageOutput, current.HostedImageOutput},
		} {
			if pair.observed != nil {
				*pair.dst = pair.observed
			}
		}
	}
	images := 0
	if d.kind == nativeUpstreamResponses {
		if d.imageCounter == nil {
			d.imageCounter = newOpenAIImageOutputCounter()
		}
		if d.sse {
			imagePayload := raw
			if strings.TrimSpace(root.Get("type").String()) == "" && kind != "" {
				// Only the private observation copy changes; wire stays exact.
				if typed, err := sjson.SetBytes(raw, "type", kind); err == nil {
					imagePayload = typed
				}
			}
			d.imageCounter.AddSSEData(imagePayload)
		} else {
			d.imageCounter.AddJSONResponse(raw)
		}
		images = d.imageCounter.Count()
		if images > 0 {
			if d.usage == nil {
				d.usage = &model.UsageFact{}
			}
			observed := int64(images)
			d.usage.ImageCount = &observed
		}
	}
	if terminal != model.TerminalUnknown {
		d.terminal = terminal
		// A failed/canceled response can carry its final cumulative snapshot.
		// Earlier watermarks alone are not Final, even on completed. Anthropic
		// retains its split message_start/message_delta/message_stop contract.
		d.finalUsage = covered && (current != nil || images > 0)
		if d.kind == "anthropic" {
			d.finalUsage = d.usage != nil
		}
	}
	return d.facts()
}
func (d *nativeOperationDecoder) facts() model.FactDelta {
	f := model.FactDelta{ResponseID: d.responseID, ReturnedModel: d.returnedModel, Terminal: d.terminal}
	if d.sse && d.kind == "chat" && (d.chatEOF || d.chatTerminal.sawDone) {
		if d.chatTerminal.failure != nil {
			f.Terminal = model.TerminalFailed
		} else if d.chatTerminal.Terminated() {
			f.Terminal = model.TerminalCompleted
		}
	}
	if d.usage != nil {
		u := model.CloneUsageFact(d.usage)
		u.ResponseID = d.responseID
		u.ReturnedModel = d.returnedModel
		u.Confidence = model.UsagePartial
		u.TerminalConfirmed = f.Terminal != model.TerminalUnknown
		if d.finalUsage || (d.sse && d.kind == "chat" && f.Terminal == model.TerminalCompleted && !d.chatUsageProofIncomplete) {
			u.Confidence = model.UsageFinal
		}
		f.Usage = u
	}
	return f
}
