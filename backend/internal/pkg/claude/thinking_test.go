package claude

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeThinking(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, mode, effort, failure string
	}{
		{"sonnet_manual", "claude-sonnet-5-5", `{"thinking":{"type":"enabled","budget_tokens":4096}}`, "adaptive", "medium", ""},
		{"sonnet_disabled", "claude-sonnet-5-5", `{"thinking":{"type":"disabled","budget_tokens":4096,"display":"omitted","block_binding":"x"},"output_config":{"effort":"max"}}`, "between_tools", "high", ""},
		{"fable_disabled", "claude-fable-5-1", `{"thinking":{"type":"disabled"}}`, "", "", "does not support thinking.type=disabled"},
		{"opus_disabled", "claude-opus-5-5", `{"thinking":{"type":"disabled"}}`, "", "", "does not support thinking.type=disabled"},
		{"opus5_disabled_high", "claude-opus-5", `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`, "disabled", "high", ""},
		{"opus5_disabled_varied_history", "claude-opus-5", `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"},"messages":[{"role":"user","output_config":{"effort":"low"},"content":"hello"},{"role":"assistant","output_config":{"effort":"medium"},"content":"hi"}]}`, "disabled", "high", ""},
		{"opus5_alias_disabled_history", "us.anthropic.claude-opus-5-v1", `{"thinking":{"type":"disabled"},"messages":[{"role":"user","output_config":{"effort":"low"},"content":"hello"}]}`, "disabled", "", ""},
		{"haiku55_disabled_history_change", "claude-haiku-5-5", `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"},"messages":[{"role":"user","output_config":{"effort":"low"},"content":"hello"}]}`, "", "", "per-message effort"},
		{"haiku55_disabled_default_history", "claude-haiku-5-5", `{"thinking":{"type":"disabled"},"messages":[{"role":"user","output_config":{"effort":"medium"},"content":"hello"}]}`, "disabled", "", ""},
		{"haiku55_disabled_default_history_change", "claude-haiku-5-5", `{"thinking":{"type":"disabled"},"messages":[{"role":"user","output_config":{"effort":"high"},"content":"hello"}]}`, "", "", "per-message effort"},
		{"sonnet55_disabled_default_history", "claude-sonnet-5-5", `{"thinking":{"type":"disabled"},"messages":[{"role":"user","output_config":{"effort":"high"},"content":"hello"}]}`, "between_tools", "", ""},
		{"sonnet55_disabled_explicit_history_change", "claude-sonnet-5-5", `{"thinking":{"type":"disabled"},"output_config":{"effort":"low"},"messages":[{"role":"user","output_config":{"effort":"high"},"content":"hello"}]}`, "", "", "per-message effort"},
		{"sonnet55_between_tools_default_history", "claude-sonnet-5-5", `{"thinking":{"type":"between_tools"},"messages":[{"role":"user","output_config":{"effort":"high"},"content":"hello"}]}`, "between_tools", "", ""},
		{"opus5_disabled_max", "claude-opus-5", `{"thinking":{"type":"disabled"},"output_config":{"effort":"max"}}`, "", "", "supports only low"},
		{"fable_xhigh", "claude-fable-5-1", `{"thinking":{"type":"enabled","budget_tokens":4096},"output_config":{"effort":"xhigh"}}`, "adaptive", "xhigh", ""},
		{"opus46_xhigh", "claude-opus-4-6", `{"output_config":{"effort":"xhigh"}}`, "", "max", ""},
		{"opus45_max", "claude-opus-4-5", `{"output_config":{"effort":"max"}}`, "", "high", ""},
		{"old_no_effort", "claude-haiku-4-5", `{"thinking":{"type":"enabled","budget_tokens":1024}}`, "enabled", "", ""},
		{"between_tools_max", "claude-sonnet-5-5", `{"thinking":{"type":"between_tools"},"output_config":{"effort":"max"}}`, "", "", "supports only low"},
		{"between_tools_history", "claude-sonnet-5-5", `{"thinking":{"type":"disabled"},"messages":[{"role":"user","output_config":{"effort":"max"},"content":"hi"}]}`, "", "", "per-message effort"},
		{"unknown_model", "claude-fable-5-99", `{"thinking":{"type":"disabled"},"output_config":{"effort":"xhigh"}}`, "disabled", "xhigh", ""},
		{"unknown_enum", "claude-fable-5-1", `{"thinking":{"type":"future-mode"},"output_config":{"effort":"future-effort"}}`, "future-mode", "future-effort", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(tc.body)
			copyOfOriginal := bytes.Clone(original)
			got, err := NormalizeThinking(original, tc.model)
			require.Equal(t, copyOfOriginal, original, "must not mutate caller's retry input")
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.mode, gjson.GetBytes(got, "thinking.type").String())
			require.Equal(t, tc.effort, gjson.GetBytes(got, "output_config.effort").String())
			require.Equal(t, gjson.GetBytes(original, "messages").Raw, gjson.GetBytes(got, "messages").Raw, "historical overrides must remain unchanged")
			if tc.effort == "" && !gjson.GetBytes(original, "output_config.effort").Exists() {
				require.False(t, gjson.GetBytes(got, "output_config.effort").Exists(), "omitted effort must remain omitted")
			}
			if tc.mode == "adaptive" || tc.mode == "between_tools" {
				require.False(t, gjson.GetBytes(got, "thinking.budget_tokens").Exists())
			}
			again, err := NormalizeThinking(got, tc.model)
			require.NoError(t, err)
			require.Equal(t, got, again, "normalization must be idempotent")
		})
	}
}

func TestNormalizeThinkingPreservesHistoryAndExtensions(t *testing.T) {
	body := []byte(`{"future":{"x":1},"max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":4096,"display":"omitted","block_binding":"prefix","future":7},"output_config":{"effort":"xhigh","format":{"type":"json_schema"},"future":3},"messages":[{"role":"assistant","output_config":{"effort":"high"},"content":[{"type":"thinking","thinking":"hidden","signature":"opaque-signed"}]}]}`)
	got, err := NormalizeThinking(body, "claude-fable-5-1")
	require.NoError(t, err)
	for _, path := range []string{"future", "max_tokens", "messages", "thinking.display", "thinking.block_binding", "thinking.future", "output_config"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(got, path).Raw, path)
	}
}

func TestThinkingBudgetBoundaries(t *testing.T) {
	for _, max := range []int64{1, 1023, 1024, 1025, 8192, 32768, 9223372036854775807} {
		budget, err := DerivedThinkingBudget("max", max)
		if max <= 1024 {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.GreaterOrEqual(t, budget, 1024)
		require.Less(t, int64(budget), max)
	}
	for _, raw := range []string{"0", "-1", "1023", "1024.5", "9223372036854775808"} {
		_, err := NormalizeThinking([]byte(`{"thinking":{"type":"enabled","budget_tokens":`+raw+`}}`), "claude-fable-5-1")
		require.Error(t, err, raw)
	}
	// Explicit native manual budgets still go through the existing upstream
	// validation/optional repair contract, rather than silently raising max_tokens.
	body := []byte(`{"max_tokens":1024,"thinking":{"type":"enabled","budget_tokens":2048}}`)
	got, err := NormalizeThinking(body, "claude-opus-4-5")
	require.NoError(t, err)
	require.Equal(t, body, got)
}
