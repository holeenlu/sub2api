package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestFinalizeClaudeCCHNativeLinuxCaptures(t *testing.T) {
	data, err := os.ReadFile("testdata/claude_code_2_1_301_linux_cch.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct{ Name, CCH, Body string }
	}
	require.NoError(t, json.Unmarshal(data, &fixtures))
	require.Len(t, fixtures.Cases, 6)
	for _, fixture := range fixtures.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			expected := []byte(fixture.Body)
			billing := gjson.GetBytes(expected, "system.0.text").String()
			for _, replacement := range []string{"cch=00000;", "cch=fffff;", ""} {
				changed := strings.Replace(billing, "cch="+fixture.CCH+";", replacement, 1)
				if replacement == "" {
					changed = strings.Replace(billing, " cch="+fixture.CCH+";", "", 1)
				}
				input, err := sjson.SetBytes(expected, "system.0.text", changed)
				require.NoError(t, err)
				original := bytes.Clone(input)
				got := finalizeClaudeCCH(input, "https://api.anthropic.com/v1/messages?beta=true")
				require.Equal(t, fixture.Body, string(got), "must reproduce exact captured bytes")
				require.Equal(t, original, input, "must not mutate shared input buffers")
				require.Equal(t, got, finalizeClaudeCCH(got, "https://api.anthropic.com/v1/messages?beta=true"))
			}
		})
	}
}

func TestFinalizeClaudeCCHScope(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.301.abc; cc_entrypoint=cli;"}],"messages":[{"role":"user","content":"cch=00000; cch=12345; 中文🙂 <>&"}]}`)
	for _, target := range []string{
		"https://relay.example/v1/messages", "https://api.anthropic.com.evil.example/v1/messages",
		"http://api.anthropic.com/v1/messages", "https://api.anthropic.com:8443/v1/messages",
		"https://api.anthropic.com/v1/models",
	} {
		require.Equal(t, body, finalizeClaudeCCH(body, target), target)
	}
	for _, input := range [][]byte{
		[]byte(`{"system":"x-anthropic-billing-header: cch=00000;"}`),
		[]byte(`{"messages":[{"role":"user","content":"x-anthropic-billing-header: cch=00000;"}]}`),
		bytes.Replace(body, []byte("2.1.301"), []byte("2.1.302"), 1),
	} {
		require.Equal(t, input, finalizeClaudeCCH(input, claudeAPIURL))
	}
	got := finalizeClaudeCCH(body, claudeAPIURL)
	require.NotEqual(t, body, got)
	require.Equal(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(got, "messages").Raw)
	require.Regexp(t, `; cch=[0-9a-f]{5};`, gjson.GetBytes(got, "system.0.text").String())
	require.NotContains(t, gjson.GetBytes(got, "system.0.text").String(), "cch=00000;")
	// Native lookup can hit a nonbilling literal first; keep that user text intact.
	withLiteral := bytes.Replace(body, []byte(`"system":[`),
		[]byte(`"system":[{"type":"text","text":"literal cch=00000;"},`), 1)
	signed := finalizeClaudeCCH(withLiteral, claudeAPIURL)
	require.Equal(t, "literal cch=00000;", gjson.GetBytes(signed, "system.0.text").String())
	require.Regexp(t, `; cch=[0-9a-f]{5};`, gjson.GetBytes(signed, "system.1.text").String())
	require.NotContains(t, gjson.GetBytes(signed, "system.1.text").String(), "cch=00000;")
}

func TestClaudeCCHByteExclusions(t *testing.T) {
	data, err := os.ReadFile("testdata/claude_code_2_1_301_cch_edges.json")
	require.NoError(t, err)
	var vectors []struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		HashInput string `json:"native_hash_input"`
		CCH       string `json:"native_cch"`
		Stamped   bool   `json:"native_stamped"`
	}
	require.NoError(t, json.Unmarshal(data, &vectors))
	require.Len(t, vectors, 88)
	hashed := 0
	for _, v := range vectors {
		if !v.Stamped {
			continue
		} // 13 native eligibility cases are retained as research evidence.
		hashed++
		t.Run(v.Name, func(t *testing.T) {
			require.True(t, json.Valid([]byte(v.Input)))
			input := claudeCCHHashInput([]byte(v.Input))
			require.Equal(t, v.HashInput, string(input))
			hash := xxhash.NewWithSeed(claudeCCHSeed)
			_, _ = hash.Write(input)
			require.Equal(t, v.CCH, fmt.Sprintf("%05x", hash.Sum64()&0xfffff))
		})
	}
	require.Equal(t, 75, hashed)
}

