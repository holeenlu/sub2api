//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise admission after a real BPS failure, including reconnects and expiry.
// Error-body logging is intentionally disabled: risk control must not depend on it.
func TestExcelBPSCyberSessionBlockAcrossClientProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"http", "error", "response.failed", "disabled", "allowlisted", "noncyber"} {
		for _, protocol := range []string{"json", "sse", "ws", "chat", "chat-json", "messages", "messages-json"} {
			event := scenario
			if scenario == "disabled" || scenario == "allowlisted" || scenario == "noncyber" {
				event = "response.failed"
			}
			t.Run(scenario+"/"+protocol, func(t *testing.T) {
				redisServer := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				var upstream *excelBPSFailoverUpstream
				upstream = &excelBPSFailoverUpstream{answer: func(call int) *http.Response {
					if call != 0 {
						return excelBPSCompleted()
					}
					detail := map[string]any{"code": "cyber_policy", "type": "invalid_request", "message": fmt.Sprintf("policy rejected token-%d file-secret", upstream.calls()[call])}
					if scenario == "noncyber" {
						detail["code"], detail["message"] = "content_policy_violation", "user text mentioned cyber_policy"
					}
					payload := map[string]any{"error": detail}
					status, contentType := http.StatusBadRequest, "application/json"
					if event != "http" {
						status, contentType = http.StatusOK, "text/event-stream"
						payload["type"] = event
						if event == "response.failed" {
							delete(payload, "error")
							payload["response"] = map[string]any{"id": "resp_cyber", "status": "failed", "error": detail, "output": []any{}, "usage": map[string]any{"input_tokens": 7, "output_tokens": 2}}
						}
					}
					raw, err := json.Marshal(payload)
					require.NoError(t, err)
					wire := string(raw)
					if event != "http" {
						wire = "data: " + wire + "\n\n"
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(wire))}
				}}
				usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 16)}
				settings := map[string]string{service.SettingKeyCyberSessionBlockEnabled: "true", service.SettingKeyCyberSessionBlockTTLSeconds: "60"}
				if scenario == "disabled" {
					settings[service.SettingKeyCyberSessionBlockEnabled] = "false"
				}
				if scenario == "allowlisted" {
					settings[service.SettingKeyCyberPolicyUserAllowlist] = "100"
				}
				h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{
					cache: repository.NewGatewayCache(rdb), usage: usage,
					settings: settings,
					apiKeys:  []*service.APIKey{newExcelBPSHandlerAPIKey(100, nativeHandlerFixtureGroupID)},
				})
				request := func(keyID int64, session, prompt string) string {
					t.Helper()
					stream := protocol != "json" && !strings.HasSuffix(protocol, "-json")
					c, rec := newExcelBPSFailoverTestContext(t.Context(), stream)
					c.Set(string(middleware2.ContextKeyAPIKey), newExcelBPSHandlerAPIKey(keyID, nativeHandlerFixtureGroupID))
					body := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": prompt}
					path := "/v1/responses"
					if strings.HasPrefix(protocol, "chat") || strings.HasPrefix(protocol, "messages") {
						delete(body, "input")
						body["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
						path = "/v1/chat/completions"
						if strings.HasPrefix(protocol, "messages") {
							body["max_tokens"], path = 100, "/v1/messages"
						}
					}
					if protocol == "ws" {
						body["type"] = "response.create"
					}
					raw, err := json.Marshal(body)
					require.NoError(t, err)
					if protocol != "ws" {
						c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
						c.Request.Header.Set("session_id", session)
						switch path {
						case "/v1/chat/completions":
							h.ChatCompletions(c)
						case "/v1/messages":
							h.Messages(c)
						default:
							h.Responses(c)
						}
						if mark := service.GetOpsCyberPolicy(c); mark != nil {
							require.NotContains(t, mark.Body, "token-1")
							require.NotContains(t, mark.Body, "token-2")
							require.NotContains(t, mark.Body, "file-secret")
						}
						return rec.Body.String()
					}
					done := make(chan struct{})
					router := gin.New()
					router.GET("/v1/responses", func(wsCtx *gin.Context) {
						defer close(done)
						wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
						wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
						h.ResponsesWebSocket(wsCtx)
					})
					server := httptest.NewServer(router)
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: http.Header{"Session_id": {session}}})
					require.NoError(t, err)
					defer client.CloseNow()
					require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
					var output strings.Builder
					followup := false
					for {
						_, data, readErr := client.Read(ctx)
						if readErr != nil {
							if followup {
								require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(readErr))
							}
							output.WriteString(readErr.Error())
							break
						}
						output.Write(data)
						kind := gjson.GetBytes(data, "type").String()
						if (scenario == "http" || scenario == "error" || scenario == "response.failed") && (kind == "response.failed" || kind == "error") {
							require.False(t, followup, "a cyber-blocked turn must not reach upstream again")
							followup = true
							require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
							continue
						}
						if kind == "response.completed" || kind == "response.failed" || kind == "error" {
							_ = client.Close(coderws.StatusNormalClosure, "done")
							break
						}
					}
					select {
					case <-done:
					case <-ctx.Done():
						t.Fatal("BPS websocket handler did not finish")
					}
					return output.String()
				}
				first := request(99, "session-cyber", "first conversation")
				require.NotContains(t, first, "token-1")
				require.NotContains(t, first, "token-2")
				require.Len(t, upstream.calls(), 1, "cyber failures must not switch accounts")
				select {
				case log := <-usage.created:
					if scenario != "noncyber" {
						require.Equal(t, service.RequestTypeCyberBlocked, log.RequestType)
					}
					if event == "response.failed" {
						require.Equal(t, 7, log.InputTokens)
						require.Equal(t, 2, log.OutputTokens)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("missing failure usage")
				}
				select {
				case log := <-usage.created:
					t.Fatalf("duplicate failure usage: %+v", log)
				case <-time.After(20 * time.Millisecond):
				}
				blocked := request(99, "session-cyber", "first conversation")
				if scenario == "disabled" || scenario == "allowlisted" || scenario == "noncyber" {
					require.Len(t, upstream.calls(), 2)
					require.Contains(t, blocked, "done")
					return
				}
				require.Len(t, upstream.calls(), 1, "the next request must be blocked before reaching BPS")
				require.Contains(t, blocked, "cyber")
				require.Contains(t, request(99, "", "first conversation"), "cyber")
				require.Len(t, upstream.calls(), 1, "omitting the session header must still match transcript history")
				require.Contains(t, request(99, "session-other", "another conversation"), "done")
				require.Len(t, upstream.calls(), 2, "another session on the same key must remain usable")
				require.Contains(t, request(100, "session-cyber", "first conversation"), "done")
				require.Len(t, upstream.calls(), 3, "session blocks must be isolated by API key")
				redisServer.FastForward(61 * time.Second)
				require.Contains(t, request(99, "session-cyber", "first conversation"), "done")
				require.Len(t, upstream.calls(), 4, "the configured TTL must expire the block")
			})
		}
	}
}
