package basispoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxToolRepairs = 2

// ToolRepairFunc continues the same BPS conversation after a rejected native
// transport call. The entire tool batch is still unexecuted at this boundary.
type ToolRepairFunc func(context.Context, map[string]any, error) (map[string]any, error)

func (b *Bridge) validateToolResponse(response object) error {
	check := *b
	check.replay = nil
	output, _ := response["output"].([]any)
	ids, calls := make(map[string]bool), make(map[string]bool)
	for _, raw := range output {
		item, _ := raw.(object)
		if !isTool(item) {
			continue
		}
		translated, err := check.translateCall(item)
		if err != nil {
			return err
		}
		id, call := text(translated["id"]), text(translated["call_id"])
		if ids[id] || calls[call] {
			return fmt.Errorf("basispoints response contains duplicate tool identities")
		}
		ids[id], calls[call] = true, true
	}
	return nil
}

// Only complete BPS wrapper batches can be returned as native tool errors.
// Never infer a target for arbitrary code, invent a call ID, or retry I/O.
func repairableTools(response object) ([]object, bool) {
	if status := text(response["status"]); status != "" && status != "completed" {
		return nil, false
	}
	output, _ := response["output"].([]any)
	var items []object
	ids := make(map[string]bool)
	for _, raw := range output {
		item, _ := raw.(object)
		if !isTool(item) {
			continue
		}
		name, call := text(item["name"]), text(item["call_id"])
		if text(item["type"]) != "function_call" || (name != "run_officejs" && name != "functions.run_officejs") || call == "" || ids[call] {
			return nil, false
		}
		args, ok := item["arguments"].(object)
		if !ok && decode([]byte(text(item["arguments"])), &args) != nil {
			return nil, false
		}
		if args == nil {
			return nil, false
		}
		ids[call] = true
		items = append(items, item)
	}
	return items, len(items) > 0 && len(items) <= 1024
}

func (b *Bridge) translateCompleted(ctx context.Context, response object, repair ToolRepairFunc) error {
	validation := b.validateToolResponse(response)
	if validation == nil {
		return b.translateResponse(response)
	}
	var schemaError toolArgumentsSchemaError
	if errors.As(validation, &schemaError) {
		return validation
	}
	original, eligible := repairableTools(response)
	if repair == nil || !eligible || b.structured != nil {
		return validation
	}
	for _, item := range original {
		if target := transportTarget(transportArguments(item)); target != "" {
			if _, _, allowed := lookupCatalogTool(b.tools, target); !allowed {
				return validation
			}
		}
	}
	failed := response
	usage := addRepairUsage(nil, response["usage"])
	for attempt := 0; attempt < maxToolRepairs; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		corrected, err := repair(ctx, failed, validation)
		usage = addRepairUsage(usage, corrected["usage"])
		if usage != nil {
			response["usage"] = usage
		}
		if err != nil {
			return fmt.Errorf("basispoints tool transport correction failed: %w", err)
		}
		items, ok := repairableTools(corrected)
		if !ok || len(items) != len(original) {
			return fmt.Errorf("basispoints tool transport correction changed the tool batch; no tool was executed")
		}
		validation = b.validateToolResponse(corrected)
		if validation == nil {
			items, err = b.restoreToolOperations(original, items)
			if err != nil {
				var mismatch *toolOperationMismatch
				if errors.As(err, &mismatch) {
					mismatch.attempt = attempt + 1
				}
				return err
			}
			outputItems := make([]any, len(items))
			for i, item := range items {
				outputItems[i] = item
			}
			combined := object{"output": outputItems}
			if err := b.validateToolResponse(combined); err != nil {
				return err
			}
			if mismatch := b.toolOperationMismatch(original, items); mismatch != nil {
				mismatch.attempt = attempt + 1
				return mismatch
			}
			// Text has already streamed. Replace only its withheld tool slots,
			// keeping one downstream response identity and stable output indexes.
			output, _ := response["output"].([]any)
			next := 0
			for i, raw := range output {
				item, _ := raw.(object)
				if isTool(item) {
					output[i] = items[next]
					next++
				}
			}
			return b.translateResponse(response)
		}
		failed = corrected
	}
	return fmt.Errorf("basispoints tool transport remains invalid after %d corrections; no tool was executed: %w", maxToolRepairs, validation)
}

