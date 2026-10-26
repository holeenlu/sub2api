package basispoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

const functionCmdTransportPrefix = "codex2api.function_cmd/"

type functionCmdTransportError struct {
	target, reason string
	attempt        int
}

func (*functionCmdTransportError) Error() string {
	return "basispoints function cmd transport requires an exact catalog function with a string cmd parameter"
}

// An absent CMD target may explain its limitation, never select another tool.
// Keep it distinct from unknownClientToolError so first-turn regeneration and
// ordinary formatting correction cannot authorize replacement execution.
func unknownFunctionCmdTarget(err error) *functionCmdTransportError {
	var rejected *functionCmdTransportError
	if errors.As(err, &rejected) && rejected.reason == "unknown_target" {
		return rejected
	}
	return nil
}

func functionCmdTransportEligibilityError(name string, info tool, known bool) *functionCmdTransportError {
	reason := ""
	properties, _ := info.Parameters["properties"].(object)
	cmd, _ := properties["cmd"].(object)
	switch {
	case !known:
		reason = "unknown_target"
	case info.Kind != "function":
		reason = "wrong_tool_kind"
	case supportsFunctionCodeTransport(name, info.Kind, info.Parameters):
		reason = "code_transport_preferred"
	case info.Parameters["type"] != "object" || cmd["type"] != "string":
		reason = "cmd_not_string"
	case !supportsFunctionCmdTransport(name, info.Kind, info.Parameters):
		reason = "raw_transport_not_declared"
	}
	if reason == "" {
		return nil
	}
	return &functionCmdTransportError{target: name, reason: reason}
}

func (b *Bridge) functionCmdTransportFailure(rejected *functionCmdTransportError, visible bool, correction ...error) *UpstreamFailure {
	detail := b.toolCatalogDiagnostic(rejected.target)
	detail["source"], detail["transport"], detail["reason"] = "gateway_tool_transport", "function_cmd", rejected.reason
	detail["correction_attempt"], detail["visible_output"], detail["executed"] = rejected.attempt, visible, false
	message := rejected.Error()
	if rejected.attempt > 0 && len(correction) > 0 && correction[0] != nil {
		message = unknownToolCorrectionDiagnostic(detail, correction[0])
	}
	failure := &UpstreamFailure{Status: http.StatusBadGateway, Code: "basispoints_protocol_error", Type: "server_error", Message: message}
	failure.Diagnostic, _ = json.Marshal(object{"error": object{"code": failure.Code, "message": failure.Message, "param": quoted(detail)}})
	return failure
}

// Only catalog exec_command functions with explicit string cmd use this transport. Other
// function arguments keep their existing JSON envelope representation.
func supportsFunctionCmdTransport(name, kind string, parameters any) bool {
	if name != "exec_command" && !strings.HasSuffix(name, ".exec_command") {
		return false
	}
	if supportsFunctionCodeTransport(name, kind, parameters) {
		return false
	}
	if kind != "function" || name == "" || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsSpace) >= 0 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return false
	}
	schema, _ := parameters.(object)
	properties, _ := schema["properties"].(object)
	code, _ := properties["cmd"].(object)
	return schema["type"] == "object" && code["type"] == "string"
}

// Keep executable text in the native code string. extended_summary carries only
// the other JSON arguments; the server serializes the final client arguments.
// No source text is parsed, repaired, evaluated or treated as another tool call.
func (b *Bridge) functionCmdTransportEnvelope(arguments object) (object, bool, error) {
	summary, ok := arguments["summary"].(string)
	if !ok || !strings.HasPrefix(summary, functionCmdTransportPrefix) {
		return nil, false, nil
	}
	target := strings.TrimPrefix(summary, functionCmdTransportPrefix)
	name, info, allowed := lookupCatalogTool(b.tools, target)
	if err := functionCmdTransportEligibilityError(name, info, allowed); err != nil {
		err.target = target
		return nil, true, err
	}
	args, err := functionCmdTransportArguments(arguments)
	if err != nil {
		return nil, true, err
	}
	// The client is authoritative for the full tool schema. The relay only
	// validates transport invariants above; rejecting here turns harmless extra
	// or forward-compatible fields into a stream-level 502.
	return object{"name": name, "arguments": args}, true, nil
}

func functionCmdTransportArguments(arguments object) (object, error) {
	code, codeOK := arguments["code"].(string)
	metadata, metadataOK := arguments["extended_summary"].(string)
	if !codeOK || !metadataOK {
		return nil, fmt.Errorf("basispoints function cmd transport requires string code and JSON arguments in extended_summary")
	}
	if len(code) > maxEnvelopeBytes || len(metadata) > maxEnvelopeBytes-len(code) {
		return nil, fmt.Errorf("basispoints function cmd transport exceeds the size limit")
	}
	var args object
	if decode([]byte(metadata), &args) != nil || args == nil {
		return nil, fmt.Errorf("basispoints function cmd transport extended_summary must contain one JSON object")
	}
	if duplicate, exists := args["cmd"]; exists {
		duplicateText, ok := duplicate.(string)
		if !ok || duplicateText != code {
			return nil, fmt.Errorf("basispoints function cmd transport has conflicting duplicate cmd in extended_summary")
		}
		// Some upstreams redundantly repeat the exact same cmd in both fields.
		// Dropping only an identical duplicate is safe; conflicting values remain rejected.
		delete(args, "cmd")
	}
	args["cmd"] = code
	encoded, err := json.Marshal(args)
	if err != nil || len(encoded) > maxEnvelopeBytes {
		return nil, fmt.Errorf("basispoints function cmd transport arguments exceed the size limit")
	}
	return args, nil
}

// A wrong CMD marker may gain an ordinary envelope only when the exact declared
// function accepts the complete original arguments. Never infer another target,
// convert cmd to code/custom input, or relax ordinary function schema validation.
func (b *Bridge) originalFunctionCmdCall(native object) (object, error) {
	arguments := transportArguments(native)
	name := strings.TrimPrefix(text(arguments["summary"]), functionCmdTransportPrefix)
	name, info, known := lookupCatalogTool(b.tools, name)
	if err := functionCmdTransportEligibilityError(name, info, known); err != nil && err.reason != "raw_transport_not_declared" {
		return nil, err
	}
	args, err := functionCmdTransportArguments(arguments)
	if err != nil {
		return nil, err
	}
	return b.finishClientToolCall(native, info, object{"name": name, "arguments": args}, false, true)
}

func encodeFunctionCmdTransport(name string, args object) (object, error) {
	metadata := make(object, len(args))
	for key, value := range args {
		if key != "cmd" {
			metadata[key] = value
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("basispoints history function metadata cannot be serialized")
	}
	return object{
		"summary": functionCmdTransportPrefix + name, "code": args["cmd"],
		"extended_summary": string(encoded), "destructive": false, "references": []any{},
	}, nil
}
