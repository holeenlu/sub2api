package urlvalidator

import "testing"

func TestSafeImageURL(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"empty", "", ""},
		{"blank", " \t\n", ""},
		{"trim", "  /logo.svg  ", "/logo.svg"},
		{"root", "/", "/"},
		{"relative", "logo.svg", ""},
		{"protocol relative", "//example.com/logo.svg", ""},
		{"backslash", "/\\example.com/logo.svg", ""},
		{"control", "/logo.svg\r\nHeader:value", ""},
		{"tab", "https://exa\tmple.com/logo.svg", ""},
		{"uppercase scheme", "HTTPS://example.invalid/logo.png", "HTTPS://example.invalid/logo.png"},
		{"quoted query", "https://example.invalid/logo.png?x=\"&y=1", "https://example.invalid/logo.png?x=\"&y=1"},
		{"uppercase data", "DATA:IMAGE/PNG;BASE64,cG5n", "DATA:IMAGE/PNG;BASE64,cG5n"},
		{"data control", "data:image/svg+xml,<svg>\t</svg>", ""},
		{"bad host", "https://bad host/logo.svg", ""},
		{"missing authority", "https:logo.svg", ""},
		{"https", "https://example.com/logo.svg?a=1&b=2", "https://example.com/logo.svg?a=1&b=2"},
		{"http", "http://example.com/logo.svg", "http://example.com/logo.svg"},
		{"missing authority", "https:///logo.svg", ""},
		{"port without hostname", "https://:443/logo.svg", ""},
		{"invalid port", "https://example.com:bad/logo.svg", ""},
		{"credentials", "https://user:secret@example.com/logo.svg", ""},
		{"empty credentials", "https://@example.com/logo.svg", ""},
		{"javascript", "javascript:alert(1)", ""},
		{"file", "file:///logo.svg", ""},
		{"data image", "DATA:image/png;BASE64,cG5n", "DATA:image/png;BASE64,cG5n"},
		{"data HTML", "data:text/html,hello", ""},
		// Reference validation is separate from favicon payload decoding.
		{"missing data comma", "data:image/png;base64", "data:image/png;base64"},
		{"empty data payload", "data:image/png;base64,", "data:image/png;base64,"},
		{"invalid data payload", "data:image/png;base64,invalid", "data:image/png;base64,invalid"},
		{"invalid data escape", "data:image/svg+xml,%GG", "data:image/svg+xml,%GG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeImageURL(tc.input); got != tc.want {
				t.Fatalf("SafeImageURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
