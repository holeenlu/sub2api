package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cloudProtocolSwitchAccountRepo struct {
	AccountRepository
	latest atomic.Pointer[Account]
}

func (r *cloudProtocolSwitchAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.latest.Load(), nil
}

// Real client WebSocket frames enter the full ingress coordinator. The HTTP
// target and Cloud RPC peer are local protocol fixtures, not external services.
func TestCloudWebSocketProtocolConfigurationSwitchPreservesTwoCompletedTurns(t *testing.T) {
	for _, target := range []string{"native", "bps"} {
		t.Run("cloud_to_"+target, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			h.allow(15 * time.Second)
			source := h.in.Account
			source.Status, source.Schedulable, source.Concurrency = StatusActive, true, 10
			source.GroupIDs = []int64{*h.in.APIKey.GroupID}
			source.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
			source.Extra["openai_excel_bps"] = true
			source.Extra["openai_excel_bps_models"] = []string{"gpt-5.1"}
			h.svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			h.svc.settingService = excelBPSSettingsTestService()
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_target", "target answer"))}
			h.svc.httpUpstream = upstream
			repo := &cloudProtocolSwitchAccountRepo{}
			repo.latest.Store(source)
			h.svc.accountRepo = repo
			h.svc.schedulerSnapshot = nil
			targetAccount := *source
			targetAccount.Credentials = maps.Clone(source.Credentials)
			targetAccount.Extra = maps.Clone(source.Extra)
			targetAccount.Extra[OpenAICloudEnabledKey] = false
			bpsToken := ""
			if target == "bps" {
				// Import a separate Excel grant for the native account's exact
				// owner. Changing primary client_id is not dual authorization.
				oauth, _, _ := excelGrantService(t)
				grant := excelGrantFixture(t, source.GetChatGPTAccountID(), source.GetChatGPTUserID(), time.Now().Add(time.Hour), "protocol-switch")
				require.NoError(t, oauth.ImportExcelCredentials(h.ctx, &targetAccount, grant))
				bpsToken = grant["access_token"].(string)
				if h.svc.openAITokenProvider == nil {
					h.svc.openAITokenProvider = NewOpenAITokenProvider(repo, nil, oauth)
				} else {
					h.svc.openAITokenProvider.openAIOAuthService = oauth
				}
				require.Equal(t, source.Credentials, targetAccount.Credentials, "the native grant must remain unchanged")
				require.False(t, targetAccount.IsExcelOAuth(), "BPS uses its independent grant rather than replacing the native primary grant")
			} else {
				targetAccount.Extra["openai_excel_bps_models"] = []string{"a-different-upstream-model"}
			}
			require.Equal(t, target == "bps", targetAccount.IsExcelBPSEnabledForModel("client-model"))

			type turnOutcome struct {
				result *OpenAIForwardResult
				err    error
			}
			turns := make(chan turnOutcome, 3)
			replayed := make(chan []byte, 1)
			ended := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					ended <- err
					return
				}
				owner := NewOpenAIWSDownstream(conn, ResolveOpenAIWSClientReadLimitBytes(h.svc.cfg))
				defer owner.CloseNow()
				_, payload, err := owner.Read(h.ctx)
				if err != nil {
					ended <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				c.Set("api_key", h.in.APIKey)
				SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
				selected := source
				requestCtx := h.ctx
				hooks := &OpenAIWSIngressHooks{
					BeforeRequest: func(turn int, _ []byte, model string) error {
						if turn == 1 {
							return nil
						}
						latest, refreshErr := h.svc.RefreshOpenAIWSAccount(requestCtx, c, selected, h.in.APIKey.GroupID, model)
						var changed *OpenAIWSAccountChangedError
						if refreshErr == nil || errors.As(refreshErr, &changed) {
							selected = latest
						}
						return refreshErr
					},
					PrepareCloudTurn: func(ctx context.Context, _ int, _ []byte, _ string) (context.Context, error) {
						return h.svc.CaptureCloudBillingSnapshot(ctx, h.in)
					},
					AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- turnOutcome{result, err} },
				}
				// Mirror the existing handler's account-change retry: re-enter
				// with the same downstream owner and the proved full-input body.
				for attempt := 0; attempt < 2; attempt++ {
					err = h.svc.ProxyResponsesWebSocketFromClient(requestCtx, c, owner, selected, selected.GetOpenAIAccessToken(), payload, hooks)
					var changed *OpenAIWSAccountChangedError
					if !errors.As(err, &changed) {
						ended <- err
						return
					}
					if !changed.RequireFullInput {
						ended <- errors.New("protocol switch did not require full input")
						return
					}
					var ok bool
					payload, ok = OpenAIWSCurrentTurnRetryPayload(err)
					if !ok || len(payload) == 0 {
						ended <- errors.New("protocol switch lost its replay payload")
						return
					}
					replayed <- append([]byte(nil), payload...)
					requestCtx, err = h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, payload, c.Request.Header)
					if err != nil {
						ended <- err
						return
					}
					c.Request = c.Request.WithContext(requestCtx)
					c.Request.Header.Del(openAIWSTurnStateHeader)
				}
				ended <- errors.New("unexpected additional protocol restart")
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(h.ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			send := func(text, previous string) {
				body := map[string]any{"type": "response.create", "model": "client-model", "store": false, "input": []any{cloudUserMessage(text)}}
				if previous != "" {
					body["previous_response_id"] = previous
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				require.NoError(t, conn.Write(h.ctx, coderws.MessageText, raw))
			}
			completedCloud := make(chan struct{})
			go func() {
				defer close(completedCloud)
				wire := h.nextWire()
				if wire == nil {
					return
				}
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
				cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "one"}})
				cloudSwitchWSCompleteText(wire, "one", "first Cloud answer", 12)
				second := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "turn/start", second.Method)
				require.Contains(t, string(second.Params), "second user")
				require.NotContains(t, string(second.Params), "first user", "the normal Cloud turn still uses its live thread and delta")
				cloudTestRead(wire, map[string]any{"id": second.ID, "result": map[string]any{"turn": map[string]any{"id": "two"}}})
				cloudSwitchWSCompleteText(wire, "two", "second Cloud answer", 30)
			}()
			send("first user", "")
			firstEvents := readCloudProtocolSwitchWSTurn(t, conn, ended)
			first := gjson.GetBytes(firstEvents[len(firstEvents)-1], "response")
			require.Equal(t, "completed", first.Get("status").String())
			firstTurn := <-turns
			require.NoError(t, firstTurn.err)
			send("second user", first.Get("id").String())
			secondEvents := readCloudProtocolSwitchWSTurn(t, conn, ended)
			second := gjson.GetBytes(secondEvents[len(secondEvents)-1], "response")
			require.Equal(t, "completed", second.Get("status").String())
			secondTurn := <-turns
			require.NoError(t, secondTurn.err)
			<-completedCloud
			require.Equal(t, 2, h.store.settlementCount())
			for _, record := range h.store.all() {
				require.Equal(t, "completed", record.snapshot().Status)
			}

			// Only the account configuration changes. The client stays on this
			// socket and sends the usual previous ID plus one new user message.
			repo.latest.Store(&targetAccount)
			send("third user", second.Get("id").String())
			lastEvents := readCloudProtocolSwitchWSTurn(t, conn, ended)
			last := gjson.GetBytes(lastEvents[len(lastEvents)-1], "response")
			require.Equal(t, "completed", last.Get("status").String(), string(lastEvents[len(lastEvents)-1]))
			require.Contains(t, last.Raw, "target answer")
			lastTurn := <-turns
			require.NoError(t, lastTurn.err)
			require.Len(t, upstream.requests, 1)
			if target == "bps" {
				require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
				require.Equal(t, "Bearer "+bpsToken, upstream.lastReq.Header.Get("Authorization"), "the target must use the independent Excel grant")
				require.NotEqual(t, "Bearer "+source.GetOpenAIAccessToken(), upstream.lastReq.Header.Get("Authorization"))
				require.Equal(t, UsageAuthorizationModeBPS, lastTurn.result.AuthorizationMode)
			} else {
				require.NotEqual(t, "bps.openai.com", upstream.lastReq.URL.Host)
				require.Equal(t, nativeOpenAIUsageAuthorizationMode(&targetAccount), lastTurn.result.AuthorizationMode)
			}
			for _, text := range []string{"first user", "first Cloud answer", "second user", "second Cloud answer", "third user"} {
				require.Equal(t, 1, strings.Count(string(upstream.lastBody), text), "target %s lost or duplicated %q", target, text)
			}
			retry := <-replayed
			require.False(t, gjson.GetBytes(retry, "previous_response_id").Exists())
			require.Len(t, gjson.GetBytes(retry, "input").Array(), 5)
			require.Equal(t, 2, h.store.settlementCount(), "migration never settles the old Cloud turns again")
			require.Empty(t, h.wires, "the selected target cannot reconnect through Cloud")
			require.NotEqual(t, firstTurn.result.RequestID, secondTurn.result.RequestID)
			_ = conn.CloseNow()
			select {
			case err := <-ended:
				require.NoError(t, err)
			case <-h.ctx.Done():
				t.Fatal("the switched socket did not exit")
			}
		})
	}
}

