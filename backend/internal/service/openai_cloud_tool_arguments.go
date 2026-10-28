package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// projectToolArguments owns only the outward spelling of already validated RPC
// arguments. JSON Schema's integer values can arrive as 7500.0 or 7.5e3, which
// strict client integer decoders reject. This value-preserving projection needs
// no second schema interpreter; custom identity comes from the compiled catalog.
// The single pending-tool copy feeds delta/done, history, terminal and replay.
func (p *cloudProtocolRequest) projectToolArguments(name string, raw json.RawMessage) (string, error) {
	if p.customTools[name] {
		return string(raw), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var projected strings.Builder
	copied := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errors.New("Cloud tool arguments are invalid")
		}
		number, ok := token.(json.Number)
		if !ok {
			continue
		}
		original := number.String()
		// Expand only values a strict 64-bit integer consumer can use. The
		// result budget belongs to the whole public response, not this token.
		next := cloudIntegralToolNumber(original)
		if next == original {
			continue
		}
		end := int(decoder.InputOffset())
		projected.Write(raw[copied : end-len(original)])
		projected.WriteString(next)
		copied = end
	}
	if copied == 0 {
		return string(raw), nil
	}
	projected.Write(raw[copied:])
	return projected.String(), nil
}

// number is a validated JSON number token. Shortening exact integers is safe at
// any precision; expansion is useful only within int64/uint64 consumer ranges.
// Fractions and larger exponents stay byte-for-byte intact. Decimal digits never
// pass through floating point, and a short exponent cannot allocate a huge value.
func cloudIntegralToolNumber(number string) string {
	if !strings.ContainsAny(number, ".eE") {
		return number
	}
	digits, exponentText := number, "0"
	if i := strings.IndexAny(digits, "eE"); i >= 0 {
		digits, exponentText = digits[:i], digits[i+1:]
	}
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	fractionDigits := 0
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		fractionDigits = len(digits) - i - 1
		digits = digits[:i] + digits[i+1:]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0"
	}
	// An exponent outside int64 cannot be offset by a bounded RPC mantissa.
	// ParseInt also bounds work to the exponent text instead of its value.
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil {
		return number
	}
	trimmed := strings.TrimRight(digits, "0")
	integerExponent := int64(fractionDigits - (len(digits) - len(trimmed)))
	// Both uint64's maximum and int64's signed minimum fit in 20 bytes.
	// Longer inputs may shrink, but cannot use their size as growth allowance.
	zerosAvailable := max(len(number), 20) - len(sign) - len(trimmed)
	if exponent < integerExponent || zerosAvailable < 0 || exponent > integerExponent+int64(zerosAvailable) {
		return number
	}
	integer := sign + trimmed + strings.Repeat("0", int(exponent-integerExponent))
	if len(integer) > len(number) {
		if sign == "-" {
			_, err = strconv.ParseInt(integer, 10, 64)
		} else {
			_, err = strconv.ParseUint(integer, 10, 64)
		}
		if err != nil {
			return number
		}
	}
	return integer
}

// toolOutputItem is shared by the publication size check and the actual stream.
// Custom input remains opaque; tool_search and namespace restoration still use
// the existing public codec after JSON encoding.
func (a *cloudSessionActor) toolOutputItem(tool *cloudPendingTool) map[string]any {
	if a.protocol != nil && a.protocol.customTools[tool.Name] {
		return map[string]any{"type": "custom_tool_call", "id": "ctc_" + tool.CallID, "call_id": tool.CallID, "name": tool.Name, "input": cloudCustomToolInput(tool.Arguments), "status": "completed"}
	}
	return map[string]any{"type": "function_call", "id": "fc_" + tool.CallID, "call_id": tool.CallID, "name": tool.Name, "arguments": tool.Arguments, "status": "completed"}
}

// checkToolPublication checks the complete tool-wait response before publishing
// any tool identity or arguments. Raw argument lengths omit existing output,
// JSON string escaping, metadata, public names and the terminal event envelope.
// A size rejection must not mark a call or sampling step as already published.
func (a *cloudSessionActor) checkToolPublication(pending []*cloudPendingTool) error {
	var usage *OpenAICloudUsage
	if !a.lastStepPublished {
		usage = a.lastStep
	}
	body := a.response("completed", usage)
	output := make([]map[string]any, 0, len(a.output)+len(pending))
	output = append(output, a.output...)
	sequence := a.sequence
	for _, tool := range pending {
		output = append(output, a.toolOutputItem(tool))
		sequence += 4 // added, delta, arguments/input done, item done
		if a.protocol != nil && a.protocol.toolSearch && tool.Name == "tool_search" {
			sequence -= 2 // tool_search publishes only its item lifecycle
		}
	}
	body["output"] = output
	raw, err := json.Marshal(body)
	if err != nil {
		return errors.New("Cloud response encoding failed")
	}
	if a.protocol != nil {
		raw = cloudRestorePayload(raw, a.protocol.names, a.protocol.toolSearch)
	}
	if len(raw) > OpenAICloudMaxResultBytes {
		return errors.New("Cloud response exceeded its size limit")
	}
	// Use the same body and codec as emit, then include the store envelope.
	// The completed response contains every tool's full value and bounds each
	// smaller item/argument event as well. Do not spend its reserved headroom
	// as extra response-body space.
	event, err := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": sequence, "response": body})
	if err != nil {
		return errors.New("Cloud response encoding failed")
	}
	if a.protocol != nil {
		event = cloudRestorePayload(event, a.protocol.names, a.protocol.toolSearch)
	}
	envelope, err := json.Marshal(OpenAICloudStreamEvent{Event: event})
	if err != nil {
		return errors.New("Cloud response encoding failed")
	}
	if len(envelope) > OpenAICloudMaxEnvelopeBytes {
		return errors.New("Cloud response event exceeded its size limit")
	}
	return nil
}
