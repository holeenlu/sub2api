package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

var (
	opsDetailBearer     = regexp.MustCompile(`(?i)\b(Bearer|Basic)[ \t]+[A-Za-z0-9._~+/-]+=*`)
	opsDetailAPIKey     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)
	opsDetailPrivateKey = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z0-9]+ )?PRIVATE KEY-----`)
	opsDetailAssignment = regexp.MustCompile(`(?i)(["']?([a-z][a-z0-9_.-]*)["']?\s*[:=]\s*)("(?:\\.|[^"\\])*"|'[^']*'|[^\s,;&}\]]+)`)
)

// sanitizeOpsDetailCredentials keeps the complete recorded detail, including
// unknown event fields. It never reapplies storage limits or permission filters.
// Only string fields are replaced on this copy; the source DTO is untouched.
func sanitizeOpsDetailCredentials(detail *OpsErrorLogDetail) *OpsErrorLogDetail {
	if detail == nil {
		return nil
	}
	out := *detail
	for _, field := range []*string{
		&out.Message, &out.ErrorBody, &out.UpstreamErrorMessage,
		&out.UpstreamErrorDetail, &out.UpstreamErrors, &out.RequestPath,
		&out.InboundEndpoint, &out.UpstreamEndpoint, &out.UserAgent,
	} {
		*field = sanitizeOpsDetailText(*field)
	}
	return &out
}

func sanitizeOpsDetailText(raw string) string {
	if json.Valid([]byte(raw)) {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) == nil {
			clean := sanitizeOpsDetailValue(value, false)
			if encoded, err := json.Marshal(clean); err == nil {
				return string(encoded)
			}
		}
	}
	// SSE keeps its event framing; each data JSON is sanitized structurally.
	raw = opsDetailPrivateKey.ReplaceAllString(raw, "[REDACTED]")
	lines := strings.SplitAfter(raw, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[len("data:"):])
			if json.Valid([]byte(payload)) {
				start := strings.Index(line, payload)
				lines[i] = line[:start] + sanitizeOpsDetailText(payload) + line[start+len(payload):]
				continue
			}
		}
		lines[i] = redactOpsDetailPlainText(line)
	}
	return strings.Join(lines, "")
}

func sanitizeOpsDetailValue(value any, schemaProperties bool) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if opsDetailCredentialKey(key) {
				// Property names in a tool schema describe inputs, not credentials.
				if schema, ok := item.(map[string]any); schemaProperties && ok {
					for _, preset := range []string{"default", "examples", "const", "enum"} {
						if _, present := schema[preset]; present {
							schema[preset] = "[REDACTED]"
						}
					}
				} else if _, booleanSchema := item.(bool); !schemaProperties || !booleanSchema {
					v[key] = "[REDACTED]"
					continue
				}
			}
			properties := key == "properties" || key == "patternProperties" || key == "$defs" || key == "definitions"
			v[key] = sanitizeOpsDetailValue(item, properties)
		}
	case []any:
		for i := range v {
			v[i] = sanitizeOpsDetailValue(v[i], false)
		}
	case string:
		return sanitizeOpsDetailText(v)
	}
	return value
}

// The storage key heuristic also covers session IDs and token counters. Export
// must preserve those diagnostics, so use the shared credential keys plus actual
// header/credential aliases instead of deleting every token/session-named field.
func opsDetailCredentialKey(key string) bool {
	normalize := func(s string) string {
		return strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	}
	k := normalize(key)
	for _, secret := range domain.SensitiveCredentialKeys {
		if k == normalize(secret) {
			return true
		}
	}
	switch k {
	case "authorization", "proxyauthorization", "xapikey", "apikeys", "token",
		"sessiontoken", "clientsecret", "secret", "passwd", "passphrase", "jwt",
		"signature", "secretaccesskey", "accesskeyid", "codeverifier", "authorizationcode", "cookies":
		return true
	}
	return false
}

func redactOpsDetailPlainText(raw string) string {
	out := logredact.RedactURLCredentials(raw)
	out = opsDetailPrivateKey.ReplaceAllString(out, "[REDACTED]")
	out = opsDetailBearer.ReplaceAllString(out, `${1} [REDACTED]`)
	out = opsDetailAPIKey.ReplaceAllString(out, "[REDACTED]")
	return opsDetailAssignment.ReplaceAllStringFunc(out, func(match string) string {
		parts := opsDetailAssignment.FindStringSubmatch(match)
		if !opsDetailCredentialKey(parts[2]) {
			return match
		}
		return parts[1] + "[REDACTED]"
	})
}
