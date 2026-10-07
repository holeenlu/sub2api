package main

import "testing"

func TestNormalizeUpstreamVersion(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		fallback string
		want     string
	}{
		{name: "ldflags with v prefix", injected: "v0.2.1", fallback: "0.1.0", want: "v0.2.1"},
		{name: "ldflags without v prefix", injected: "0.2.1", fallback: "0.1.0", want: "v0.2.1"},
		{name: "ldflags whitespace trimmed", injected: "  v0.2.1\n", fallback: "", want: "v0.2.1"},
		{name: "falls back to embedded VERSION", injected: "", fallback: "0.1.146\n", want: "v0.1.146"},
		{name: "empty when nothing is known", injected: "", fallback: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeUpstreamVersion(tt.injected, tt.fallback); got != tt.want {
				t.Fatalf("normalizeUpstreamVersion(%q, %q) = %q, want %q", tt.injected, tt.fallback, got, tt.want)
			}
		})
	}
}
