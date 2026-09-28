package basispoints

import "testing"

func TestNativeFallbackReason(t *testing.T) {
	cases := []struct {
		body, want string
	}{
		{`{"tools":[{"type":"image_generation"}]}`, "image_generation"},
		{`{"tools":[{"type":"web_search","external_web_access":true}]}`, "web_search"},
		{`{"tools":[{"type":"function","name":"exec"}]}`, ""},
		{`{"tools":[{"type":"web_search","external_web_access":true}],"tool_choice":"none"}`, ""},
		{`{"tool_choice":{"type":"web_search"}}`, "tool_choice"},
		{`{"tool_choice":{"type":"image_generation"}}`, "tool_choice"},
		{`{"tool_choice":{"type":"function","name":"web_search_client"}}`, ""},
		{`{"tool_choice":{"type":"custom","name":"image_generation_client"}}`, ""},
	}
	for _, tc := range cases {
		if got := NativeFallbackReason([]byte(tc.body)); got != tc.want {
			t.Errorf("reason=%q want %q for %s", got, tc.want, tc.body)
		}
	}
}

func TestRelaxHostedToolChoice(t *testing.T) {
	cases := []struct{ body, want, kind string }{
		{`{"tool_choice":{"type":"web_search"},"tools":[{"type":"web_search"}]}`, `{"tool_choice":"auto","tools":[{"type":"web_search"}]}`, "web_search"},
		{`{"tool_choice":{"type":"image_generation"}}`, `{"tool_choice":"auto"}`, "image_generation"},
		{`{"tool_choice":{"type":"web_search_preview_2025_03_11"}}`, `{"tool_choice":"auto"}`, "web_search_preview_2025_03_11"},
		{`{"tool_choice":{"type":"function","name":"lookup"}}`, `{"tool_choice":{"type":"function","name":"lookup"}}`, ""},
		{`{"tool_choice":"required"}`, `{"tool_choice":"required"}`, ""},
		{`{"tool_choice":"none"}`, `{"tool_choice":"none"}`, ""},
		{`{"input":"no choice"}`, `{"input":"no choice"}`, ""},
		{`not json`, `not json`, ""},
	}
	for _, tc := range cases {
		got, kind, err := RelaxHostedToolChoice([]byte(tc.body))
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tc.body, err)
		}
		if string(got) != tc.want || kind != tc.kind {
			t.Errorf("RelaxHostedToolChoice(%s) = %s, %q; want %s, %q", tc.body, got, kind, tc.want, tc.kind)
		}
	}
}