func FuzzClaudeCCHHashInput(f *testing.F) {
	for _, input := range []string{`"model":"max_tokens":00`, `{"model":"a","max_tokens":1}`, `"model":"fallbacks":[[]]`, ""} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			t.Skip()
		}
		original := []byte(input)
		_ = claudeCCHHashInput(original)
		require.Equal(t, input, string(original))
	})
}

func TestFinalizeClaudeCCHRemovesUnverifiedClientChecksum(t *testing.T) {
	input := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.302.abc; cc_entrypoint=cli; cch=abcde; cc_prompt_id=retained;"}],"messages":[{"role":"user","content":"literal cch=abcde;"}]}`)
	got := finalizeClaudeCCH(input, claudeAPIURL)
	require.Equal(t, "x-anthropic-billing-header: cc_version=2.1.302.abc; cc_entrypoint=cli; cc_prompt_id=retained;", gjson.GetBytes(got, "system.0.text").String())
	require.Equal(t, gjson.GetBytes(input, "messages").Raw, gjson.GetBytes(got, "messages").Raw)
	require.Equal(t, got, finalizeClaudeCCH(got, claudeAPIURL))
}

func TestOAuthFinalCCHUsesSanitizedBodyAndReplayBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "stream", "count_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			body := []byte(`{"model":"claude-haiku-4-5","max_tokens":64,"context_management":{"edits":[]},"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.301.abc; cc_entrypoint=cli; cch=fffff;"}],"messages":[{"role":"user","content":"hello"}]}`)
			svc := &GatewayService{cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
			var req *http.Request
			var wire []byte
			var err error
			if endpoint == "count_tokens" {
				req, wire, err = svc.buildCountTokensRequest(t.Context(), c, account, body, "synthetic", "oauth", "claude-haiku-4-5", false)
			} else {
				req, wire, err = svc.buildUpstreamRequest(t.Context(), c, account, body, "synthetic", "oauth", "claude-haiku-4-5", endpoint == "stream", false)
			}
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(wire, "context_management").Exists())
			require.NotContains(t, gjson.GetBytes(wire, "system.0.text").String(), "cch=fffff;")
			require.Equal(t, wire, finalizeClaudeCCH(wire, req.URL.String()))
			sent, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, req.Body.Close())
			require.Equal(t, wire, sent)
			replay, err := req.GetBody()
			require.NoError(t, err)
			repeated, err := io.ReadAll(replay)
			require.NoError(t, err)
			require.NoError(t, replay.Close())
			require.Equal(t, wire, repeated)
			require.EqualValues(t, len(wire), req.ContentLength)
		})
	}
}

func TestSyncBillingHeaderVersion(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		userAgent string
		wantSub   string // substring expected in result
		unchanged bool   // expect body to remain the same
	}{
		{
			name:      "replaces cc_version and recomputes message-derived suffix",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81.df2; cc_entrypoint=cli; cch=00000;"},{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22 (external, cli)",
			wantSub:   "cc_version=2.1.22." + computeClaudeCodeFingerprint([]byte(`{"messages":[]}`), "2.1.22"),
		},
		{
			name:      "no billing header in system",
			body:      `{"system":[{"type":"text","text":"You are Claude Code."}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "no system field",
			body:      `{"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "user-agent without version",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "Mozilla/5.0",
			unchanged: true,
		},
		{
			name:      "empty user-agent",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "",
			unchanged: true,
		},
		{
			name:      "version already matches",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.22; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "matching version preserves fingerprint from before CLI message merging",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.301.a3d; cc_entrypoint=sdk-cli;"}],"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>Environment</system-reminder>"},{"type":"text","text":"Reply OK."}]}]}`,
			userAgent: "claude-cli/2.1.301 (external, sdk-cli)",
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncBillingHeaderVersion([]byte(tt.body), tt.userAgent)
			if tt.unchanged {
				assert.Equal(t, tt.body, string(result), "body should remain unchanged")
			} else {
				assert.Contains(t, string(result), tt.wantSub)
				// Ensure old semver is gone
				assert.NotContains(t, string(result), "cc_version=2.1.81")
			}
		})
	}
}

