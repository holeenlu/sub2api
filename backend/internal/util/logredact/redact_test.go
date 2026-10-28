package logredact

import (
	"strings"
	"testing"
)

func TestRedactText_JSONLike(t *testing.T) {
	in := `{"access_token":"ya29.a0AfH6SMDUMMY","refresh_token":"1//0gDUMMY","other":"ok"}`
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction, got unchanged")
	}
	if want := `"access_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
	if want := `"refresh_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
}

func TestRedactText_QueryLike(t *testing.T) {
	in := "access_token=ya29.a0AfH6SMDUMMY refresh_token=1//0gDUMMY"
	out := RedactText(in)
	if strings.Contains(out, "ya29") || strings.Contains(out, "1//0") {
		t.Fatalf("expected tokens redacted, got %q", out)
	}
}

func TestRedactText_URLCredentials(t *testing.T) {
	for _, input := range []string{
		`request "https://alice:private-pass@api.example.test/v1?q=private-query&other=private-other" failed`,
		`connect wss://alice:private-pass@[2001:db8::1]/v1?custom=private-query;`,
		`{"error":{"message":"https://alice:private-pass@api.example.test/v1?custom=private-query"}}`,
		`bad https://alice:private-pass@api.example.test/%zz?custom=private-query`,
		`callback https://auth.example.test/#access_token=private-query`,
	} {
		t.Run(input, func(t *testing.T) {
			out := RedactText(input)
			for _, secret := range []string{"alice", "private-pass", "private-query", "private-other"} {
				if strings.Contains(out, secret) {
					t.Fatalf("credential %q survived in %q", secret, out)
				}
			}
		})
	}
	readable := `see 'https://docs.example.org/help(foo)' or https://docs.example.org/errors#quota; model gpt-6.1-sol and user@example.org`
	if out := RedactText(readable); out != readable {
		t.Fatalf("readable diagnostics changed: %q", out)
	}
	providerError := "provider error code: 403; model gpt-6.1-sol; " + readable
	if out := RedactURLCredentials(providerError); out != providerError {
		t.Fatalf("provider diagnostics changed: %q", out)
	}
	if out := RedactText(`request "https://api.example.test/v1?q=private-query" failed`); !strings.HasSuffix(out, `" failed`) {
		t.Fatalf("URL delimiters changed: %q", out)
	}
}

func TestRedactText_GOCSPX(t *testing.T) {
	in := "client_secret=GOCSPX-your-client-secret"
	out := RedactText(in)
	if strings.Contains(out, "your-client-secret") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "client_secret=***") {
		t.Fatalf("expected key redacted, got %q", out)
	}
}

func TestRedactText_GeminiAPIKeys(t *testing.T) {
	legacyKey := "AI" + "za" + strings.Repeat("Ab1_-", 7)
	authKey := "AQ." + "Ab" + strings.Repeat("Xy9_-z", 8)
	in := "upstream rejected " + legacyKey + " and " + authKey + " for model gemini-3.1-pro"
	out := RedactText(in)
	if strings.Contains(out, legacyKey) || strings.Contains(out, authKey) {
		t.Fatalf("expected Gemini keys redacted, got %q", out)
	}
	for _, want := range []string{"AIza***", "AQ.***", "for model gemini-3.1-pro"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in %q", want, out)
		}
	}
}

func TestRedactText_GeminiAuthKeyPrefixAloneUnchanged(t *testing.T) {
	in := "key format AQ.Ab... is documented"
	if out := RedactText(in); out != in {
		t.Fatalf("expected short AQ. prefix unchanged, got %q", out)
	}
}

func TestRedactText_ExtraKeyCacheUsesNormalizedSortedKey(t *testing.T) {
	clearExtraTextPatternCache()

	out1 := RedactText("custom_secret=abc", "Custom_Secret", " custom_secret ")
	out2 := RedactText("custom_secret=xyz", "custom_secret")
	if !strings.Contains(out1, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in first call, got %q", out1)
	}
	if !strings.Contains(out2, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in second call, got %q", out2)
	}

	if got := countExtraTextPatternCacheEntries(); got != 1 {
		t.Fatalf("expected 1 cached pattern set, got %d", got)
	}
}

func TestRedactText_DefaultPathDoesNotUseExtraCache(t *testing.T) {
	clearExtraTextPatternCache()

	out := RedactText("access_token=abc")
	if !strings.Contains(out, "access_token=***") {
		t.Fatalf("expected default key redacted, got %q", out)
	}
	if got := countExtraTextPatternCacheEntries(); got != 0 {
		t.Fatalf("expected extra cache to remain empty, got %d", got)
	}
}

func clearExtraTextPatternCache() {
	extraTextPatternCache.Range(func(key, value any) bool {
		extraTextPatternCache.Delete(key)
		return true
	})
}

func countExtraTextPatternCacheEntries() int {
	count := 0
	extraTextPatternCache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