// bareArgumentObject reports whether an unnamed JSON object reads as the
// arguments of a declared function tool: every key is a declared property of
// at least one non-custom catalog tool. Literal JSON that no function could
// own (for example a CUSTOM tool whose input happens to be JSON) is not an
// argument object and may still be bound as raw input.
func (b *Bridge) bareArgumentObject(candidate object) bool {
	if len(candidate) == 0 {
		return false
	}
	for _, info := range b.tools {
		if info.Kind == "custom" {
			continue
		}
		properties, _ := info.Parameters["properties"].(object)
		if len(properties) == 0 {
			continue
		}
		owned := true
		for key := range candidate {
			if _, declared := properties[key]; !declared {
				owned = false
				break
			}
		}
		if owned {
			return true
		}
	}
	return false
}

// A correction chooses transport formatting, not replacement operations.
// Keep already valid calls intact when the correction addresses the same tool,
// and bind unframed raw payloads to their original bytes. New call identities
// belong to the correction turn; replay must retain the operation sent downstream.
// Clone corrected calls so replay records exactly what the client receives,
// without rewriting the model response used by the continuation.
func (b *Bridge) restoreToolOperations(original, corrected []object) ([]object, error) {
	check := *b
	check.replay = nil
	result := append([]object(nil), corrected...)
	for i, native := range original {
		if before, err := check.translateCall(native); err == nil {
			after, afterErr := check.translateCall(corrected[i])
			// Never use argument restoration to hide a changed tool target.
			if afterErr == nil && before["type"] == after["type"] && before["name"] == after["name"] && text(before["namespace"]) == text(after["namespace"]) {
				bound := make(object, len(native))
				for key, value := range native {
					bound[key] = value
				}
				bound["id"], bound["call_id"] = corrected[i]["id"], corrected[i]["call_id"]
				result[i] = bound
			}
			continue
		}
		originalArgs := transportArguments(native)
		code, ok := originalArgs["code"].(string)
		if !ok {
			continue
		}
		summary := text(originalArgs["summary"])
		rawOriginal := strings.HasPrefix(summary, customTransportPrefix) || strings.HasPrefix(summary, functionCodeTransportPrefix) || strings.HasPrefix(summary, functionCmdTransportPrefix)
		if !rawOriginal {
			if envelope, err := decodeTransportCode(code); err == nil {
				if name, nameErr := envelopeName(envelope); nameErr != nil || name != "" {
					continue // named or conflicting envelopes are not raw executable text
				}
				after, afterErr := check.translateCall(corrected[i])
				if afterErr != nil {
					return nil, afterErr
				}
				if after["type"] == "function_call" {
					// This is an argument object, including when framed as JSON.
					// Compare its values below; never bind the whole object as code
					// in either an ordinary envelope or a corrected raw transport.
					continue
				}
				if check.bareArgumentObject(envelope) {
					// An unnamed JSON object whose keys belong to a declared function
					// is a bare argument object. A correction that redirects it to a
					// CUSTOM tool or a raw code/cmd transport would turn arguments
					// into executable input for another tool.
					return nil, &toolOperationMismatch{reason: "original_payload_unverifiable", index: i,
						originalShape: transportShape(code), correctedKind: text(after["type"])}
				}
			} else if looksLikeTransportJSON(code) {
				// An undecodable envelope is not evidence of executable source.
				// Binding it verbatim would conceal both the original arguments
				// and changes made by the correction. Explicit RAW is separate.
				after, _ := check.translateCall(corrected[i])
				return nil, &toolOperationMismatch{reason: "original_payload_unverifiable", index: i,
					originalShape: transportShape(code), correctedKind: text(after["type"])}
			}
		}
		args := transportArguments(corrected[i])
		boundArgs := make(object, len(args))
		for key, value := range args {
			boundArgs[key] = value
		}
		summary = text(args["summary"])
		if strings.HasPrefix(summary, customTransportPrefix) || strings.HasPrefix(summary, functionCodeTransportPrefix) || strings.HasPrefix(summary, functionCmdTransportPrefix) {
			boundArgs["code"] = code
		} else {
			// The correction may choose an ordinary envelope instead of a raw
			// marker. Restore the same source bytes in its declared payload field;
			// tool target and explicit metadata are still checked by the caller.
			after, err := check.translateCall(corrected[i])
			if err != nil {
				return nil, err
			}
			name := text(after["name"])
			if namespace := text(after["namespace"]); namespace != "" {
				name = namespace + "." + name
			}
			envelope := object{"name": name}
			if after["type"] == "custom_tool_call" {
				envelope["input"] = code
			} else {
				_, info, allowed := lookupCatalogTool(b.tools, name)
				if !allowed {
					continue
				}
				field := "code"
				if supportsFunctionCmdTransport(name, info.Kind, info.Parameters) {
					field = "cmd"
				} else if !supportsFunctionCodeTransport(name, info.Kind, info.Parameters) {
					continue // never guess a raw field on an unrelated function
				}
				var payload object
				if decode([]byte(text(after["arguments"])), &payload) != nil || payload == nil {
					continue
				}
				payload[field] = code
				envelope["arguments"] = payload
			}
			boundArgs["code"] = quoted(envelope)
		}
		encoded, err := json.Marshal(boundArgs)
		if err != nil {
			return nil, fmt.Errorf("basispoints cannot bind the original tool payload: %w", err)
		}
		bound := make(object, len(corrected[i]))
		for key, value := range corrected[i] {
			bound[key] = value
		}
		bound["arguments"] = string(encoded)
		result[i] = bound
	}
	return result, nil
}

