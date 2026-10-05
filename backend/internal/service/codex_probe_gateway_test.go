package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexProbeBuildersHonorTemplateSnapshot(t *testing.T) {
	custom := strings.Replace(DefaultCodexProbeTemplate(), "anonymous workspace", "pinned workspace", 1)
	repo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAICodexTicketPromptTemplate: custom}}
	var harvestHeaders http.Header
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assertCodexProbeIdentity(t, body, req.Header)
		require.Contains(t, string(body), "pinned workspace")
		require.Equal(t, "harvest challenge", gjson.GetBytes(body, "input.4.content.0.text").String())
		harvestHeaders = req.Header.Clone()
		return codexTicketResponse(), nil
	}}
	svc, account, _ := challengeHarvestService(t, upstream)
	svc.settingService.settingRepo = repo
	pinned, err := svc.PrepareCodexProbeContext(context.Background())
	require.NoError(t, err)
	_, _, _, _, err = svc.fireOpenAICodexTicketProbe(pinned, account, "test-token", "gpt-6-astra", "", ModelTraceChallenge{Prompt: "harvest challenge"}, time.Second)
	require.NoError(t, err)
	repo.values[SettingKeyOpenAICodexTicketPromptTemplate] = strings.Replace(custom, "pinned workspace", "updated workspace", 1)
	svc.settingService.InvalidateCodexProbeTemplateCache()
	body, headers, err := svc.BuildCodexDiagnosticRequest(pinned, account.ID, "gpt-6-astra", ModelTraceChallenge{Prompt: "diagnostic challenge"})
	require.NoError(t, err)
	assertCodexProbeIdentity(t, body, headers)
	require.Contains(t, string(body), "pinned workspace")
	require.Equal(t, "diagnostic challenge", gjson.GetBytes(body, "input.4.content.0.text").String())
	require.NotEqual(t, harvestHeaders.Get("session-id"), headers.Get("session-id"))
	require.Equal(t, harvestHeaders.Get("x-codex-installation-id"), headers.Get("x-codex-installation-id"))
	body, _, err = svc.BuildCodexDiagnosticRequest(context.Background(), account.ID, "gpt-5.4", ModelTraceChallenge{Prompt: "next model challenge"})
	require.NoError(t, err)
	require.Contains(t, string(body), "updated workspace")
}

func TestCodexProbeGatewayAccountAndFingerprintRewritesStayConsistent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"off", "device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			account := newTestOAuthAccount(41, map[string]any{codexFingerprintModeExtraKey: mode})
			account.Credentials = map[string]any{"chatgpt_account_id": "probe-test-account"}
			svc := &OpenAIGatewayService{}
			body, headers, err := svc.buildCodexProbeRequest(context.Background(), account, "gpt-5.4", ModelTraceChallenge{Prompt: "probe challenge"})
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header = headers.Clone()
			c.Set("api_key", &APIKey{ID: 77})
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(body, &decoded))
			// Exercise the same identity stages and order used by Forward.
			require.True(t, applyCodexAccountIdentityClientMetadataMap(decoded, account, 77))
			ids := resolveCodexFingerprintIDsFromRequest(account, headers)
			applyCodexFingerprintClientMetadata(decoded, ids)
			stageCodexFingerprintIDs(c, ids)
			encoded, err := json.Marshal(decoded)
			require.NoError(t, err)
			request, err := svc.buildUpstreamRequest(context.Background(), c, account, encoded, "test-token", true, headers.Get("session-id"), true)
			require.NoError(t, err)
			cm := gjson.GetBytes(encoded, "client_metadata")
			for _, pair := range [][2]string{{"session-id", "session_id"}, {"thread-id", "thread_id"}, {"x-codex-installation-id", "x-codex-installation-id"}, {"x-codex-window-id", "x-codex-window-id"}} {
				if !openaiAllowedHeaders[pair[0]] && (mode == "off" || mode == "device") {
					// Normal gateway filtering still applies to optional session/thread headers.
					require.Empty(t, request.Header.Get(pair[0]), pair[0])
					continue
				}
				require.Equal(t, cm.Get(pair[1]).String(), request.Header.Get(pair[0]), pair[0])
			}
			require.Equal(t, cm.Get("session_id").String(), gjson.GetBytes(encoded, "prompt_cache_key").String())
			require.JSONEq(t, cm.Get("x-codex-turn-metadata").String(), request.Header.Get("x-codex-turn-metadata"))
			require.NotEqual(t, headers.Get("x-codex-installation-id"), request.Header.Get("x-codex-installation-id"))
			require.Equal(t, "probe challenge", gjson.GetBytes(encoded, "input.4.content.0.text").String())
		})
	}
}

