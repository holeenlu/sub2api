package basispoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const maxEnvelopeBytes = 1 << 20

// decodeTransportCode unwraps common model formatting without evaluating code.
// Each layer must contain one complete JSON value; ambiguous batches are rejected.
func decodeTransportCode(value any) (object, error) {
	original := value
	for depth := 0; depth < 4; depth++ {
		if item, ok := value.(object); ok && item != nil {
			return item, nil
		}
		raw, ok := value.(string)
		if !ok || len(raw) > maxEnvelopeBytes {
			break
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			break
		}
		if decoded, ok := decodeEnvelopeValue(raw); ok {
			value = decoded
			continue
		}
		// Strip a complete Markdown fence, optionally preceded by a short prose label.
		if start := strings.Index(raw, "```"); start >= 0 && prosePrefix(raw[:start]) {
			fenced := raw[start:]
			newline := strings.IndexByte(fenced, '\n')
			if newline >= 0 && strings.HasSuffix(fenced, "```") {
				language := strings.TrimSpace(fenced[3:newline])
				if language == "" || strings.EqualFold(language, "json") {
					value = strings.TrimSpace(fenced[newline+1 : len(fenced)-3])
					continue
				}
			}
		}
		if start := strings.IndexByte(raw, '{'); start > 0 && prosePrefix(raw[:start]) {
			if decoded, ok := decodeEnvelopeValue(raw[start:]); ok {
				value = decoded
				continue
			}
		}
		break
	}
	return nil, fmt.Errorf("basispoints tool transport code must contain one JSON client-tool envelope; OfficeJS and multiple calls are unsupported (%s)", transportShape(original))
}

// Report only structural facts, never client code, prompts or tool arguments.
func transportShape(value any) string {
	raw, ok := value.(string)
	if !ok {
		if value == nil {
			return "format=missing"
		}
		return fmt.Sprintf("format=non_string_%T", value)
	}
	trimmed := strings.TrimSpace(raw)
	format := "text_or_code"
	switch {
	case trimmed == "":
		format = "empty"
	case strings.HasPrefix(trimmed, "```"):
		format = "markdown"
	case strings.HasPrefix(trimmed, "{"):
		format = "json_object"
	case strings.HasPrefix(trimmed, "["):
		format = "json_array"
	case strings.HasPrefix(trimmed, `"`):
		format = "json_string"
	}
	detail := fmt.Sprintf("format=%s; bytes=%d", format, len(raw))
	var syntax *json.SyntaxError
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); errors.As(err, &syntax) {
		detail += fmt.Sprintf("; json_offset=%d; json_failure=%s", syntax.Offset, jsonFailureKind(raw, syntax))
	}
	return detail
}

// Classify the parser's structural context without returning its message, which
// can contain a byte from the caller's code. Offsets use the original wire text.
func jsonFailureKind(raw string, syntax *json.SyntaxError) string {
	if syntax == nil {
		return "unknown"
	}
	message := syntax.Error()
	if syntax.Offset > 0 && syntax.Offset <= int64(len(raw)) && raw[syntax.Offset-1] < 0x20 {
		return "raw_control"
	}
	if syntax.Offset > 1 && raw[syntax.Offset-2] == '\\' {
		return "invalid_escape"
	}
	if syntax.Offset > 5 && raw[syntax.Offset-6] == '\\' && raw[syntax.Offset-5] == 'u' {
		return "invalid_escape"
	}
	switch {
	case strings.Contains(message, "unexpected end of JSON input"):
		return "unexpected_eof"
	case strings.Contains(message, "after top-level value"):
		return "trailing_data"
	case strings.Contains(message, "in string escape code"), strings.Contains(message, "in \\u hexadecimal character escape"):
		return "invalid_escape"
	case strings.Contains(message, "in string literal"):
		if syntax.Offset > 0 && syntax.Offset <= int64(len(raw)) && raw[syntax.Offset-1] < 0x20 {
			return "raw_control"
		}
		return "invalid_string"
	case strings.Contains(message, "after object key"), strings.Contains(message, "after array element"):
		return "missing_separator"
	default:
		return "unexpected_token"
	}
}