// A rejected correction exposes only structural evidence through the existing
// diagnostic snapshot, never argument values, code, call IDs or catalog contents.
type toolOperationMismatch struct {
	reason, originalShape, correctedKind string
	index, attempt                       int
}

func (e *toolOperationMismatch) Error() string {
	if e.reason == "original_payload_unverifiable" {
		return "basispoints original tool operation could not be verified; no tool was executed"
	}
	return "basispoints tool transport correction changed an operation; no tool was executed"
}

func (e *toolOperationMismatch) failure() *UpstreamFailure {
	failure := &UpstreamFailure{Status: http.StatusBadGateway, Code: "basispoints_protocol_error", Type: "server_error", Message: e.Error()}
	detail := object{"source": "gateway_tool_transport", "reason": e.reason, "tool_index": e.index, "correction_attempt": e.attempt, "executed": false,
		"original_code_shape": e.originalShape, "corrected_tool_kind": e.correctedKind}
	failure.Diagnostic, _ = json.Marshal(object{"error": object{"code": failure.Code, "message": failure.Message, "param": quoted(detail)}})
	return failure
}

// Compare operations independently of their transport representation. An
// unframed JSON argument object may gain an envelope, but none of its values may
// change. Malformed named envelopes are not evidence of an arbitrary operation.
func (b *Bridge) toolOperationMismatch(original, corrected []object) *toolOperationMismatch {
	check := *b
	check.replay = nil
	for i, native := range original {
		before, err := check.translateCall(native)
		after, afterErr := check.translateCall(corrected[i])
		reject := func(reason string) *toolOperationMismatch {
			return &toolOperationMismatch{reason: reason, index: i,
				originalShape: transportShape(transportArguments(native)["code"]), correctedKind: text(after["type"])}
		}
		if afterErr != nil {
			return reject("invalid_corrected_transport")
		}
		if err == nil {
			before["call_id"], after["call_id"] = "compare", "compare"
			if historyCallFingerprint(before) != historyCallFingerprint(after) {
				return reject("operation_changed")
			}
			continue
		}
		args := transportArguments(native)
		target := transportTarget(args)
		if target != "" {
			_, info, allowed := lookupCatalogTool(b.tools, target)
			if !allowed || after["name"] != info.Name || text(after["namespace"]) != info.Namespace {
				return reject("target_changed")
			}
		}
		code, isText := args["code"].(string)
		if !isText {
			return reject("original_payload_unverifiable")
		}
		summary := text(args["summary"])
		rawMarker := strings.HasPrefix(summary, customTransportPrefix) || strings.HasPrefix(summary, functionCodeTransportPrefix) || strings.HasPrefix(summary, functionCmdTransportPrefix)
		// Only unmarked code can be an envelope or a bare function argument object.
		// Explicit raw transports can contain JSON as executable text or custom input.
		if !rawMarker {
			if envelope, envelopeErr := decodeTransportEnvelope(code); envelopeErr == nil {
				name, nameErr := envelopeName(envelope)
				if nameErr != nil {
					return reject("original_payload_unverifiable")
				}
				if name != "" {
					_, info, allowed := lookupCatalogTool(b.tools, name)
					if !allowed {
						return reject("target_changed")
					}
					expected, expectedErr := check.finishClientToolCall(native, info, envelope, false, true)
					if expectedErr != nil {
						return reject("original_payload_unverifiable")
					}
					expected["call_id"], after["call_id"] = "compare", "compare"
					if historyCallFingerprint(expected) != historyCallFingerprint(after) {
						return reject("arguments_changed")
					}
					continue
				}
			} else if raw, rawErr := decodeTransportCode(code); rawErr == nil {
				// Invalid/conflicting targets and broken nested wrappers cannot
				// become arbitrary CUSTOM input just by preserving their JSON bytes.
				if name, nameErr := envelopeName(raw); nameErr != nil || name != "" {
					return reject("original_payload_unverifiable")
				}
				// A bare argument object can only be compared as function arguments
				// below; it is never literal CUSTOM input or raw source.
				if after["type"] != "function_call" && b.bareArgumentObject(raw) {
					return reject("original_payload_unverifiable")
				}
			}
			if after["type"] == "function_call" {
				var correctedArgs object
				// Use the same bounded formatting decoder as the transport. A JSON
				// fence, label or string layer is not executable source text.
				if originalArgs, decodeErr := decodeTransportCode(code); decodeErr == nil && originalArgs != nil {
					// A broken/conflicting named envelope is not a bare argument object.
					if name, err := envelopeName(originalArgs); err != nil || name != "" {
						return reject("original_payload_unverifiable")
					}
					if decode([]byte(text(after["arguments"])), &correctedArgs) != nil || quoted(originalArgs) != quoted(correctedArgs) {
						return reject("arguments_changed")
					}
					continue
				}
			}
		}
		if after["type"] == "custom_tool_call" {
			if after["input"] != code {
				return reject("raw_payload_changed")
			}
			continue
		}
		var payload object
		if decode([]byte(text(after["arguments"])), &payload) != nil {
			return reject("invalid_corrected_transport")
		}
		field := "code"
		name := text(after["name"])
		if namespace := text(after["namespace"]); namespace != "" {
			name = namespace + "." + name
		}
		_, info, _ := lookupCatalogTool(b.tools, name)
		if supportsFunctionCmdTransport(info.Name, info.Kind, info.Parameters) {
			field = "cmd"
		}
		if payload[field] != code {
			return reject("raw_payload_changed")
		}
		if strings.HasPrefix(summary, functionCodeTransportPrefix) || strings.HasPrefix(summary, functionCmdTransportPrefix) {
			var metadata object
			if decode([]byte(text(args["extended_summary"])), &metadata) != nil || metadata == nil {
				return reject("original_metadata_unverifiable")
			}
			if duplicate, exists := metadata[field]; exists && duplicate != code {
				return reject("original_metadata_conflict")
			}
			metadata[field] = code
			if quoted(metadata) != quoted(payload) {
				return reject("metadata_changed")
			}
		}
	}
	return nil
}