// Preserve the ingress failure when the peer closes without a response event;
// otherwise a generic EOF hides routing and authorization fixture mistakes.
func readCloudProtocolSwitchWSTurn(t *testing.T, conn *coderws.Conn, ended <-chan error) []json.RawMessage {
	t.Helper()
	var events []json.RawMessage
	for {
		raw, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
		if err != nil {
			select {
			case serverErr := <-ended:
				t.Fatalf("client WebSocket read failed: %v; ingress returned: %v", err, serverErr)
			case <-time.After(time.Second):
				t.Fatalf("client WebSocket read failed: %v; ingress has not returned", err)
			}
		}
		require.True(t, json.Valid(raw))
		events = append(events, raw)
		kind := gjson.GetBytes(raw, "type").String()
		if isOpenAIWSTerminalEvent(kind) || kind == "error" {
			return events
		}
	}
}

func cloudSwitchWSCompleteText(wire *cloudRPCFakeConn, turn, text string, total int) {
	params := func(extra map[string]any) map[string]any {
		extra["threadId"], extra["turnId"] = "thread", turn
		return extra
	}
	usage := map[string]any{"inputTokens": total - 2, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": total}
	cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": params(map[string]any{"tokenUsage": map[string]any{"total": usage}})})
	cloudTestRead(wire, map[string]any{"method": "item/started", "params": params(map[string]any{"item": map[string]any{"id": "answer-" + turn, "type": "agentMessage"}})})
	cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": params(map[string]any{"itemId": "answer-" + turn, "delta": text})})
	cloudTestRead(wire, map[string]any{"method": "item/completed", "params": params(map[string]any{"item": map[string]any{"id": "answer-" + turn, "type": "agentMessage", "text": text}})})
	cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": turn, "status": "completed"}}})
}