func decodeTransportEnvelope(value any) (object, error) {
	for depth := 0; depth < 3; depth++ {
		envelope, err := decodeTransportCode(value)
		if err != nil {
			return nil, err
		}
		name, err := envelopeName(envelope)
		if err != nil {
			return nil, err
		}
		if name != "run_officejs" && name != "functions.run_officejs" {
			return envelope, nil
		}
		args, err := envelopeArguments(envelope)
		if err != nil {
			return nil, err
		}
		if raw, ok := args.(string); ok {
			var parsed object
			if decode([]byte(raw), &parsed) != nil {
				return nil, fmt.Errorf("basispoints nested transport arguments must be one JSON object")
			}
			args = parsed
		}
		outer, ok := args.(object)
		if !ok {
			return nil, fmt.Errorf("basispoints nested transport arguments must be an object")
		}
		value = outer["code"]
	}
	return nil, fmt.Errorf("basispoints tool transport exceeds two nested wrappers")
}

var catalogInvocation = regexp.MustCompile(`^(?:(?:return\s+)?await\s+|return\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*\(`)

// recoverTransportEnvelope accepts one complete catalog invocation whose sole
// argument is a JSON literal. It never evaluates code or extracts an object from
// a program, batch, incomplete call or trailing text. The callee selects the tool;
// fields named name/arguments inside the literal remain ordinary tool arguments.
func recoverTransportEnvelope(value any, catalog map[string]tool) (object, bool) {
	raw, ok := value.(string)
	if !ok || len(raw) > maxEnvelopeBytes {
		return nil, false
	}
	raw = strings.TrimSpace(raw)
	match := catalogInvocation.FindStringSubmatch(raw)
	if len(match) != 2 {
		// Retain the existing complete JSON/fence/prose forms without searching
		// arbitrary text for an executable-looking object.
		envelope, err := decodeTransportEnvelope(raw)
		if err != nil {
			return nil, false
		}
		name, err := envelopeName(envelope)
		_, _, known := lookupCatalogTool(catalog, name)
		return envelope, err == nil && known
	}
	name := match[1]
	name, info, known := lookupCatalogTool(catalog, name)
	if !known {
		return nil, false
	}
	argument := raw[len(match[0]):]
	decoder := json.NewDecoder(strings.NewReader(argument))
	decoder.UseNumber()
	var literal any
	if decoder.Decode(&literal) != nil {
		return nil, false
	}
	tail := strings.TrimSpace(argument[decoder.InputOffset():])
	if !strings.HasPrefix(tail, ")") {
		return nil, false
	}
	if tail = strings.TrimSpace(tail[1:]); tail != "" && tail != ";" {
		return nil, false
	}
	switch info.Kind {
	case "function":
		if args, ok := literal.(object); ok && args != nil {
			return object{"name": name, "arguments": args}, true
		}
	case "custom":
		if input, ok := literal.(string); ok {
			return object{"name": name, "input": input}, true
		}
	}
	return nil, false
}

func envelopeName(envelope object) (string, error) {
	name := text(envelope["name"])
	alias := text(envelope["tool"])
	if name != "" && alias != "" && name != alias {
		return "", fmt.Errorf("basispoints tool envelope contains conflicting names")
	}
	if name == "" {
		name = alias
	}
	return name, nil
}

func envelopeArguments(envelope object) (any, error) {
	args, exists := envelope["arguments"]
	alias, hasAlias := envelope["args"]
	if exists && hasAlias {
		return nil, fmt.Errorf("basispoints tool envelope contains conflicting argument fields")
	}
	if !exists {
		args = alias
	}
	return args, nil
}

func prosePrefix(prefix string) bool {
	return len(prefix) <= 512 && !strings.ContainsAny(prefix, "{}[]();=`\"")
}

// Unmarked JSON-looking data that cannot be decoded must not become executable
// source during model correction. Explicit CUSTOM/CODE/CMD bypass this check.
func looksLikeTransportJSON(raw string) bool {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, `"`) {
		return true
	}
	if start := strings.Index(raw, "```"); start >= 0 && prosePrefix(raw[:start]) {
		return true
	}
	start := strings.IndexByte(raw, '{')
	return start > 0 && prosePrefix(raw[:start])
}

