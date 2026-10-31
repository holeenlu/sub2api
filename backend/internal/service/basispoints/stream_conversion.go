package basispoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// RepairKind identifies a protocol correction; it grants no permission to send.
type RepairKind string

const (
	RepairToolTransport       RepairKind = "tool_transport"
	RepairUnknownContinuation RepairKind = "unknown_tool_continuation"
	RepairUnknownTarget       RepairKind = "unknown_tool_target"
)

// RepairPolicy selects existing correction paths supported by the caller.
// Its zero value disables correction. It does not create an attempt budget.
type RepairPolicy struct {
	ToolTransport bool
	UnknownTarget bool
}

// RepairDemand is a withheld, unexecuted batch and its validation fact.
// The caller may build a request from FailedResponse but must not mutate it.
// No context, transport, credential, account selection or send callback is held.
type RepairDemand struct {
	Kind           RepairKind
	FailedResponse map[string]any
	Validation     error
}

// StreamConversion owns synchronous protocol state only. Convert yields at a
// repair boundary. The application owns sends, cancellation and body lifetime,
// then calls ResolveRepair with the observed response and error. Not concurrent.
type StreamConversion struct {
	bridge                *Bridge
	deliver               func(string, []byte) error
	window                *compactedStreamWindow
	policy                RepairPolicy
	sequence              int
	terminal              bool
	started               bool
	visibleContent        bool
	regenerationAttempted bool
	rejectedTool          unknownClientToolError
	hasRejectedTool       bool
	rejectedCmd           *functionCmdTransportError
	terminalResponse      object
	progressiveUsage      object
	emitted               map[string]bool
	pendingTools          map[string]bool
	failure               *UpstreamFailure
	cause                 error
	pending               *RepairDemand
	completion            object
	correction            *toolCorrection
	replays               []replayWrite
	ready                 bool
	repairStart           int
}

// NewStreamConversion constructs a converter without starting any worker.
// deliver receives one complete Responses JSON event, never SSE framing. Each
// payload has its own backing bytes and may be retained by the consumer. The
// synchronous callback supplies backpressure and must return on cancellation.
func (b *Bridge) NewStreamConversion(deliver func(string, []byte) error, policy RepairPolicy) *StreamConversion {
	c := &StreamConversion{deliver: deliver, policy: policy, emitted: make(map[string]bool), pendingTools: make(map[string]bool)}
	pure := *b
	pure.replay, pure.stagedReplays = nil, &c.replays
	c.bridge = &pure
	return c
}

