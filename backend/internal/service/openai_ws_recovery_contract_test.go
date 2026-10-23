package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSAccountRefreshRespectsCredentialOwnership(t *testing.T) {
	selected := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "old-token", "chatgpt_account_id": "owner"}}
	for _, tc := range []struct {
		name                            string
		change                          func(*Account)
		wantChanged, preserve, rejected bool
	}{
		{name: "unchanged"},
		{name: "token rotation", change: func(a *Account) { a.Credentials["access_token"] = "fresh-token" }, wantChanged: true, preserve: true},
		{name: "different owner", change: func(a *Account) { a.Credentials["chatgpt_account_id"] = "different-owner" }, wantChanged: true},
		{name: "different endpoint", change: func(a *Account) { a.Credentials["base_url"] = "https://different.example" }, wantChanged: true},
		{name: "disabled", change: func(a *Account) { a.Schedulable = false }, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			latest := *selected
			latest.Credentials = maps.Clone(selected.Credentials)
			if tc.change != nil {
				tc.change(&latest)
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: &codexAccountIdentityRepoStub{account: &latest}}
			got, err := svc.RefreshOpenAIWSAccount(context.Background(), nil, selected, nil)
			var changed *OpenAIWSAccountChangedError
			require.Equal(t, tc.wantChanged, errors.As(err, &changed))
			if tc.rejected {
				require.ErrorIs(t, err, ErrOpenAIWSLocalClientPolicy)
				return
			}
			require.Same(t, &latest, got)
			if !tc.wantChanged {
				require.NoError(t, err)
				return
			}
			require.Equal(t, tc.preserve, changed.PreserveContext)
			payload := []byte(`{"type":"response.create","model":"gpt-5.1","store":false,"previous_response_id":"resp_old"}`)
			input := []json.RawMessage{json.RawMessage(`{"type":"reasoning","summary":[],"encrypted_content":"private-cipher"}`)}
			recovery := svc.openAIWSAccountRefreshRetry(selected, changed, payload, input, true, "gpt-5.1")
			replay, ok := OpenAIWSCurrentTurnRetryPayload(recovery)
			require.Equal(t, tc.preserve, ok)
			if tc.preserve {
				require.Contains(t, string(replay), "private-cipher")
				require.NotContains(t, string(replay), "previous_response_id")
			} else {
				require.ErrorIs(t, recovery, ErrOpenAIWSReplayUnavailable)
			}
		})
	}
	require.Equal(t, "old-token", selected.GetOpenAIAccessToken())
}

func TestOpenAICodexTurnMetadataDoesNotStartOutput(t *testing.T) {
	for _, eventType := range []string{"codex.rate_limits", "codex.response.metadata", "response.interrupt.accepted", "response.interrupt.failed", "codex.unknown"} {
		t.Run(eventType, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{"type": eventType})
			require.NoError(t, err)
			wantOutput := eventType == "codex.unknown"
			require.Equal(t, wantOutput, openAIStreamDataStartsClientOutput(string(payload), eventType))
			require.Equal(t, wantOutput, openAIStreamDataStartsSemanticTTFT(string(payload), eventType))
			if !wantOutput {
				require.False(t, classifyOpenAITimingEvent(payload).semantic)
			}
			progress := beginOpenAIResponseProgress(nil, "ctx_pool", "websocket", nil)
			progress.observeDownstream(payload)
			require.Equal(t, wantOutput, progress.details()["semantic_output_seen"])
			require.True(t, progress.committed, "already delivered frames still commit the response identity")
		})
	}
}

