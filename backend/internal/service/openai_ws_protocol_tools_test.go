package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudWSProtocolMigrationRetainsConfirmedToolCatalog(t *testing.T) {
	const originalTools = `[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]`
	const changedTools = `[{"type":"custom","name":"exec"}]`
	for _, tc := range []struct {
		name, secondTools, wantTools string
		independent                  bool
	}{
		{name: "omitted twice", wantTools: originalTools},
		{name: "explicit empty clears", secondTools: `[]`, wantTools: `[]`},
		{name: "explicit replacement", secondTools: changedTools, wantTools: changedTools},
		{name: "independent request drops old catalog", independent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, _, _ := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ended := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					ended <- err
					return
				}
				defer conn.CloseNow()
				_, first, err := conn.Read(ctx)
				if err != nil {
					ended <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				c.Set("api_key", in.APIKey)
				hooks := &OpenAIWSIngressHooks{BeforeRequest: func(turn int, _ []byte, _ string) error {
					if turn == 3 {
						return &OpenAIWSAccountChangedError{PreserveContext: true, RequireFullInput: true}
					}
					return nil
				}}
				ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			send := func(previous, tools string, warmup bool) {
				body := map[string]any{"type": "response.create", "model": "client-model", "input": "next"}
				if previous != "" {
					body["previous_response_id"] = previous
				}
				if tools != "" {
					body["tools"] = json.RawMessage(tools)
				}
				if warmup {
					body["generate"] = false
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				require.NoError(t, conn.Write(ctx, coderws.MessageText, raw))
			}
			readCompleted := func() string {
				for {
					_, raw, err := conn.Read(ctx)
					require.NoError(t, err)
					if gjson.GetBytes(raw, "type").String() == "response.completed" {
						return gjson.GetBytes(raw, "response.id").String()
					}
				}
			}
			send("", originalTools, true)
			previous := readCompleted()
			if tc.independent {
				previous = ""
			}
			send(previous, tc.secondTools, true)
			send(readCompleted(), "", false)
			select {
			case err := <-ended:
				retry, ok := OpenAIWSCurrentTurnRetryPayload(err)
				require.True(t, ok, "%v", err)
				require.False(t, gjson.GetBytes(retry, "previous_response_id").Exists())
				tools := gjson.GetBytes(retry, "tools")
				if tc.wantTools == "" {
					require.False(t, tools.Exists(), "an independent request cannot inherit an earlier catalog")
				} else {
					require.JSONEq(t, tc.wantTools, tools.Raw)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

// Exercise the native upstream WS path rather than a local HTTP-bridge warmup:
// catalog inheritance is limited to confirmed pending-tool continuations.
func TestNativeWSProtocolMigrationRetainsConfirmedToolCatalog(t *testing.T) {
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
	upstream := newStagedPassthroughConn()
	defer upstream.Close()
	pool := newOpenAIWSConnPool(cfg)
	defer pool.Close()
	pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
	svc := newPassthroughLifecycleService(cfg, upstream)
	svc.openaiWSPool = pool
	account := nativeWSTestAccount()
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	originalTools := []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}
	user := cloudUserMessage("look up both entries")
	outputOne := map[string]any{"type": "function_call_output", "call_id": "call_one", "output": "first lookup result"}
	outputTwo := map[string]any{"type": "function_call_output", "call_id": "call_two", "output": "second lookup result"}
	frame := func(input any, previous string, includeTools bool) []byte {
		body := map[string]any{"type": "response.create", "model": "gpt-6.1-sol", "store": false, "input": []any{input}}
		body["client_metadata"] = map[string]any{"thread_id": "11111111-1111-4111-8111-111111111111", "session_id": "22222222-2222-4222-8222-222222222222", "x-codex-window-id": "11111111-1111-4111-8111-111111111111:0"}
		if previous != "" {
			body["previous_response_id"] = previous
		}
		if includeTools {
			body["tools"] = originalTools
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		return raw
	}
	hooks := &OpenAIWSIngressHooks{BeforeRequest: func(turn int, _ []byte, _ string) error {
		if turn == 3 {
			return &OpenAIWSAccountChangedError{PreserveContext: true, RequireFullInput: true}
		}
		return nil
	}}
	// A wire session is cache affinity. Explicit client thread/window metadata
	// in every frame proves one Codex context window.
	header := http.Header{"session_id": {"native-tool-migration-session"}}
	client, ended := startOpenAIWSMemorySession(t, svc, account, header, string(frame(user, "", true)), hooks)
	defer client.CloseNow()
	firstWire := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	wantTools, _ := json.Marshal(originalTools)
	require.Equal(t, "lookup", gjson.GetBytes(firstWire, "tools.0.name").String())
	completeTool := func(responseID, callID string) map[string]any {
		call := map[string]any{"type": "function_call", "id": "fc_" + callID, "call_id": callID, "name": "lookup", "arguments": "{}", "status": "completed"}
		body := map[string]any{"type": "response.completed", "response": map[string]any{
			"id": responseID, "status": "completed", "model": "gpt-6.1-sol", "output": []any{call},
			"usage": map[string]any{"input_tokens": 2, "output_tokens": 1},
		}}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		upstream.Send(string(raw))
		return call
	}
	callOne := completeTool("resp_native_one", "call_one")
	firstEvents := readNativeWSTestTurn(t, client)
	require.Equal(t, "resp_native_one", gjson.GetBytes(firstEvents[len(firstEvents)-1], "response.id").String())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	second := frame(outputOne, "resp_native_one", false)
	require.False(t, gjson.GetBytes(second, "tools").Exists())
	require.NoError(t, client.Write(ctx, coderws.MessageText, second))
	secondWire := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Equal(t, "resp_native_one", gjson.GetBytes(secondWire, "previous_response_id").String(), "native pending-tool continuation retains its actual anchor")
	require.Equal(t, "call_one", gjson.GetBytes(secondWire, "input.0.call_id").String())
	callTwo := completeTool("resp_native_two", "call_two")
	secondEvents := readNativeWSTestTurn(t, client)
	require.Equal(t, "resp_native_two", gjson.GetBytes(secondEvents[len(secondEvents)-1], "response.id").String())
	require.NoError(t, client.Write(ctx, coderws.MessageText, frame(outputTwo, "resp_native_two", false)))
	select {
	case err := <-ended:
		var changed *OpenAIWSAccountChangedError
		require.ErrorAs(t, err, &changed)
		require.True(t, changed.RequireFullInput)
		retry, ok := OpenAIWSCurrentTurnRetryPayload(err)
		require.True(t, ok, "%v", err)
		require.JSONEq(t, string(wantTools), gjson.GetBytes(retry, "tools").Raw, "the inherited catalog belongs to the continuing native tool chain")
		require.False(t, gjson.GetBytes(retry, "previous_response_id").Exists())
		wantHistory, _ := json.Marshal([]any{user, callOne, outputOne, callTwo, outputTwo})
		require.JSONEq(t, string(wantHistory), gjson.GetBytes(retry, "input").Raw)
		require.Len(t, gjson.GetBytes(retry, "input").Array(), 5)
		coverage := AnalyzeToolCallOutputContextCoverageBytes(retry)
		require.True(t, coverage.ContextCoversAllCallIDs)
		require.Empty(t, upstream.writes, "the third tool result must not execute on the old socket during migration")
		require.Empty(t, svc.httpUpstream.(*httpUpstreamRecorder).requests, "both confirmed turns used native upstream WS")
	case <-ctx.Done():
		t.Fatal("native WS did not return its protocol migration payload")
	}
}

func TestNativeWSReplayWithoutToolContinuationDoesNotRestoreCatalog(t *testing.T) {
	inherited, _ := json.Marshal([]any{map[string]any{"type": "function", "name": "old_lookup", "parameters": map[string]any{"type": "object"}}})
	body, _ := json.Marshal(map[string]any{"type": "response.create", "model": "gpt-6.1-sol", "previous_response_id": "resp_old", "input": []any{cloudUserMessage("new user turn")}})
	// The native caller grants inheritance only to pending function outputs;
	// a new user request omitting tools must clear the confirmed catalog.
	source, catalog, err := openAIWSReplaySourceWithTools(body, inherited, false)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(source, "tools").Exists())
	require.Empty(t, catalog)
	next, nextCatalog, err := openAIWSReplaySourceWithTools(source, catalog, true)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(next, "tools").Exists(), "later replay cannot resurrect a cleared native catalog")
	require.Empty(t, nextCatalog)
}