// Convert reads the original SSE body through its terminal or repair demand.
// It is called once; a demand must be resolved before reading final facts.
func (c *StreamConversion) Convert(reader io.Reader) (*RepairDemand, error) {
	if c.started {
		return nil, errors.New("basispoints conversion already started")
	}
	c.started = true
	err := readEvents(reader, func(event string, data []byte) error {
		if err := c.process(event, data); err != nil {
			return protocolError{err}
		}
		if c.terminal || c.pending != nil {
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, c.finish(err)
	}
	if c.pending != nil {
		return c.pending, nil
	}
	if !c.terminal {
		return nil, ErrStreamIncomplete
	}
	return nil, nil
}

// Usage retains reported counters, including withheld correction attempts.
// Call after the driver has stopped; missing counters remain missing.
func (c *StreamConversion) Usage() map[string]any {
	usage, _ := c.terminalResponse["usage"].(object)
	usage = maxReportedUsage(nil, usage)
	if !hasImageUsage(usage) {
		// An explicitly reported zero is a fact; absent counters stay absent.
		usage = maxReportedUsage(c.progressiveUsage, usage)
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

// Failure returns the classified upstream or local protocol failure, if any.
func (c *StreamConversion) Failure() *UpstreamFailure { return c.failure }

// Cause retains the application's repair failure independently of client-safe
// protocol formatting. It grants no retry permission and is never serialized.
func (c *StreamConversion) Cause() error { return c.cause }

func (c *StreamConversion) emit(kind string, payload object) error {
	var err error
	if c.window != nil {
		err = c.window.emit(kind, payload, c.emitEvent)
	} else {
		err = c.emitEvent(kind, payload)
	}
	if err == nil && c.bridge.clientStream && hasVisibleStreamContent(kind, payload) {
		c.visibleContent = true
	}
	return err
}

func (c *StreamConversion) emitEvent(kind string, payload object) error {
	payload["type"] = kind
	payload["sequence_number"] = c.sequence
	c.sequence++
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > MaxSSEEventBytes {
		return fmt.Errorf("basispoints converted event exceeds %d MiB", MaxSSEEventBytes>>20)
	}
	return c.deliver(kind, raw)
}
func (c *StreamConversion) emitTool(item object, index any) error {
	id := text(item["id"])
	if c.emitted[id] {
		return nil
	}
	c.emitted[id] = true
	field, prefix := "arguments", "response.function_call_arguments"
	if text(item["type"]) == "custom_tool_call" {
		field, prefix = "input", "response.custom_tool_call_input"
	}
	added := make(object, len(item))
	for k, v := range item {
		added[k] = v
	}
	added[field], added["status"] = "", "in_progress"
	if err := c.emit("response.output_item.added", object{"output_index": index, "item": added}); err != nil {
		return err
	}
	if err := c.emit(prefix+".delta", object{"output_index": index, "item_id": id, "delta": item[field]}); err != nil {
		return err
	}
	if err := c.emit(prefix+".done", object{"output_index": index, "item_id": id, field: item[field]}); err != nil {
		return err
	}
	return c.emit("response.output_item.done", object{"output_index": index, "item": item})
}
func (c *StreamConversion) process(event string, data []byte) error {
	if string(data) == "[DONE]" {
		return nil
	}
	var payload object
	if decode(data, &payload) != nil || payload == nil {
		return fmt.Errorf("invalid Basispoints SSE event")
	}
	observeUpstreamEvent(c.bridge.OnUpstreamEvent, event, data, payload)
	kind := text(payload["type"])
	if kind == "" {
		kind = event
	}
	if failure := classifyUpstreamFailure(kind, payload); failure != nil {
		c.failure = failure
	}
	response, _ := payload["response"].(object)
	usage, _ := response["usage"].(object)
	if usage == nil {
		usage, _ = payload["usage"].(object)
	}
	if usage != nil {
		c.progressiveUsage = maxReportedUsage(c.progressiveUsage, usage)
	}
	if kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed" || kind == "response.cancelled" {
		c.terminalResponse = response
	}
	if c.bridge.structured != nil && kind == "response.completed" {
		if response, ok := payload["response"].(object); !ok || response == nil {
			return fmt.Errorf("basispoints structured output is missing its terminal response")
		}
	}
	if isToolEvent(kind) {
		return nil
	}
	item, _ := payload["item"].(object)
	if c.bridge.structured != nil && isStructuredMessageEvent(kind, item) {
		return nil
	}
	if kind == "response.output_item.added" && isTool(item) {
		return nil
	}
	if kind == "response.output_item.done" && isTool(item) {
		// Only the terminal response contains the authoritative native item.
		// Text keeps streaming; tool calls wait until the whole response validates.
		if len(c.pendingTools) >= 1024 {
			return fmt.Errorf("basispoints response contains too many tool items")
		}
		c.pendingTools[text(item["call_id"])+"\x00"+text(item["id"])] = true
		return nil
	}
	if response, ok := payload["response"].(object); ok {
		if c.bridge.structured != nil {
			config, _ := response["text"].(object)
			if config == nil {
				config = make(object)
			}
			config["format"] = c.bridge.structured.format
			response["text"] = config
		}
		if kind == "response.completed" {
			c.terminalResponse = response
			if c.bridge.structured != nil {
				if err := c.bridge.structured.validate(response); err != nil {
					return err
				}
			}
			output, _ := response["output"].([]any)
			for _, raw := range output {
				item, _ := raw.(object)
				if isTool(item) {
					delete(c.pendingTools, text(item["call_id"])+"\x00"+text(item["id"]))
				}
			}
			if len(c.pendingTools) != 0 {
				return fmt.Errorf("basispoints completed response omitted an original tool item")
			}
			return c.complete(payload, response)
		} else {
			// Never expose native or incomplete tool arguments to the client.
			output, _ := response["output"].([]any)
			filtered := make([]any, 0, len(output))
			for _, raw := range output {
				item, _ := raw.(object)
				if !isTool(item) && (c.bridge.structured == nil || text(item["type"]) != "message") {
					filtered = append(filtered, raw)
				}
			}
			response["output"] = filtered
			response["reasoning"] = object{"effort": c.bridge.Effort}
		}
	}
	c.terminal = kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed" || kind == "response.cancelled" || kind == "error"
	return c.emit(kind, normalizeFailureEvent(kind, payload))
}

func (c *StreamConversion) complete(payload, response object) error {
	b := c.bridge
	validation := b.validateToolResponse(response)
	c.hasRejectedTool = errors.As(validation, &c.rejectedTool)
	c.rejectedCmd = unknownFunctionCmdTarget(validation)
	c.completion = payload
	switch {
	case c.policy.ToolTransport && b.unknownContinuationBlockReason(response, validation, c.visibleContent) == "":
		if c.visibleContent {
			return validation
		}
		if c.rejectedCmd != nil {
			c.rejectedCmd.attempt = 1
		}
		c.pending = &RepairDemand{Kind: RepairUnknownContinuation, FailedResponse: response, Validation: validation}
	case c.policy.UnknownTarget && b.canRepair(response, validation):
		if c.visibleContent {
			return validation
		}
		c.pending = &RepairDemand{Kind: RepairUnknownTarget, FailedResponse: response, Validation: validation}
	default:
		correction, err := b.beginToolCorrection(response, c.policy.ToolTransport)
		if err != nil {
			return err
		}
		c.correction = correction
		if correction == nil {
			c.ready, c.terminal, c.repairStart = true, true, -1
			return nil
		}
		c.pending = correction.demand()
	}
	return nil
}

// ResolveRepair consumes one application result. It never reads from a network
// or invokes a callback. A returned demand still needs the owner's send budget.
func (c *StreamConversion) ResolveRepair(corrected map[string]any, sendErr error) (*RepairDemand, error) {
	if c.pending == nil {
		return nil, errors.New("basispoints conversion has no pending repair")
	}
	demand := c.pending
	c.pending = nil
	c.cause = sendErr
	response, index, err := c.terminalResponse, -1, sendErr
	switch demand.Kind {
	case RepairToolTransport:
		var done bool
		done, err = c.correction.apply(c.bridge, corrected, sendErr)
		if err == nil && !done {
			c.pending = c.correction.demand()
			return c.pending, nil
		}
	case RepairUnknownContinuation:
		c.regenerationAttempted = true
		response, index, err = c.bridge.applyUnknownContinuation(c.terminalResponse, corrected, sendErr, demand.Validation)
	case RepairUnknownTarget:
		c.regenerationAttempted = true
		response, index, err = c.bridge.applyUnknownTarget(c.terminalResponse, corrected, sendErr)
		if err == nil && c.bridge.structured != nil {
			err = c.bridge.structured.validate(response)
		}
	default:
		err = errors.New("invalid basispoints repair demand")
	}
	if err != nil {
		return nil, c.finish(protocolError{err})
	}
	c.terminalResponse = response
	c.completion["response"] = response
	c.ready, c.terminal, c.repairStart = true, true, index
	return nil, nil
}

func (c *StreamConversion) emitCompleted(response object, repairStart int) error {
	output, _ := response["output"].([]any)
	for i, raw := range output {
		item, _ := raw.(object)
		if isTool(item) {
			if err := c.emitTool(item, i); err != nil {
				return err
			}
		} else if (c.bridge.structured != nil || repairStart >= 0 && i >= repairStart) && text(item["type"]) == "message" {
			if err := emitStructuredMessage(item, i, c.emit); err != nil {
				return err
			}
		}
	}
	c.terminal = true
	return c.emit("response.completed", c.completion)
}

func (c *StreamConversion) finish(err error) error {
	if err != nil && !errors.Is(err, io.EOF) {
		var invalid protocolError
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &invalid) {
			return err
		}
		failed := object{
			"status": "failed", "output": []any{},
			"error": object{"code": "basispoints_protocol_error", "message": err.Error()},
		}
		var upstreamFailure *UpstreamFailure
		if errors.As(err, &upstreamFailure) {
			c.failure = upstreamFailure
			failed["error"] = upstreamFailure.Details()
		} else {
			localFailure := &UpstreamFailure{Status: http.StatusBadGateway, Code: "basispoints_protocol_error", Type: "server_error", Message: err.Error()}
			var unknown unknownClientToolError
			if errors.Is(err, ErrStateUnavailable) || errors.Is(err, ErrStateCapacity) {
				code, message := "basispoints_state_unavailable", ErrStateUnavailable.Error()
				if errors.Is(err, ErrStateCapacity) {
					code, message = "basispoints_state_capacity", ErrStateCapacity.Error()
				}
				localFailure = &UpstreamFailure{Status: http.StatusServiceUnavailable, Code: code, Type: "server_error", Message: message, cause: err}
				failed["error"] = localFailure.Details()
				localFailure.Diagnostic, _ = json.Marshal(object{"error": localFailure.Details()})
			} else if c.rejectedCmd != nil {
				localFailure = c.bridge.functionCmdTransportFailure(c.rejectedCmd, c.visibleContent, err)
			} else if errors.As(err, &unknown) || c.hasRejectedTool {
				if c.hasRejectedTool {
					unknown = c.rejectedTool
				}
				localFailure = c.bridge.unknownToolFailure(unknown, c.terminalResponse, c.visibleContent, c.regenerationAttempted, err)
			} else {
				var mismatch *toolOperationMismatch
				var cmdFailure *functionCmdTransportError
				if errors.As(err, &cmdFailure) {
					localFailure = c.bridge.functionCmdTransportFailure(cmdFailure, c.visibleContent)
				} else if errors.As(err, &mismatch) {
					localFailure = mismatch.failure()
				}
			}
			localFailure.Local = true
			c.failure = localFailure
		}
		for _, field := range []string{"id", "model", "usage"} {
			if value := c.terminalResponse[field]; value != nil {
				failed[field] = value
			}
		}
		return c.emit("response.failed", object{"response": failed})
	}
	return err
}

// ReplayFact preserves the validated native/client call pair for scoped replay.
// These values are read-only and belong to this conversion until delivery ends.
type ReplayFact struct {
	CallID string
	Native map[string]any
	Client map[string]any
}

// Replays returns facts only when the entire batch has passed validation.
func (c *StreamConversion) Replays() []ReplayFact {
	if !c.ready {
		return nil
	}
	facts := make([]ReplayFact, len(c.replays))
	for i, w := range c.replays {
		facts[i] = ReplayFact{CallID: w.id, Native: w.native, Client: w.client}
	}
	return facts
}

// Deliver publishes the validated terminal after the application has committed
// its replay facts. It reports persistence failures using the existing failure
// mapping. Calling it on a failed/incomplete conversion is a no-op.
func (c *StreamConversion) Deliver(replayErr error) error {
	if !c.ready {
		return nil
	}
	c.ready = false
	if replayErr != nil {
		return c.finish(protocolError{replayErr})
	}
	return c.emitCompleted(c.terminalResponse, c.repairStart)
}
