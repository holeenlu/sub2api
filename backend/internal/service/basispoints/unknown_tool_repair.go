package basispoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
)

type unknownClientToolError struct{ target string }

func (unknownClientToolError) Error() string {
	return "basispoints returned a tool outside the client's catalog"
}

// Reasons are assigned at local validation boundaries, never copied from model
// text. Keep the cause for cancellation and typed upstream failure handling.
type unknownToolCorrectionError struct {
	reason string
	cause  error
}

func (e *unknownToolCorrectionError) Error() string {
	return "basispoints unknown tool correction failed (" + e.reason + "); no tool was executed"
}

func (e *unknownToolCorrectionError) Unwrap() error { return e.cause }

func unknownToolCorrectionDiagnostic(detail object, correction error) string {
	reason := "correction_failed"
	var rejected unknownClientToolError
	var invalid *unknownToolCorrectionError
	var schema toolArgumentsSchemaError
	switch {
	case errors.As(correction, &invalid):
		reason = invalid.reason
	case errors.As(correction, &rejected):
		reason = "unknown_tool"
	case errors.As(correction, &schema):
		reason = "schema_error"
	}
	detail["correction_outcome"], detail["correction_reason"] = "failed", reason
	if errors.As(correction, &rejected) {
		detail["correction_rejected_tool"] = toolDiagnosticIdentifier(rejected.target)
	}
	return (&unknownToolCorrectionError{reason: reason}).Error()
}

// Keep structural evidence separate from client events. The gateway's existing
// error.param sanitizer handles this bounded metadata; no arguments, schema,
// conversation content, or complete catalog is included.
func (b *Bridge) toolCatalogDiagnostic(target string) object {
	names := make([]string, 0, len(b.tools))
	for name := range b.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return object{
		"rejected_tool":       toolDiagnosticIdentifier(target),
		"catalog_tool_count":  len(b.tools),
		"catalog_fingerprint": fingerprint(names),
	}
}

func (b *Bridge) unknownToolFailure(unknown unknownClientToolError, response object, visible, regenerated bool, correction ...error) *UpstreamFailure {
	detail := b.toolCatalogDiagnostic(unknown.target)
	detail["source"], detail["has_tool_history"] = "gateway_tool_catalog", b.hasToolHistory
	detail["visible_output"], detail["regeneration_attempted"] = visible, regenerated
	detail["response_model"] = toolDiagnosticIdentifier(text(response["model"]))
	if b.hasToolHistory && !regenerated {
		if blocker := b.unknownContinuationBlockReason(response, unknown, visible); blocker != "" {
			detail["correction_blocker"] = blocker
		}
	}
	message := unknown.Error()
	if regenerated && len(correction) > 0 && correction[0] != nil {
		message = unknownToolCorrectionDiagnostic(detail, correction[0])
	}
	failure := &UpstreamFailure{Status: http.StatusBadGateway, Code: "basispoints_protocol_error", Type: "server_error", Message: message}
	failure.Diagnostic, _ = json.Marshal(object{"error": object{"code": failure.Code, "message": failure.Message, "param": quoted(detail)}})
	return failure
}

func toolDiagnosticIdentifier(value string) string {
	if len(value) == 0 || len(value) > 256 {
		return "[unavailable]"
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '.' || char == '-' || char == ':') {
			return "[invalid identifier]"
		}
	}
	return value
}

func (b *Bridge) canRepair(response object, err error) bool {
	var unknown unknownClientToolError
	// External context is not executed history, but still forbids regeneration.
	if len(b.tools) == 0 || b.hasToolHistory || b.hasStandaloneContext || !errors.As(err, &unknown) || text(response["status"]) != "completed" {
		return false
	}
	output, _ := response["output"].([]any)
	count := 0
	for i, raw := range output {
		item, _ := raw.(object)
		if !isTool(item) {
			continue
		}
		count++
		name := text(item["name"])
		if i != len(output)-1 || text(item["type"]) != "function_call" || (name != "run_officejs" && name != "functions.run_officejs") {
			return false
		}
	}
	return count == 1
}

