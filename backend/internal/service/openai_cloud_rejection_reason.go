package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Cloud JSON-RPC refusals carry the App Server's validation text, which is the
// only way to learn why a history batch was refused (2026-10-25: -32602 at
// thread/inject_items, cause unknown because the text had been discarded).
// Provider text may echo request values or credentials, so only this bounded,
// sanitized form exists, and only in operator logs: it is never returned to
// clients, persisted in session records or copied into diagnostics exports.
const cloudRejectionReasonLimit = 300

var (
	cloudReasonBearer = regexp.MustCompile(`(?i)\bbearer\s+[^\s"'` + "`" + `]+`)
	cloudReasonAPIKey = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`)
	cloudReasonOpaque = regexp.MustCompile(`[A-Za-z0-9+/=_.-]{48,}`)
	// Serde and validators quote offending values; long literals are content.
	cloudReasonQuoted = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.){41,}\"|'(?:[^'\\\\]|\\\\.){41,}'|`[^`]{41,}`")
	cloudReasonItem   = regexp.MustCompile(`\bitems\[(\d+)\]`)
)

func cloudSanitizeUpstreamText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	text = cloudReasonQuoted.ReplaceAllStringFunc(text, func(literal string) string {
		quote := literal[:1]
		return fmt.Sprintf("%s<%d chars>%s", quote, len([]rune(literal))-2, quote)
	})
	text = cloudReasonBearer.ReplaceAllString(text, "Bearer <redacted>")
	text = cloudReasonAPIKey.ReplaceAllString(text, "<redacted>")
	text = cloudReasonOpaque.ReplaceAllStringFunc(text, func(run string) string {
		return fmt.Sprintf("<%d opaque chars>", len(run))
	})
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > cloudRejectionReasonLimit {
		text = string(runes[:cloudRejectionReasonLimit]) + "…"
	}
	return text
}

// cloudRPCRejectionReason renders one JSON-RPC error object as
// "grpc=<status> <sanitized message>"; empty when it carries no message.
func cloudRPCRejectionReason(raw json.RawMessage) string {
	if len(raw) == 0 || len(raw) > cloudErrorPayloadLimit {
		return ""
	}
	var value struct {
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	reason := cloudSanitizeUpstreamText(value.Message)
	if reason == "" {
		return ""
	}
	var data struct {
		GRPCStatus *int `json:"grpcStatusCode"`
	}
	if len(value.Data) != 0 && json.Unmarshal(value.Data, &data) == nil && data.GRPCStatus != nil {
		reason = "grpc=" + strconv.Itoa(*data.GRPCStatus) + " " + reason
	}
	return reason
}

// cloudRejectedItemShape describes the injected item a refusal names as
// items[N]: its type, field names and content part types, never values.
func cloudRejectedItemShape(items []any, reason string) string {
	match := cloudReasonItem.FindStringSubmatch(reason)
	if len(match) != 2 {
		return ""
	}
	index, err := strconv.Atoi(match[1])
	if err != nil || index < 0 || index >= len(items) {
		return ""
	}
	item, ok := items[index].(map[string]any)
	if !ok {
		return fmt.Sprintf("items[%d] %T", index, items[index])
	}
	shape := []string{fmt.Sprintf("items[%d]", index), "type=" + openAIHistoryItemType(item), "fields=[" + strings.Join(sortedKeys(item), ",") + "]"}
	for _, field := range []string{"content", "output", "summary"} {
		switch value := item[field].(type) {
		case nil:
		case []any:
			for _, raw := range value {
				part, ok := raw.(map[string]any)
				if !ok {
					shape = append(shape, fmt.Sprintf("%s:%T", field, raw))
					continue
				}
				kind, _ := part["type"].(string)
				shape = append(shape, field+":"+kind+"["+strings.Join(sortedKeys(part), ",")+"]")
			}
		default:
			shape = append(shape, fmt.Sprintf("%s:%T", field, value))
		}
	}
	return cloudSanitizeUpstreamText(strings.Join(shape, " "))
}