func TestOpenAIRetryStopCapturesBoundedProgressWithoutContent(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	budget := NewOpenAITurnBudget(&config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: 3}})
	budget.attempts, budget.deadline = 2, time.Now().Add(time.Second)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), openAITurnBudgetKey{}, budget))
	progress := beginOpenAIResponseProgress(c, "http_bridge", "HTTP/2.0", nil)
	progress.observeUpstream("response.created")
	progress.observeDownstream([]byte(`{"type":"codex.rate_limits"}`))
	progress.observeDownstream([]byte(`{"type":"codex.response.metadata"}`))
	progress.observeDownstream([]byte(`{"type":"response.created","response":{"id":"private-response-id"}}`))
	require.False(t, progress.semanticSeen, "Codex metadata is not model output")
	progress.observeDownstream([]byte(`{"type":"response.output_text.delta","delta":"private-output"}`))
	setOpenAIReplaySafety(c, []byte(`{"tools":[{"type":"private-key-as-tool-type"}],"input":[{"type":"compaction","encrypted_content":"private-history"}]}`))
	RecordOpenAIRetryStop(c, &Account{ID: 10}, OpenAIRetryStopDownstreamCommitted)
	value, _ := c.Get(OpsUpstreamErrorsKey)
	events := value.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0].Detail), &detail))
	require.Equal(t, "http_bridge", detail["upstream_mode"])
	require.Equal(t, "HTTP/2.0", detail["upstream_protocol"])
	require.Equal(t, true, detail["downstream_committed"])
	require.Equal(t, true, detail["semantic_output_seen"])
	require.Equal(t, true, detail["visible_output_seen"])
	require.Equal(t, "token_event", detail["first_semantic_event"])
	require.Equal(t, float64(4), detail["downstream_events"])
	require.Equal(t, float64(3), detail["downstream_metadata_events"])
	require.Equal(t, float64(2), detail["attempts"])
	require.Equal(t, "unknown_tool", detail["blocking_tool_kind"])
	require.Equal(t, "encrypted_content", detail["context_blocker"])
	require.Equal(t, false, detail["context_portable"])
	require.NotContains(t, events[0].Detail, "private-")
	BeginOpsStreamTurn(c, 2)
	RecordOpenAIRetryStop(c, &Account{ID: 10}, OpenAIRetryStopHistoryIncomplete)
	value, _ = c.Get(OpsUpstreamErrorsKey)
	events = value.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.NotContains(t, events[0].Detail, "unknown_tool")
	require.NotContains(t, events[0].Detail, "context_blocker")
	require.NotContains(t, events[0].Detail, "http_bridge")
}

func TestOpenAIWSBridgeEncryptedRejectionRetainsUsageWithoutAccountPenalty(t *testing.T) {
	body := `data: {"type":"response.failed","response":{"id":"resp_rejected","status":"failed","error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"Cannot verify encrypted input"},"usage":{"input_tokens":8,"output_tokens":2}}}` + "\n\n"
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}}}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)
	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), nil, account, "test", payload, len(payload), "gpt-5.1", "", "", "", "", 1, func([]byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
	require.False(t, result.SchedulingOutcomeKnown())
}

func TestOpenAIProgressDistinguishesNonTokenOutputWithoutContent(t *testing.T) {
	for _, tc := range []struct {
		name, payload, event, item string
		visible                    bool
	}{
		{"tool progress", `{"type":"response.web_search_call.searching","query":"private-query"}`, "response.web_search_call.searching", "", false},
		{"encrypted reasoning", `{"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"private-cipher","summary":[]}}`, "response.output_item.done", "reasoning", false},
		{"whole message", `{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"private-text"}]}}`, "response.output_item.done", "message", true},
		{"unknown event", `{"type":"response.private-event","secret":"private-value"}`, "other_event", "", false},
		{"unknown item", `{"type":"response.output_item.done","item":{"type":"private-item"}}`, "response.output_item.done", "other_item", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := beginOpenAIResponseProgress(nil, "ctx_pool", "websocket", nil)
			p.observeDownstream([]byte(tc.payload))
			detail := p.details()
			require.Equal(t, 0, detail["downstream_token_events"])
			require.Equal(t, true, detail["semantic_output_seen"])
			require.Equal(t, tc.visible, detail["visible_output_seen"])
			require.Equal(t, tc.event, detail["first_semantic_event"])
			if tc.item == "" {
				require.NotContains(t, detail, "first_semantic_item_type")
			} else {
				require.Equal(t, tc.item, detail["first_semantic_item_type"])
			}
			require.NotContains(t, detail, "upstream_close_code")
			p.observeDownstream([]byte(`{"type":"response.output_text.delta","delta":"private-later"}`))
			require.Equal(t, tc.event, p.details()["first_semantic_event"], "retain the first semantic event")
			encoded, err := json.Marshal(p.details())
			require.NoError(t, err)
			require.Less(t, len(encoded), 1024)
			require.NotContains(t, string(encoded), "private-")
		})
	}
}