// Continuations correct one withheld call, never regenerate executed history.
func (b *Bridge) unknownContinuationBlockReason(response object, err error, visible bool) string {
	var unknown unknownClientToolError
	cmdUnknown := unknownFunctionCmdTarget(err)
	switch {
	case !errors.As(err, &unknown) && cmdUnknown == nil:
		return "not_unknown_tool"
	case !b.hasToolHistory && cmdUnknown == nil:
		return "no_tool_history"
	case visible:
		return "visible_output"
	case b.structured != nil:
		return "structured_output"
	case cmdUnknown != nil && b.hasStandaloneContext:
		return "standalone_context"
	case b.hasToolHistory && len(b.completedToolHistory) == 0:
		return "incomplete_history"
	case len(b.tools) == 0:
		return "empty_catalog"
	case text(response["status"]) != "completed":
		return "non_completed"
	}
	items, ok := repairableTools(response)
	output, _ := response["output"].([]any)
	if !ok || len(items) != 1 || len(output) == 0 {
		return "invalid_batch"
	}
	last, _ := output[len(output)-1].(object)
	switch {
	case !isTool(last):
		return "tool_not_last"
	case b.completedToolHistory["call/"+text(items[0]["call_id"])]:
		return "reused_call_id"
	case text(items[0]["id"]) == "":
		return "missing_item_id"
	case b.completedToolHistory["item/"+text(items[0]["id"])]:
		return "reused_item_id"
	}
	if cmdUnknown != nil {
		for _, raw := range output {
			item, ok := raw.(object)
			if !ok {
				return "invalid_batch"
			}
			switch text(item["type"]) {
			case "function_call", "reasoning", "message":
			default:
				return "invalid_batch"
			}
		}
		if _, err := functionCmdTransportArguments(transportArguments(items[0])); err != nil {
			return "invalid_cmd_transport"
		}
	}
	return ""
}

func (b *Bridge) originalHistoryOperations(input []any) map[string]string {
	pending, completed := make(map[string]string), make(map[string]string)
	ids := make(map[string]string)
	for _, raw := range input {
		item, _ := raw.(object)
		call := text(item["call_id"])
		if isTool(item) {
			if _, seen := pending[call]; seen {
				pending[call] = ""
				delete(completed, call)
				continue
			}
			pending[call] = ""
			if id := text(item["id"]); id != "" {
				if previous, duplicate := ids[id]; duplicate {
					pending[previous] = ""
					delete(completed, previous)
					continue
				}
				ids[id] = call
			}
			if historyCallFingerprint(item) == "" {
				continue
			}
			canonical := make(object, len(item))
			for key, value := range item {
				canonical[key] = value
			}
			name := text(item["name"])
			if namespace := text(item["namespace"]); namespace != "" {
				name = namespace + "." + name
			}
			if _, info, known := lookupCatalogTool(b.tools, name); known {
				// Normalize an exact catalog identity without applying today's
				// argument schema to an already completed historical operation.
				canonical["name"], canonical["namespace"] = info.Name, info.Namespace
			}
			canonical["call_id"] = "compare"
			pending[call] = historyCallFingerprint(canonical)
		} else if strings.HasSuffix(text(item["type"]), "_call_output") && call != "" {
			if signature := pending[call]; signature != "" {
				completed[call] = signature
			} else {
				delete(completed, call)
			}
			// Keep the identity seen even after a result or an output-first
			// record; a later call/result cannot replace the original proof.
			pending[call] = ""
		}
	}
	return completed
}