func decodeEnvelopeValue(raw string) (any, bool) {
	var value any
	if decode([]byte(raw), &value) == nil {
		return value, true
	}
	// Escape raw line breaks and tabs inside strings, preserving those exact
	// characters, and repair illegal backslash escapes only after strict decoding
	// fails. Do not combine string repairs with separator recovery: that could
	// reinterpret an unescaped quote as an argument boundary.
	fixed := repairTransportJSONStrings(raw)
	if fixed != raw && decode([]byte(fixed), &value) == nil {
		return value, true
	}
	return decodeSeparatedEnvelopeValue(raw)
}

// Recover only commas/colons between complete, whitespace-separated JSON tokens.
// The strict parser chooses the punctuation; quotes, values and delimiters are
// never inserted, removed or rewritten. Bound both input size and repair work.
func decodeSeparatedEnvelopeValue(raw string) (any, bool) {
	for repairs := 0; repairs <= 8 && len(raw) <= maxEnvelopeBytes; repairs++ {
		var value any
		err := decode([]byte(raw), &value)
		if err == nil {
			return value, uniqueEnvelopeKeys(raw)
		}
		var syntax *json.SyntaxError
		if repairs == 8 || !errors.As(err, &syntax) {
			break
		}
		at := int(syntax.Offset) - 1
		if at <= 0 || at >= len(raw) || !strings.ContainsRune(" \t\r\n", rune(raw[at-1])) {
			break // adjacent tokens may be broken quotes, numbers or literals
		}
		separator := ":"
		switch message := syntax.Error(); {
		case strings.HasSuffix(message, "after object key:value pair"):
			if raw[at] != '"' {
				return nil, false
			}
			separator = ","
		case strings.HasSuffix(message, "after array element"):
			separator = ","
		case strings.HasSuffix(message, "after object key"):
		default:
			return nil, false
		}
		if !strings.ContainsRune(`"{[tfn-0123456789`, rune(raw[at])) {
			break
		}
		raw = raw[:at] + separator + raw[at:]
	}
	return nil, false
}

// A repaired object must not hide an earlier value behind a duplicate key,
// including escaped aliases. Use tokens to check every nested object without
// recursing or changing the decoder's number and string semantics.
func uniqueEnvelopeKeys(raw string) bool {
	type frame struct {
		keys map[string]bool
		key  bool
	}
	var stack []frame
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	for {
		token, err := d.Token()
		if err != nil {
			return err == io.EOF && len(stack) == 0
		}
		delim, _ := token.(json.Delim)
		if delim == '}' || delim == ']' {
			stack = stack[:len(stack)-1]
			continue
		}
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			if parent.keys != nil {
				if parent.key {
					key, ok := token.(string)
					if !ok || parent.keys[key] {
						return false
					}
					parent.keys[key], parent.key = true, false
					continue
				}
				parent.key = true
			}
		}
		if delim == '{' {
			stack = append(stack, frame{keys: make(map[string]bool), key: true})
		} else if delim == '[' {
			stack = append(stack, frame{})
		}
	}
}

func repairTransportJSONStrings(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))
	quoted := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch == '"' {
			quoted = !quoted
		}
		if quoted {
			switch ch {
			case '\n':
				_, _ = out.WriteString(`\n`)
				continue
			case '\r':
				_, _ = out.WriteString(`\r`)
				continue
			case '\t':
				_, _ = out.WriteString(`\t`)
				continue
			}
		}
		if ch != '\\' || !quoted || i+1 >= len(raw) {
			_ = out.WriteByte(ch)
			continue
		}
		next := raw[i+1]
		valid := strings.ContainsRune(`"\/bfnrt`, rune(next))
		if next == 'u' && i+5 < len(raw) {
			valid = true
			for _, digit := range raw[i+2 : i+6] {
				if !strings.ContainsRune("0123456789abcdefABCDEF", digit) {
					valid = false
				}
			}
		}
		_ = out.WriteByte('\\')
		if valid {
			_ = out.WriteByte(next)
			i++
		} else {
			_ = out.WriteByte('\\')
		}
	}
	return out.String()
}
