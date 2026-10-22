package basispoints

import (
	"context"
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

// Keep structural evidence separate from client events. The gateway's existing
// error.param sanitizer handles this bounded metadata; no arguments, schema,
// conversation content, or complete catalog is included.
func (b *Bridge) unknownToolFailure(unknown unknownClientToolError, response object, visible, regenerated bool) *UpstreamFailure {
	names := make([]string, 0, len(b.tools))
	for name := range b.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	detail := object{
		"source":                 "gateway_tool_catalog",
		"rejected_tool":          toolDiagnosticIdentifier(unknown.target),
		"catalog_tool_count":     len(b.tools),
		"catalog_fingerprint":    fingerprint(names),
		"has_tool_history":       b.hasToolHistory,
		"visible_output":         visible,
		"regeneration_attempted": regenerated,
		"response_model":         toolDiagnosticIdentifier(text(response["model"])),
	}
	failure := &UpstreamFailure{Status: http.StatusBadGateway, Code: "basispoints_protocol_error", Type: "server_error", Message: unknown.Error()}
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

type RepairToolCall func(context.Context) (io.ReadCloser, error)

func (b *Bridge) canRepair(response object, err error) bool {
	var unknown unknownClientToolError
	if len(b.tools) == 0 || b.hasToolHistory || !errors.As(err, &unknown) || text(response["status"]) != "completed" {
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
func (b *Bridge) canRepairUnknownContinuation(response object, err error) bool {
	var unknown unknownClientToolError
	if !b.hasToolHistory || b.structured != nil || len(b.completedToolHistory) == 0 || len(b.tools) == 0 || !errors.As(err, &unknown) || text(response["status"]) != "completed" {
		return false
	}
	items, ok := repairableTools(response)
	output, _ := response["output"].([]any)
	if !ok || len(items) != 1 || len(output) == 0 {
		return false
	}
	last, _ := output[len(output)-1].(object)
	return isTool(last) && !b.completedToolHistory["call/"+text(items[0]["call_id"])] && text(items[0]["id"]) != "" && !b.completedToolHistory["item/"+text(items[0]["id"])]
}

// Expanded history includes scoped replay recovery. Missing, duplicate or
// unverifiable calls/results leave continuation correction disabled.
func (b *Bridge) completeToolHistory(input []any) map[string]bool {
	seen, pending := make(map[string]bool), make(map[string]bool)
	check := *b
	check.replay = nil
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
			if err != nil {
				return nil
			}
			translated["call_id"] = "compare"
			seen["operation/"+historyCallFingerprint(translated)] = true
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

func (b *Bridge) repairUnknownContinuation(ctx context.Context, original object, repair ToolRepairFunc, validation error) (object, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, -1, err
	}
	corrected, err := repair(ctx, original, validation)
	original["usage"] = addRepairUsage(addRepairUsage(nil, original["usage"]), corrected["usage"])
	if err != nil {
		return nil, -1, err
	}
	if err := ctx.Err(); err != nil {
		return nil, -1, err
	}
	items, ok := repairableTools(corrected)
	if !ok || text(corrected["status"]) != "completed" || len(items) != 1 {
		return nil, -1, fmt.Errorf("basispoints unknown tool correction changed the tool batch; no tool was executed")
	}
	if text(items[0]["id"]) == "" || b.completedToolHistory["call/"+text(items[0]["call_id"])] || b.completedToolHistory["item/"+text(items[0]["id"])] {
		return nil, -1, fmt.Errorf("basispoints unknown tool correction reused a historical tool identity")
	}
	check := *b
	check.replay = nil
	translated, err := check.translateCall(items[0])
	if err != nil {
		return nil, -1, err
	}
	translated["call_id"] = "compare"
	if b.completedToolHistory["operation/"+historyCallFingerprint(translated)] {
		return nil, -1, fmt.Errorf("basispoints unknown tool correction repeated a completed historical operation")
	}
	output, _ := original["output"].([]any)
	replacement, _ := corrected["output"].([]any)
	index := len(output) - 1
	merged := make(object, len(original))
	for key, value := range original {
		merged[key] = value
	}
	merged["output"] = append(append([]any{}, output[:index]...), replacement...)
	if err := b.translateResponse(merged); err != nil {
		return nil, -1, err
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

func readRepairResponse(reader io.Reader, onEvent ...func([]byte)) (object, error) {
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

func (b *Bridge) repairResponse(ctx context.Context, original object, repair RepairToolCall) (object, int, error) {
	reader, err := repair(ctx)
	if err != nil {
		return nil, -1, err
	}
	defer func() { _ = reader.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = reader.Close() })
	defer stop()
	corrected, err := readRepairResponse(reader, b.OnUpstreamEvent)
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