// Expanded history includes scoped replay recovery. Missing, duplicate or
// unverifiable calls/results leave continuation correction disabled.
func (b *Bridge) completeToolHistory(input []any, originalInput ...[]any) map[string]bool {
	var originalOperations map[string]string
	if len(originalInput) == 1 {
		originalOperations = b.originalHistoryOperations(originalInput[0])
	}
	seen, pending := make(map[string]bool), make(map[string]bool)
	check := *b
	check.replay, check.stagedReplays = nil, nil
	for _, raw := range input {
		item, _ := raw.(object)
		call := text(item["call_id"])
		if isTool(item) {
			id := text(item["id"])
			if call == "" || seen["call/"+call] || id != "" && seen["item/"+id] {
				return nil
			}
			seen["call/"+call], pending[call] = true, true
			if id != "" {
				seen["item/"+id] = true
			}
			translated, err := check.translateCall(item)
			operation := originalOperations[call]
			if err != nil {
				if operation == "" {
					return nil
				}
			} else {
				translated["call_id"] = "compare"
				operation = historyCallFingerprint(translated)
			}
			if operation == "" {
				return nil
			}
			seen["operation/"+operation] = true
		} else if strings.HasSuffix(text(item["type"]), "_call_output") {
			if !pending[call] {
				return nil
			}
			delete(pending, call)
		}
	}
	if len(pending) != 0 {
		return nil
	}
	return seen
}

func (b *Bridge) applyUnknownContinuation(original, corrected object, err, validation error) (object, int, error) {
	original["usage"] = addRepairUsage(addRepairUsage(nil, original["usage"]), corrected["usage"])
	if err != nil {
		return nil, -1, &unknownToolCorrectionError{reason: "request_failed", cause: err}
	}
	if text(corrected["status"]) != "completed" {
		return nil, -1, &unknownToolCorrectionError{reason: "non_completed"}
	}
	replacement, validOutput := corrected["output"].([]any)
	if !validOutput || len(replacement) == 0 || corrected["error"] != nil || corrected["incomplete_details"] != nil {
		return nil, -1, &unknownToolCorrectionError{reason: "invalid_output"}
	}
	toolCount := 0
	for _, raw := range replacement {
		item, ok := raw.(object)
		if !ok {
			return nil, -1, &unknownToolCorrectionError{reason: "invalid_output"}
		}
		switch text(item["type"]) {
		case "function_call", "custom_tool_call":
			toolCount++
		case "reasoning", "message":
		default:
			return nil, -1, &unknownToolCorrectionError{reason: "invalid_output"}
		}
	}
	if unknownFunctionCmdTarget(validation) != nil && toolCount != 0 {
		return nil, -1, &unknownToolCorrectionError{reason: "tool_output_forbidden"}
	}
	if toolCount == 0 {
		// The correction prompt explicitly permits an explanation when no current
		// tool can help. Opaque reasoning or unrecognized items are not an answer.
		if !validUnknownToolExplanation(replacement) {
			return nil, -1, &unknownToolCorrectionError{reason: "invalid_explanation"}
		}
	} else {
		items, ok := repairableTools(corrected)
		if !ok || len(items) != 1 {
			return nil, -1, &unknownToolCorrectionError{reason: "invalid_batch"}
		}
		switch {
		case text(items[0]["id"]) == "":
			return nil, -1, &unknownToolCorrectionError{reason: "missing_item_id"}
		case b.completedToolHistory["call/"+text(items[0]["call_id"])]:
			return nil, -1, &unknownToolCorrectionError{reason: "reused_call_id"}
		case b.completedToolHistory["item/"+text(items[0]["id"])]:
			return nil, -1, &unknownToolCorrectionError{reason: "reused_item_id"}
		}
		check := *b
		check.replay, check.stagedReplays = nil, nil
		translated, err := check.translateCall(items[0])
		if err != nil {
			reason := "invalid_transport"
			var unknown unknownClientToolError
			var schema toolArgumentsSchemaError
			if errors.As(err, &unknown) {
				reason = "unknown_tool"
			} else if errors.As(err, &schema) {
				reason = "schema_error"
			}
			return nil, -1, &unknownToolCorrectionError{reason: reason, cause: err}
		}
		translated["call_id"] = "compare"
		if b.completedToolHistory["operation/"+historyCallFingerprint(translated)] {
			return nil, -1, &unknownToolCorrectionError{reason: "repeated_operation"}
		}
	}
	output, _ := original["output"].([]any)
	index := len(output) - 1
	merged := make(object, len(original))
	for key, value := range original {
		merged[key] = value
	}
	merged["output"] = append(append([]any{}, output[:index]...), replacement...)
	if err := b.translateResponse(merged); err != nil {
		return nil, -1, &unknownToolCorrectionError{reason: "response_validation_failed", cause: err}
	}
	return merged, index, nil
}