func TestClaudeCodeFingerprintUsesJavaScriptStringIndexing(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"hello world", "720"},
		{"中文测试指纹字段应该按字符取样而不是字节", "5e2"},
		{"abcd🙂Z🧪01234567890123456789", "f9b"},
		{"abcd🙂", "72a"}, // a sampled unpaired surrogate hashes as U+FFFD
		{"", "eca"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			body, err := sjson.SetBytes([]byte(`{"messages":[{"role":"user","content":""}]}`), "messages.0.content", tc.text)
			require.NoError(t, err)
			require.Equal(t, tc.want, computeClaudeCodeFingerprint(body, "2.1.301"))
		})
	}
}

func TestSyncBillingHeaderVersion_RecomputesSuffixAndIsIdempotent(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81.df2; cc_entrypoint=cli;"}],"messages":[{"role":"user","content":"hello world"}]}`)
	version := "2.1.22"
	result := syncBillingHeaderVersion(body, "claude-cli/"+version)
	require.Contains(t, gjson.GetBytes(result, "system.0.text").String(),
		"cc_version="+version+"."+computeClaudeCodeFingerprint(body, version)+";")
	require.Equal(t, string(result), string(syncBillingHeaderVersion(result, "claude-cli/"+version)))
	require.JSONEq(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(result, "messages").Raw)
}

func TestBuildOAuthRequest_BillingMatchesWireUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "count_tokens"} {
		for _, tc := range []struct {
			name      string
			mimic     bool
			identity  bool
			disableFP bool
		}{
			{name: "mimic_uses_cached_version", mimic: true, identity: true},
			{name: "mimic_without_identity", mimic: true},
			{name: "mimic_with_fingerprint_disabled", mimic: true, identity: true, disableFP: true},
			{name: "passthrough_uses_cached_version", identity: true},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				resetGatewayForwardingSettingsCacheForTest(t)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":""}],"messages":[{"role":"user","content":"hello world"}]}`)
				billing, err := buildBillingAttributionText(body, "2.1.81")
				require.NoError(t, err)
				body, err = sjson.SetBytes(body, "system.0.text", billing)
				require.NoError(t, err)

				cfg := &config.Config{}
				svc := &GatewayService{cfg: cfg}
				cachedUA := "claude-cli/2.9.0 (external, cli)"
				if tc.identity {
					svc.identityService = NewIdentityService(&stubIdentityCache{fingerprint: &Fingerprint{
						UserAgent: cachedUA, ClientID: "test-client", UpdatedAt: time.Now().Unix(),
					}})
				}
				if tc.disableFP {
					svc.settingService = NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
						SettingKeyEnableFingerprintUnification: "false",
					}}, cfg)
				}
				account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
				var req *http.Request
				var wireBody []byte
				if endpoint == "messages" {
					req, wireBody, err = svc.buildUpstreamRequest(context.Background(), c, account,
						body, "test-token", "oauth", "claude-haiku-4-5", false, tc.mimic)
				} else {
					req, wireBody, err = svc.buildCountTokensRequest(context.Background(), c, account,
						body, "test-token", "oauth", "claude-haiku-4-5", tc.mimic)
				}
				require.NoError(t, err)
				defer func() { require.NoError(t, req.Body.Close()) }()
				wantUA := cachedUA
				if tc.mimic {
					wantUA = claudeCodeMimicUserAgent()
				}
				require.Equal(t, wantUA, getHeaderRaw(req.Header, "User-Agent"))
				version := ExtractCLIVersion(wantUA)
				require.Contains(t, gjson.GetBytes(wireBody, "system.0.text").String(),
					"cc_version="+version+"."+computeClaudeCodeFingerprint(wireBody, version)+";")
				actualBody, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, wireBody, actualBody)
			})
		}
	}
}
