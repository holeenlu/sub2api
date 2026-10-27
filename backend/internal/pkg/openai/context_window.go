package openai

import "strings"

// IsContextWindowErrorText reports whether any value reads as an upstream
// "context window exhausted" error. It is the single owner of the phrase rules
// shared by the native gateway classifier and the basispoints in-stream
// classifier (basispoints cannot import service). Callers decide which fields
// to pass; matched text is used for classification only, never echoed.
func IsContextWindowErrorText(values ...string) bool {
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "context_too_large") || strings.Contains(lower, "context_length_exceeded") {
			return true
		}
		if strings.Contains(lower, "maximum context length") || strings.Contains(lower, "max context length") {
			return true
		}
		exceeded := strings.Contains(lower, "exceed") || strings.Contains(lower, "too large") || strings.Contains(lower, "too long")
		if (strings.Contains(lower, "context window") || strings.Contains(lower, "context length")) && exceeded {
			return true
		}
		if strings.Contains(lower, "token limit") && strings.Contains(lower, "context") && exceeded {
			return true
		}
	}
	return false
}
