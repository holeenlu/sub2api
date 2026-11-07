package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const localMinimalPromptTemplate = `[{"type":"text","text":"{billing_header}"},{"type":"text","text":"{claude_code_system_prompt}"},{"type":"text","text":"{caller_system}","drop_if_empty":true}]`
const localFullPromptTemplate = `[{"type":"text","text":"{billing_header}"},{"type":"text","text":"{claude_code_system_prompt}"},{"type":"text","text":"{claude_code_expansion_prompt}","cache_control":{"type":"ephemeral","ttl":"5m"}},{"type":"text","text":"{caller_system}","drop_if_empty":true}]`

func TestClaudePromptTemplates_ExplicitEmptyIsZero(t *testing.T) {
	for _, template := range []string{`[]`, `{"blocks":[]}`} {
		for _, body := range []string{
			`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`,
			`{"model":"claude-sonnet-4-6","system":"  You are OpenCode, the best coding agent on the planet.  ","messages":[{"role":"user","content":"hello"}]}`,
			`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"{billing_header}","cache_control":{"type":"ephemeral"}}],"messages":[]}`,
		} {
			require.NoError(t, ValidateClaudeOAuthSystemPromptBlocksConfig(template))
			out := rewriteSystemForNonClaudeCodeWithPromptBlocks([]byte(body), gjson.Get(body, "system").Value(), "", template)
			require.Equal(t, body, string(out), "zero must not add even a placeholder or acknowledgement")
			normalized, _ := normalizeClaudeOAuthRequestBody(out, "claude-sonnet-4-6", claudeOAuthNormalizeOptions{
				preserveSystem: claudeOAuthSystemPromptPreservesCallerSystem(true, "claude-sonnet-4-6", template),
			})
			require.Equal(t, gjson.Get(body, "system").Raw, gjson.GetBytes(normalized, "system").Raw)
			require.Equal(t, gjson.Get(body, "messages").Raw, gjson.GetBytes(normalized, "messages").Raw)
		}
	}
	defaults, err := buildClaudeOAuthSystemPromptBlocksJSON([]byte(`{"messages":[]}`), "", "")
	require.NoError(t, err)
	require.Len(t, defaults, 3, "unconfigured settings keep the existing layout")
}