func transportArguments(native object) object {
	if args, ok := native["arguments"].(object); ok {
		return args
	}
	var args object
	_ = decode([]byte(text(native["arguments"])), &args)
	return args
}

func transportTarget(args object) string {
	for _, prefix := range []string{customTransportPrefix, functionCodeTransportPrefix, functionCmdTransportPrefix} {
		if summary := text(args["summary"]); strings.HasPrefix(summary, prefix) {
			return strings.TrimPrefix(summary, prefix)
		}
	}
	if envelope, err := decodeTransportEnvelope(args["code"]); err == nil {
		name, _ := envelopeName(envelope)
		return name
	}
	return ""
}

// Sum billable usage across model continuations, including failed corrections.
func addRepairUsage(total object, value any) object {
	usage, _ := value.(object)
	if usage == nil {
		return total
	}
	result := make(object)
	for k, v := range total {
		result[k] = v
	}
	for k, v := range usage {
		if nested, ok := v.(object); ok {
			previous, _ := result[k].(object)
			result[k] = addRepairUsage(previous, nested)
			continue
		}
		var n int64
		var err error
		switch v := v.(type) {
		case json.Number:
			n, err = v.Int64()
		case int:
			n = int64(v)
		case int64:
			n = v
		default:
			continue
		}
		if err == nil && n >= 0 {
			previous, _ := result[k].(int64)
			result[k] = previous + n
		}
	}
	return result
}