func TestDiagnosticRecorderBoundsRetainedOutput(t *testing.T) {
	w := &diagnosticRecorder{ResponseRecorder: httptest.NewRecorder()}
	_, err := w.Write([]byte(strings.Repeat("1 ", 1<<20)))
	require.NoError(t, err)
	_, err = w.Write([]byte("extra"))
	require.NoError(t, err)
	require.True(t, w.overflow)
	require.Equal(t, 2<<20, w.Body.Len())
}
func TestDiagnosticFailedTerminalNeverBecomesNormal(t *testing.T) {
	_, complete := CodexDiagnosticOutput([]byte("data: {\"type\":\"response.completed\"}\n\ndata: {\"type\":\"response.failed\"}\n\n"))
	require.False(t, complete)
	_, complete = CodexDiagnosticOutput([]byte("data: [DONE]\n\n"))
	require.False(t, complete)
}

// The same probe is used by manual and scheduled execution. No old synchronous
// handler or separate parsing policy is needed to test these boundaries.
func TestCodexDiagnosticProbeGatewayContract(t *testing.T) {
	account := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "fixture"}}
	settingsRepo := &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAICodexTicketPromptTemplate: strings.Replace(DefaultCodexProbeTemplate(), "anonymous workspace", "custom workspace", 1)}}
	gateway := &OpenAIGatewayService{accountRepo: diagnosticAccounts{account: account}, settingService: NewSettingService(settingsRepo, nil)}
	for _, tc := range []struct {
		name          string
		status, count int
		reason        string
	}{
		{"completed enough", 200, 292, ""}, {"too few for challenge", 200, 332, "insufficient_numbers"}, {"gateway denial", 403, 292, "gateway_request_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.True(t, isCodexTicketDiagnostic(r.Context()))
				require.Equal(t, "/v1/responses", r.URL.Path)
				require.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
				deadline, ok := r.Context().Deadline()
				require.True(t, ok)
				require.InDelta(t, 120, time.Until(deadline).Seconds(), 2)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "custom workspace")
				require.Equal(t, "challenge", gjson.GetBytes(body, "input.4.content.0.text").String())
				w.WriteHeader(tc.status)
				if tc.status != 200 {
					_, _ = w.Write([]byte("{\"error\":{\"code\":\"POLICY_DENIED\",\"message\":\"private detail\"}}"))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed", "output_text": strings.Repeat("1 ", 170)})
			})
			item := RunCodexDiagnosticProbe(context.Background(), gateway, router, &APIKey{Key: "fixture-key"}, 41, "gpt-5.4", "127.0.0.1:0", "localhost", func() (ModelTraceChallenge, error) {
				return ModelTraceChallenge{Prompt: "challenge", ExpectedCount: tc.count}, nil
			})
			require.Equal(t, 1, calls)
			if tc.reason != "" {
				require.Equal(t, tc.reason, item.Reason)
			}
			if tc.status != 200 {
				require.Equal(t, "POLICY_DENIED", item.GatewayErrorCode)
				raw, _ := json.Marshal(item)
				require.NotContains(t, string(raw), "private detail")
			}
		})
	}
	settingsRepo.values[SettingKeyOpenAICodexTicketPromptTemplate] = "{private-secret"
	gateway.settingService.InvalidateCodexProbeTemplateCache()
	item := RunCodexDiagnosticProbe(context.Background(), gateway, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid template entered gateway") }), &APIKey{Key: "fixture"}, 41, "gpt-5.4", "", "", NewModelTraceChallenge)
	require.Equal(t, "template_invalid", item.Reason)
}