func TestClaudePromptTemplates_CallerBlocksAreLiteralAndKeepMarkers(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"You are OpenCode, the best coding agent on the planet.","cache_control":{"type":"ephemeral","ttl":"1h"}},{"type":"text","text":"literal {billing_header} and {caller_system}","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hello"}]}`)
	for _, template := range []string{localMinimalPromptTemplate, localFullPromptTemplate} {
		out := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, gjson.GetBytes(body, "system").Value(), "Local expansion", template)
		out, _ = normalizeClaudeOAuthRequestBody(out, "claude-sonnet-4-6", claudeOAuthNormalizeOptions{preserveSystem: true})
		blocks := gjson.GetBytes(out, "system").Array()
		caller := gjson.GetBytes(body, "system").Array()
		require.Equal(t, caller[0].Raw, blocks[len(blocks)-2].Raw)
		require.Equal(t, caller[1].Raw, blocks[len(blocks)-1].Raw)
		require.Equal(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(out, "messages").Raw)
		if template == localFullPromptTemplate {
			require.Equal(t, "Local expansion", blocks[2].Get("text").String())
		}
	}
}

func TestClaudePromptTemplates_DropEmptyDoesNotCreatePlaceholder(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	blocks, err := buildClaudeOAuthSystemPromptBlocksJSON(body, "", `[{"text":"  ","drop_if_empty":true,"cache_control":true},{"text":"{caller_system}","drop_if_empty":true}]`)
	require.NoError(t, err)
	require.Empty(t, blocks)
	for _, template := range []string{`[{"text":"{caller_system}","drop_if_empty":false}]`, `[{"text":" ","drop_if_empty":false}]`} {
		blocks, err = buildClaudeOAuthSystemPromptBlocksJSON(body, "", template)
		require.NoError(t, err)
		require.Len(t, blocks, 1)
		require.Equal(t, "\u200b", gjson.GetBytes(blocks[0], "text").String())
	}
}

func TestClaudePromptTemplates_Validation(t *testing.T) {
	for _, raw := range []string{
		`null`, `[null]`, `{}`, `{"blocks":null}`, `[] []`, `[{"type":"image"}]`,
		`[{"text":"x","cache_control":{}}]`,
		`[{"text":"x","cache_control":{"type":"persistent","ttl":"5m"}}]`,
		`[{"text":"x","cache_control":{"type":"ephemeral","ttl":"2h"}}]`,
		`[{"text":"x","cache_control":{"type":"ephemeral","ttl":null}}]`,
		`[{"text":"x","cache_control":{"type":"ephemeral","ttl":60}}]`,
		`[{"text":"prefix {caller_system}"}]`, `[{"text":"{caller_system}","cache_control":true}]`,
		`[{"text":"{caller_system}"},{"text":"{caller_system}"}]`,
		`[{"text":"a","cache_control":true},{"text":"b","cache_control":true},{"text":"c","cache_control":true},{"text":"d","cache_control":true},{"text":"e","cache_control":true}]`,
	} {
		t.Run(raw, func(t *testing.T) { require.Error(t, ValidateClaudeOAuthSystemPromptBlocksConfig(raw)) })
	}
	for _, raw := range []string{
		"", `[]`, `{"blocks":[]}`, localMinimalPromptTemplate, localFullPromptTemplate,
		`[{"text":"x","cache_control":{"type":"ephemeral"}}]`,
		`[{"text":"x","cache_control":{"type":"ephemeral","ttl":"1h","scope":"global"}}]`,
		`[{"text":"{caller_system}"},{"text":"{caller_system}","enabled":false}]`,
	} {
		require.NoError(t, ValidateClaudeOAuthSystemPromptBlocksConfig(raw), raw)
	}
}

func TestClaudePromptTemplates_FableKeepsIdentityContract(t *testing.T) {
	body := []byte(`{"model":"claude-fable-5","system":"Keep project instructions","messages":[{"role":"user","content":"hello"}]}`)
	for _, template := range []string{`[]`, localMinimalPromptTemplate, localFullPromptTemplate, `[{"text":"custom expansion"}]`} {
		out := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, "Keep project instructions", "", template)
		blocks := gjson.GetBytes(out, "system").Array()
		require.Len(t, blocks, 2)
		require.Contains(t, blocks[0].Get("text").String(), "x-anthropic-billing-header:")
		require.Equal(t, claudeCodeSystemPrompt, blocks[1].Get("text").String())
		require.Contains(t, gjson.GetBytes(out, "messages.0.content.0.text").String(), "Keep project instructions")
		require.False(t, claudeOAuthSystemPromptPreservesCallerSystem(true, "claude-fable-5", template))
	}
}

func TestClaudePromptTemplates_ClientIdentityPrefixDoesNotDiscardInstructions(t *testing.T) {
	text := claudeCodeSystemPrompt + "\n\nKeep the caller's project policy."
	body, err := json.Marshal(map[string]any{"system": text, "messages": []any{}})
	require.NoError(t, err)
	out := rewriteSystemForNonClaudeCode(body, text)
	require.Contains(t, gjson.GetBytes(out, "messages.0.content.0.text").String(), text)
}

func TestClaudePromptTemplates_CacheBudgetIncludesAutomaticMarker(t *testing.T) {
	body := []byte(`{"cache_control":{"type":"ephemeral","ttl":"1h"},"system":[{"type":"text","text":"a","cache_control":{"type":"ephemeral","ttl":"5m"}},{"type":"text","text":"b","cache_control":{"type":"ephemeral"}},{"type":"text","text":"c","cache_control":{"type":"ephemeral"}},{"type":"text","text":"d","cache_control":{"type":"ephemeral"}}],"messages":[]}`)
	out := enforceCacheControlLimit(body)
	_, messages, tools, system := collectCacheControlPaths(out)
	require.Len(t, system, 3)
	require.LessOrEqual(t, 1+len(messages)+len(tools)+len(system), maxCacheControlBlocks)
	for _, path := range system {
		require.Equal(t, "1h", gjson.GetBytes(out, path+".ttl").String())
	}
	require.Len(t, gjson.GetBytes(out, "system").Array(), 4, "budgeting must not delete instructions")
}

func TestClaudePromptTemplates_ForwardZeroPreservesTextAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGatewayForwardingSettingsCacheForTest(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := []byte(`{"model":"claude-sonnet-4-6","system":"You are OpenCode, the best coding agent on the planet.","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":12,"output_tokens":7}}`)),
	}}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream,
		rateLimitService: &RateLimitService{}, deferredService: &DeferredService{},
		settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
			SettingKeyEnableClaudeOAuthSystemPromptInjection: "true", SettingKeyClaudeOAuthSystemPromptBlocks: `[]`,
		}}, cfg),
	}
	account := &Account{ID: 302, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"}, Status: StatusActive, Schedulable: true}
	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.Equal(t, gjson.GetBytes(body, "system").Raw, gjson.GetBytes(upstream.lastBody, "system").Raw)
	require.Equal(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(upstream.lastBody, "messages").Raw)
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header:")
	require.EqualValues(t, 12, result.Usage.InputTokens)
	require.EqualValues(t, 7, result.Usage.OutputTokens)
}

func TestClaudePromptTemplatesJSONLVariablesAndOverlay(t *testing.T) {
	template := `{"id":"identity","note":"local layout","hide":true,"text":"{{identity}}"}
{"id":"env","text":"{{timezone}} {{locale}} {{session_id}} {{model}}"}
{"placement":"overlay_body","text":"Use authorized tools only."}
{"placement":"overlay_wrapper","text":"<policy>{{overlay_body}}</policy>"}`
	require.NoError(t, ValidateClaudeOAuthSystemPromptBlocksConfig(template))
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	out := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, nil, "", template, map[string]string{"timezone": "Asia/Shanghai", "locale": "zh-CN", "session_id": "wire-session"})
	require.Contains(t, gjson.GetBytes(out, "system.1.text").String(), "Asia/Shanghai zh-CN wire-session claude-sonnet-4-5")
	require.Equal(t, "hello", gjson.GetBytes(out, "messages.0.content.0.text").String())
	require.Equal(t, "<policy>Use authorized tools only.</policy>", gjson.GetBytes(out, "messages.0.content.1.text").String())
	require.False(t, gjson.GetBytes(out, "system.0.id").Exists())
	require.False(t, gjson.GetBytes(out, "system.0.hide").Exists())
	require.Error(t, ValidateClaudeOAuthSystemPromptBlocksConfig(`[{"placement":"overlay_wrapper","text":"missing slot"}]`))
	require.Error(t, ValidateClaudeOAuthSystemPromptBlocksConfig(`[{"placement":"overlay_body","text":"x","cache_control":true}]`))
}

func TestClaudePromptTemplatesLatestFullPresetAndUnknownVariables(t *testing.T) {
	config := string(claude.PromptTemplatePreset("official_full"))
	require.NoError(t, ValidateClaudeOAuthSystemPromptBlocksConfig(config))
	raw := []byte(`{"model":"claude-sonnet-5-5","system":"caller {literal_slot}","messages":[{"role":"user","content":"hello"}]}`)
	_, environment := claudePromptEnvironment(map[string]json.RawMessage{"platform": json.RawMessage(`"linux"`), "shell": json.RawMessage(`"bash"`), "linux_kernel": json.RawMessage(`"6.8.0"`)}, map[string]string{"model": "claude-sonnet-5-5", "timezone": "UTC", "locale": "en-US", "cwd": "/home/test"})
	blocks, err := buildClaudeOAuthSystemPromptBlocksJSON(raw, "custom expansion", config, map[string]string{"env_official": environment})
	require.NoError(t, err)
	require.Len(t, blocks, 6)
	require.Equal(t, "You are Claude Code, Anthropic's official CLI for Claude.", gjson.GetBytes(blocks[1], "text").String())
	require.Contains(t, gjson.GetBytes(blocks[2], "text").String(), "# Reporting outcomes")
	require.Greater(t, len(gjson.GetBytes(blocks[3], "text").String()), 10000)
	require.NotContains(t, gjson.GetBytes(blocks[3], "text").String(), "custom expansion")
	require.Equal(t, "1h", gjson.GetBytes(blocks[3], "cache_control.ttl").String())
	require.Equal(t, "1h", gjson.GetBytes(blocks[4], "cache_control.ttl").String())
	require.Contains(t, gjson.GetBytes(blocks[4], "text").String(), "Primary working directory: /home/test")
	require.Contains(t, gjson.GetBytes(blocks[4], "text").String(), "cutoff is June 2026")
	require.Equal(t, "caller {literal_slot}", gjson.GetBytes(blocks[5], "text").String())
	for _, block := range blocks {
		require.NotContains(t, string(block), "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__")
	}
	for _, bad := range []string{`[{"text":"{typo_variable}"}]`, `[{"text":"prefix {dynamic_boundary}"}]`, `[{"text":"{dynamic_boundary}","cache_control":true}]`} {
		require.Error(t, ValidateClaudeOAuthSystemPromptBlocksConfig(bad))
	}
	_, err = buildClaudeOAuthSystemPromptBlocksJSON(raw, "", `[{"text":"{typo_variable}"}]`)
	require.Error(t, err, "persisted invalid templates cannot forward literal internal placeholders")
}
