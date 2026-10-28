package openai

import "testing"

func TestIsContextWindowErrorText(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   bool
	}{
		{"empty", []string{"", "  "}, false},
		{"code context_length_exceeded", []string{"context_length_exceeded"}, true},
		{"code context_too_large", []string{"CONTEXT_TOO_LARGE"}, true},
		{"maximum context length", []string{"This model's maximum context length is 400000 tokens."}, true},
		{"context window exceeded", []string{"Your input exceeds the context window of this model."}, true},
		{"context length too long", []string{"context length is too long"}, true},
		{"token limit with context", []string{"request exceeds the token limit for this context"}, true},
		{"token limit without context", []string{"rate token limit exceeded"}, false},
		{"unrelated", []string{"invalid_request_error", "model not found"}, false},
		{"second value matches", []string{"server_error", "max context length reached"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsContextWindowErrorText(tc.values...); got != tc.want {
				t.Fatalf("IsContextWindowErrorText(%q) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}