// The correction reuses the already prepared body, including uploaded file IDs,
// selected model, account-scoped metadata, and expanded history. It adds no tool
// result and never represents the rejected call as executed.
func RepairRequest(prepared []byte) ([]byte, error) {
	var request object
	if decode(prepared, &request) != nil {
		return nil, fmt.Errorf("invalid prepared Basispoints request")
	}
	input, ok := request["input"].([]any)
	if !ok {
		return nil, fmt.Errorf("missing Basispoints request history")
	}
	reminder := message("developer", "The previous response selected a tool absent from the current client catalog. No client tool from that response was executed. Correct this once using exactly one tool declared in the client catalog and its documented run_officejs transport. Do not call executor-internal helpers as standalone tools. Preserve the original task and use only the declared argument schema.")
	index := len(input)
	if index > 0 {
		last, _ := input[index-1].(object)
		if text(last["type"]) == "compaction_trigger" {
			index--
		}
	}
	next := make([]any, 0, len(input)+1)
	next = append(next, input[:index]...)
	next = append(next, reminder)
	next = append(next, input[index:]...)
	request["input"] = next
	return json.Marshal(request)
}

// ReadRepairResponse decodes a withheld correction and retains progressive usage.
// The caller owns cancellation and closing reader. It never sends requests.
func ReadRepairResponse(reader io.Reader, onEvent ...func([]byte)) (object, error) {
	var observer func([]byte)
	if len(onEvent) > 0 {
		observer = onEvent[0]
	}
	var response object
	var observedUsage object
	pending := make(map[string]bool)
	total := 0
	err := readEvents(reader, func(eventName string, data []byte) error {
		total += len(data)
		if total > 32<<20 {
			return fmt.Errorf("basispoints correction response exceeds 32 MiB")
		}
		if string(data) == "[DONE]" {
			return nil
		}
		var event object
		if decode(data, &event) != nil {
			return fmt.Errorf("invalid Basispoints correction event")
		}
		if event != nil {
			observeUpstreamEvent(observer, eventName, data, event)
		}
		if current, ok := event["response"].(object); ok {
			if usage, ok := current["usage"].(object); ok {
				observedUsage = maxReportedUsage(observedUsage, usage)
			}
		}
		if usage, ok := event["usage"].(object); ok {
			observedUsage = maxReportedUsage(observedUsage, usage)
		}
		kind := text(event["type"])
		if kind == "" {
			kind = eventName
		}
		if kind == "response.output_item.done" {
			item, _ := event["item"].(object)
			if isTool(item) {
				if len(pending) >= 1024 {
					return fmt.Errorf("too many Basispoints correction tool items")
				}
				pending[text(item["call_id"])+"\x00"+text(item["id"])] = true
			}
		}
		switch kind {
		case "response.completed", "response.incomplete", "response.failed", "response.cancelled":
			response, _ = event["response"].(object)
			if response == nil {
				return fmt.Errorf("missing Basispoints correction response")
			}
			if kind != "response.completed" {
				response["status"] = "failed"
				if failure := classifyUpstreamFailure(kind, event); failure != nil {
					return failure
				}
			}
			if kind == "response.completed" {
				output, _ := response["output"].([]any)
				for _, raw := range output {
					item, _ := raw.(object)
					if isTool(item) {
						delete(pending, text(item["call_id"])+"\x00"+text(item["id"]))
					}
				}
				if len(pending) != 0 {
					return fmt.Errorf("basispoints correction omitted an original tool item")
				}
			}
			return io.EOF
		case "error":
			return classifyUpstreamFailure(kind, event)
		}
		return nil
	})
	if response == nil {
		response = object{"usage": observedUsage}
	} else if response["usage"] == nil {
		response["usage"] = observedUsage
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return response, err
	}
	if text(response["status"]) == "" {
		return response, io.ErrUnexpectedEOF
	}
	return response, nil
}

