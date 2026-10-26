package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGeminiThinking(t *testing.T) {
	for _, tc := range []struct{ model, config, level, budget, failure string }{
		{"gemini-3.8-flash", `{"thinkingLevel":"minimal"}`, "low", "", ""},
		{"gemini-3.1-pro-preview", `{"thinkingLevel":"xhigh"}`, "high", "", ""},
		{"gemini-3.1-pro-preview", `{"thinkingLevel":"medium"}`, "medium", "", ""},
		{"gemini-3-pro-preview", `{"thinkingLevel":"medium"}`, "low", "", ""},
		{"gemini-2.5-flash", `{"thinkingLevel":"high"}`, "", "24576", ""},
		{"gemini-2.5-flash", `{"thinkingBudget":0}`, "", "0", ""},
		{"gemini-2.5-pro", `{"thinkingBudget":-1}`, "", "-1", ""},
		{"gemini-2.5-pro", `{"thinkingBudget":0}`, "", "", "cannot disable"},
		{"gemini-3.8-flash", `{"thinkingBudget":0}`, "", "", "cannot disable"},
		{"gemini-2.5-flash-lite", `{"thinkingBudget":1}`, "", "", "between 512"},
		{"gemini-2.5-flash", `{"thinkingBudget":24577}`, "", "", "between 0"},
		{"gemini-2.5-pro", `{"thinkingBudget":-2}`, "", "", "integer"},
		{"gemini-2.5-pro", `{"thinkingBudget":1.5}`, "", "", "integer"},
		{"gemini-2.5-pro", `{"thinkingBudget":9223372036854775808}`, "", "", "integer"},
		{"gemini-3.8-flash", `{"thinkingLevel":"high","thinkingBudget":4096}`, "", "", "not both"},
		{"gemini-unknown", `{"thinkingLevel":"future","thinkingBudget":0}`, "future", "0", ""},
	} {
		t.Run(tc.model+tc.config, func(t *testing.T) {
			body := []byte(`{"generationConfig":{"thinkingConfig":` + tc.config + `,"maxOutputTokens":16},"contents":[{"parts":[{"thoughtSignature":"opaque","text":"keep"}]}],"future":true}`)
			got, err := normalizeGeminiThinking(body, tc.model)
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.level, gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingLevel").String())
			require.Equal(t, tc.budget, gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingBudget").Raw)
			for _, p := range []string{"contents", "future", "generationConfig.maxOutputTokens"} {
				require.Equal(t, gjson.GetBytes(body, p).Raw, gjson.GetBytes(got, p).Raw)
			}
			again, err := normalizeGeminiThinking(got, tc.model)
			require.NoError(t, err)
			require.Equal(t, got, again)
		})
	}
	body := []byte(`{"generation_config":{"thinking_config":{"thinking_level":"xhigh","include_thoughts":false}}}`)
	got, err := normalizeGeminiThinking(body, "gemini-3.8-flash")
	require.NoError(t, err)
	require.Equal(t, "high", gjson.GetBytes(got, "generation_config.thinking_config.thinking_level").String())
	require.Equal(t, "false", gjson.GetBytes(got, "generation_config.thinking_config.include_thoughts").Raw)
}

func TestClaudeToGeminiThinkingSeparatesModeBudgetAndVisibility(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"thoughtSignature":"signed"}]}]}`)
	request := []byte(`{"thinking":{"type":"enabled","budget_tokens":1024,"display":"omitted"},"output_config":{"effort":"high"}}`)
	got, err := applyClaudeThinkingToGemini(body, request, "gemini-2.5-pro")
	require.NoError(t, err)
	require.Equal(t, int64(1024), gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingBudget").Int())
	require.Equal(t, "false", gjson.GetBytes(got, "generationConfig.thinkingConfig.includeThoughts").Raw)
	require.Nil(t, extractGeminiReasoningEffortFromBody(got), "a token budget is not a billing level")
	require.Equal(t, gjson.GetBytes(body, "contents").Raw, gjson.GetBytes(got, "contents").Raw)
	_, err = applyClaudeThinkingToGemini(body, []byte(`{"thinking":{"type":"disabled"}}`), "gemini-3.1-pro-preview")
	require.ErrorContains(t, err, "cannot disable")
}

func TestClaudeToGeminiEffortPreservesVisibilityAndExtensions(t *testing.T) {
	body := []byte(`{"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4096,"future":7}}}`)
	got, err := applyClaudeThinkingToGemini(body, []byte(`{"output_config":{"effort":"high"}}`), "gemini-3.8-flash")
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(got, "generationConfig.thinkingConfig.includeThoughts").Bool())
	require.Equal(t, int64(7), gjson.GetBytes(got, "generationConfig.thinkingConfig.future").Int())
	require.Equal(t, "high", gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingLevel").String())
	require.False(t, gjson.GetBytes(got, "generationConfig.thinkingConfig.thinkingBudget").Exists())
}