// BuildToolRepairRequest extends an already prepared BPS request. It preserves
// the model, effort, scope and native history; no client tool is executed here.
func BuildToolRepairRequest(prepared []byte, failed map[string]any, validation error) ([]byte, error) {
	items, ok := repairableTools(failed)
	if !ok || validation == nil {
		return nil, fmt.Errorf("basispoints tool response is not eligible for correction")
	}
	var request object
	if decode(prepared, &request) != nil || request == nil {
		return nil, fmt.Errorf("invalid prepared Basispoints request")
	}
	input, ok := request["input"].([]any)
	if !ok {
		return nil, fmt.Errorf("basispoints correction requires expanded history")
	}
	var tail []any
	if len(input) > 0 {
		last, _ := input[len(input)-1].(object)
		if text(last["type"]) == "compaction_trigger" {
			tail = input[len(input)-1:]
			input = append([]any(nil), input[:len(input)-1]...)
		}
	}
	output, _ := failed["output"].([]any)
	input = append(input, output...)
	detail := object{"code": "invalid_client_tool_transport", "message": validation.Error()}
	var unknown unknownClientToolError
	if errors.As(validation, &unknown) {
		detail["code"], detail["rejected_tool"] = "unknown_client_tool", toolDiagnosticIdentifier(unknown.target)
	}
	feedback, _ := json.Marshal(object{"executed": false, "error": detail})
	for _, item := range items {
		call := text(item["call_id"])
		input = append(input, object{"type": "function_call_output", "id": "fc_" + fingerprint([]any{call, len(input)}), "call_id": call, "output": string(feedback)})
	}
	if errors.As(validation, &unknown) {
		input = append(input, message("developer", "The preceding native call selected a tool absent from the current client catalog and was NOT executed. Continue the original task with exactly one declared client tool using its documented run_officejs transport and argument schema. Do not repeat historical operations, invent capabilities or treat the rejected call as executed. If no declared tool can perform the task, stop and explain the limitation. Do not repeat commentary."))
	} else {
		input = append(input, message("developer", fmt.Sprintf("The preceding tool batch failed transport validation before any client tool was executed. Correct only its transport formatting and return exactly %d run_officejs calls in the same order, preserving the intended operations and exact raw code. Use the existing client catalog: FUNCTION needs one JSON envelope with object arguments; CUSTOM needs summary=codex2api.custom/CATALOG_NAME and raw input in code; FUNCTION_CODE and FUNCTION_CMD need their declared markers and metadata JSON in extended_summary. Calls that already passed validation must remain unchanged. Do not execute Office code, infer an undeclared target, add operations, or repeat commentary.", len(items))))
	}
	request["input"] = append(input, tail...)
	metadata, _ := request["metadata"].(object)
	if metadata == nil {
		return nil, fmt.Errorf("basispoints correction requires request metadata")
	}
	iteration, err := strconv.Atoi(text(metadata["agent_iteration"]))
	if err != nil {
		return nil, fmt.Errorf("invalid Basispoints agent iteration")
	}
	metadata["agent_iteration"] = strconv.Itoa(iteration + 1)
	return json.Marshal(request)
}

// ReadToolRepairResponse withholds all intermediate events from the client.
// Only the terminal native response is authoritative, as in the primary stream.
func ReadToolRepairResponse(reader io.Reader, onEvent ...func([]byte)) (map[string]any, error) {
	response, err := readRepairResponse(io.LimitReader(reader, 32<<20), onEvent...)
	if err != nil {
		return response, err
	}
	if text(response["status"]) != "completed" {
		return response, fmt.Errorf("basispoints correction did not complete")
	}
	return response, nil
}