// Token counters are JSON integers. Preserve exact counters without float64,
// and combine nested cached/reasoning details for both provider attempts.
func mergeUsage(first, second object) object {
	result := make(object)
	for key, v := range first {
		result[key] = v
	}
	for key, v := range second {
		if nested, ok := v.(object); ok {
			before, _ := result[key].(object)
			result[key] = mergeUsage(before, nested)
			continue
		}
		a, okA := new(big.Int).SetString(fmt.Sprint(result[key]), 10)
		n, okB := new(big.Int).SetString(fmt.Sprint(v), 10)
		if okB && n.Sign() >= 0 {
			if okA && a.Sign() >= 0 {
				n.Add(a, n)
			}
			result[key] = json.Number(n.String())
		}
	}
	return result
}

func (b *Bridge) applyUnknownTarget(original, corrected object, err error) (object, int, error) {
	first, _ := original["usage"].(object)
	second, _ := corrected["usage"].(object)
	original["usage"] = mergeUsage(first, second)
	if err != nil {
		return nil, -1, err
	}
	if text(corrected["status"]) != "completed" {
		return nil, -1, fmt.Errorf("basispoints tool correction did not complete")
	}
	output, _ := original["output"].([]any)
	replacement, ok := corrected["output"].([]any)
	if !ok || len(replacement) == 0 {
		return nil, -1, fmt.Errorf("basispoints tool correction returned no output")
	}
	index := len(output) - 1
	merged := make(object, len(original))
	for k, v := range original {
		merged[k] = v
	}
	merged["output"] = append(append([]any{}, output[:index]...), replacement...)
	if err := b.translateResponse(merged); err != nil {
		return nil, -1, err
	}
	return merged, index, nil
}

// Progressive usage reports are snapshots, not separate charges. Retain only
// their maxima as a fallback when a failed correction has no terminal usage.
func maxReportedUsage(first, second object) object {
	result := make(object)
	for key, v := range first {
		result[key] = v
	}
	for key, v := range second {
		if nested, ok := v.(object); ok {
			before, _ := result[key].(object)
			result[key] = maxReportedUsage(before, nested)
			continue
		}
		n, valid := new(big.Int).SetString(fmt.Sprint(v), 10)
		if !valid || n.Sign() < 0 {
			continue
		}
		old, exists := new(big.Int).SetString(fmt.Sprint(result[key]), 10)
		if !exists || n.Cmp(old) > 0 {
			result[key] = json.Number(n.String())
		}
	}
	return result
}

func validUnknownToolExplanation(output []any) bool {
	nonempty := false
	ids := make(map[string]bool)
	for _, raw := range output {
		item, ok := raw.(object)
		if !ok {
			return false
		}
		switch text(item["type"]) {
		case "reasoning":
			if encrypted := item["encrypted_content"]; encrypted != nil {
				if _, ok := encrypted.(string); !ok {
					return false
				}
			}
			if summary, exists := item["summary"]; exists {
				parts, ok := summary.([]any)
				if !ok {
					return false
				}
				for _, rawPart := range parts {
					part, ok := rawPart.(object)
					if !ok || text(part["type"]) != "summary_text" {
						return false
					}
					if _, ok := part["text"].(string); !ok {
						return false
					}
				}
			}
			continue
		case "message":
		default:
			return false
		}
		id := text(item["id"])
		if text(item["role"]) != "assistant" || strings.TrimSpace(id) == "" || ids[id] {
			return false
		}
		ids[id] = true
		if status, exists := item["status"]; exists && status != "completed" {
			return false
		}
		content, ok := item["content"].([]any)
		if !ok {
			return false
		}
		for _, rawPart := range content {
			part, ok := rawPart.(object)
			if !ok {
				return false
			}
			field := "text"
			switch text(part["type"]) {
			case "output_text":
			case "refusal":
				field = "refusal"
			default:
				return false
			}
			value, ok := part[field].(string)
			if !ok {
				return false
			}
			nonempty = nonempty || strings.TrimSpace(value) != ""
			for _, key := range []string{"annotations", "logprobs"} {
				if value := part[key]; value != nil {
					if _, ok := value.([]any); !ok {
						return false
					}
				}
			}
		}
	}
	return nonempty
}
