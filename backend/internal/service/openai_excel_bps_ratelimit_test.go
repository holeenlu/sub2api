package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func excelBPSNativeFallbackTestAccount(t *testing.T, svc *OpenAIGatewayService) *Account {
	t.Helper()
	oauth, _, account := excelGrantService(t)
	account.Credentials["access_token"] = "native-codex-token"
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "bps-grant")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
	return account
}

func TestExcelBPS429NativeFallbackPreservesOpaqueHistory(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, rejection := range []string{"attachment", "http", "cooldown"} {
			t.Run(protocol+"/"+rejection, func(t *testing.T) {
				svc := excelBPSGatewayTestService(nil)
				enableNativeAttachments(svc)
				account := excelBPSNativeFallbackTestAccount(t, svc)
				if rejection == "cooldown" {
					svc.coolDownExcelBPS(t.Context(), account, "30")
				}
				var source map[string]any
				require.NoError(t, json.Unmarshal(excelBPSBoundAttachmentBody(t, false), &source))
				source["stream"] = protocol != "json"
				if rejection == "http" {
					input, ok := source["input"].([]any)
					require.True(t, ok)
					source["input"] = input[:1]
				}
				body, err := json.Marshal(source)
				require.NoError(t, err)
				uploads, nativeCalls := 0, 0
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
					require.Equal(t, account.ID, id)
					if req.URL.Path == "/basispoints/api/attachments" || req.URL.Path == "/basispoints/api/responses" {
						uploads++
						return bpsCompletionResponse(429, "{}"), nil
					}
					nativeCalls++
					require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
					require.Equal(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
					wire, readErr := io.ReadAll(req.Body)
					require.NoError(t, readErr)
					require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(wire, "input").Raw, "preserve original encrypted state and images")
					return bpsCompletionResponse(200, bpsCompletedStream("resp_native_bound", "native answer")), nil
				}}
				ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, "low")
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				if protocol == "ws" {
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native-codex-token", body, len(body), "gpt-6-astra", "", "", "", "", 1, func(event []byte) error {
						_, writeErr := rec.Write(append(event, '\n'))
						return writeErr
					})
				} else {
					_, err = svc.Forward(ctx, c, account, body)
				}
				require.NoError(t, err)
				wantUploads := 1
				if rejection == "cooldown" {
					wantUploads = 0
				}
				require.Equal(t, wantUploads, uploads, "prefer native fallback over another rejected upload")
				require.Equal(t, 1, nativeCalls)
				require.Contains(t, rec.Body.String(), "native answer")
				require.False(t, OpenAIResponsesAccountReplayAllowed(body), "same-owner recovery must not permit cross-account replay")
				require.False(t, c.GetBool(OpenAIAccountReplayAllowedContextKey))
				require.True(t, account.IsExcelBPSEnabled())
			})
		}
	}
}

func TestExcelBPS429NativeFallbackAcrossTransports(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		for _, protocol := range []string{"json", "sse", "ws", "chat", "chat-sse", "messages", "messages-sse"} {
			for _, rejection := range []string{"http", "stream", "cooldown"} {
				t.Run(fmt.Sprintf("%s/%s/passthrough=%t", protocol, rejection, passthrough), func(t *testing.T) {
					native := bpsCompletionResponse(200, "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"item_id\":\"msg_native\",\"delta\":\"native answer\"}\n\n"+bpsCompletedStream("resp_native", "native answer"))
					native.Header.Set("X-Request-Id", "native-request")
					native.Header.Set("Content-Type", "text/event-stream")
					upstream := &httpUpstreamRecorder{}
					if rejection == "http" {
						upstream.responses = append(upstream.responses, &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"rate_limit_exceeded","message":"PRIVATE_BPS"}}`))})
					} else if rejection == "stream" {
						upstream.responses = append(upstream.responses, bpsCompletionResponse(200, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_rejected\"}}\n\n"+
							"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_rejected\",\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"rate_limit_error\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}}\n\n"))
					}
					upstream.responses = append(upstream.responses, native)
					svc := excelBPSGatewayTestService(upstream)
					account := excelBPSNativeFallbackTestAccount(t, svc)
					account.Extra["openai_passthrough"] = passthrough
					if rejection == "cooldown" {
						svc.coolDownExcelBPS(t.Context(), account, "30")
					}
					stream := strings.Contains(protocol, "sse") || protocol == "ws"
					body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":"hello"}`, stream))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					budget := NewOpenAITurnBudget(svc.cfg)
					ctx := WithOpenAITurnBudget(t.Context(), budget)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
					var result *OpenAIForwardResult
					var err error
					switch {
					case protocol == "ws":
						result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native-codex-token", body, len(body), "gpt-6-astra", "", "", "", "", 1, func(event []byte) error {
							_, writeErr := rec.Write(append(event, '\n'))
							return writeErr
						})
					case strings.HasPrefix(protocol, "chat"), strings.HasPrefix(protocol, "messages"):
						body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`, stream))
						result, err = svc.forwardOpenAIGrantCompatibility(ctx, c, account, body, "", "gpt-6-astra", strings.HasPrefix(protocol, "messages"))
					default:
						result, err = svc.Forward(ctx, c, account, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					calls := 2
					if rejection == "cooldown" {
						calls = 1
					}
					require.Len(t, upstream.requests, calls)
					require.Equal(t, calls, budget.attempts)
					require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
					require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
					require.Equal(t, "Bearer native-codex-token", upstream.lastReq.Header.Get("Authorization"))
					require.Empty(t, upstream.lastReq.Header.Get("X-Basispoints-Auth-Mode"))
					require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Contains(t, rec.Body.String(), "native answer")
					if !stream {
						require.True(t, gjson.Valid(rec.Body.String()), "non-streaming clients need a JSON response")
						require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
					}
					require.NotContains(t, rec.Body.String(), "PRIVATE_BPS")
					require.NotContains(t, rec.Body.String(), "resp_rejected")
					require.Equal(t, "/v1/responses", result.UpstreamEndpoint)
					require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
					if rejection == "stream" {
						require.Len(t, result.PriorAuthorizationResults, 1)
						require.Equal(t, UsageAuthorizationModeBPS, result.PriorAuthorizationResults[0].AuthorizationMode)
						require.Equal(t, 7, result.PriorAuthorizationResults[0].Usage.InputTokens)
						require.Equal(t, 10, result.authorizationUsage.InputTokens)
					} else {
						require.Empty(t, result.PriorAuthorizationResults)
					}
					require.True(t, account.IsExcelBPSEnabled(), "fallback must not mutate the account snapshot")
					require.True(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
					require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"))
					wantInput, wantOutput := 10, 2
					if rejection == "stream" {
						wantInput, wantOutput = 17, 3
					}
					require.Equal(t, wantInput, result.Usage.InputTokens)
					require.Equal(t, wantOutput, result.Usage.OutputTokens)
				})
			}
		}
	}
}

func TestExcelBPSPreGenerationReplayChecksCompletePayload(t *testing.T) {
	state := `{"type":"compaction","encrypted_content":"opaque"}`
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"compaction", `{"input":[` + state + `]}`, true},
		{"reasoning", `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, true},
		{"file after state", `{"input":[` + state + `,{"type":"message","content":[{"type":"input_image","file_id":"file-owner"}]}]}`, false},
		{"server state", `{"input":[` + state + `],"previous_response_id":"resp_owner"}`, false},
		{"hosted tool after state", `{"input":[` + state + `],"tools":[{"type":"mcp","server_label":"example"}]}`, false},
		{"nested encrypted message", `{"input":[` + state + `,{"role":"user","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`, false},
		{"orphan result", `{"input":[` + state + `,{"type":"function_call_output","call_id":"missing","output":"result"}]}`, false},
		{"duplicate input", `{"input":[` + state + `],"input":[{"type":"item_reference","id":"owner"}]}`, false},
		{"duplicate input orphan result", `{"input":[` + state + `],"input":[{"type":"function_call_output","call_id":"missing","output":"result"}]}`, false},
		{"duplicate output call id", `{"input":[` + state + `,{"type":"function_call","call_id":"present","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"present","call_id":"missing","output":"result"}]}`, false},
		{"duplicate tool fields", `{"input":[` + state + `],"tools":[],"tools":[{"type":"mcp"}]}`, false},
		{"duplicate state type", `{"input":[{"type":"compaction","encrypted_content":"opaque","type":"message"}]}`, false},
		{"nested state ciphertext", `{"input":[{"type":"compaction","encrypted_content":"opaque","summary":{"encrypted_content":"hidden"}}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := newExcelBPSRateLimitedFailoverError("")
			err.RequestNotSent = true
			require.Equal(t, tc.allowed, OpenAIResponsesBPSPreGenerationReplayAllowed([]byte(tc.body), err))
			require.False(t, OpenAIResponsesAccountReplayAllowed([]byte(tc.body)), "do not relax native ambiguous-failure recovery")
			err.RequestNotSent = false
			require.False(t, OpenAIResponsesBPSPreGenerationReplayAllowed([]byte(tc.body), err), "inference/compaction already started")
		})
	}
}

type excelBPSBudgetScheduler struct {
	OpenAIAccountScheduler
	selectAccount func(context.Context, OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)
}

func (s excelBPSBudgetScheduler) Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectAccount(ctx, req)
}

func TestExcelBPSPoolRecoverySharesSelectionBudget(t *testing.T) {
	dependencyErr := fmt.Errorf("storage unavailable")
	for _, tc := range []struct {
		name       string
		nativeOps  int
		dependency bool
		wantErr    error
	}{
		{name: "fits", nativeOps: 1},
		{name: "exhausted", nativeOps: 25, wantErr: selectionbudget.ErrExhausted},
		{name: "dependency failure", dependency: true, wantErr: dependencyErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := NewOpenAITurnBudget(nil)
			turn.bpsRecovery.rejected = map[int64]bool{1: true}
			ctx := WithOpenAITurnBudget(t.Context(), turn)
			excluded := map[int64]struct{}{1: {}}
			var budgets []*selectionbudget.Budget
			var deadlines []time.Time
			svc := &OpenAIGatewayService{}
			svc.openaiScheduler = excelBPSBudgetScheduler{selectAccount: func(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				budgets = append(budgets, selectionbudget.From(ctx))
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				deadlines = append(deadlines, deadline)
				if tc.dependency {
					selectionbudget.RecordError(ctx, dependencyErr)
					return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
				}
				ops := tc.nativeOps
				if len(budgets) == 1 {
					ops = 40
				}
				for range ops {
					if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
						return nil, OpenAIAccountScheduleDecision{}, err
					}
				}
				if len(budgets) == 1 {
					if tc.dependency {
						selectionbudget.RecordError(ctx, dependencyErr)
					}
					return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
				}
				require.True(t, excelBPSNativeRecoveryActive(ctx))
				require.NotContains(t, req.ExcludedIDs, int64(1))
				return &AccountSelectionResult{Account: &Account{ID: 1}}, OpenAIAccountScheduleDecision{}, nil
			}}
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, selection)
			} else {
				require.NoError(t, err)
				require.NotNil(t, selection)
			}
			if tc.dependency {
				require.Len(t, budgets, 3, "two bounded dependency retries must not trigger native recovery")
				require.False(t, excelBPSNativeRecoveryActive(ctx))
			} else {
				require.Len(t, budgets, 2)
				require.Same(t, budgets[0], budgets[1], "one public selection cannot reset its storage budget during fallback")
				require.Equal(t, deadlines[0], deadlines[1], "fallback cannot renew the selection deadline")
				require.Equal(t, min(selectionbudget.Limit, 40+tc.nativeOps), budgets[0].Snapshot().Operations)
			}
		})
	}
}

func TestExcelBPS429PoolReturnsToDualGrantOnlyInNativePhase(t *testing.T) {
	for _, tc := range []struct{ loadBatch, veto bool }{{false, false}, {true, false}, {true, true}} {
		t.Run(fmt.Sprintf("loadBatch=%t/veto=%t", tc.loadBatch, tc.veto), func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			dual := excelBPSNativeFallbackTestAccount(t, svc)
			dual.Status, dual.Schedulable, dual.Concurrency = StatusActive, true, 1
			other := excelAccount()
			other.ID, other.Status, other.Schedulable, other.Concurrency = dual.ID+1, StatusActive, true, 1
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*dual, *other}}
			svc.cfg.Gateway.Scheduling.LoadBatchEnabled = tc.loadBatch
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			groupID := int64(123)
			excluded := make(map[int64]struct{})
			body := excelBPSBoundAttachmentBody(t, false)
			var uploadAccounts []int64
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				if req.URL.Path == "/basispoints/api/attachments" {
					uploadAccounts = append(uploadAccounts, id)
					return bpsCompletionResponse(429, "{}"), nil
				}
				require.Equal(t, dual.ID, id)
				require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
				require.Equal(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
				return bpsCompletionResponse(200, bpsCompletedStream("resp_native", "done")), nil
			}}
			for attempt := 0; attempt < 3; attempt++ {
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
				require.NoError(t, err)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				if attempt == 2 && tc.veto {
					excluded[selection.Account.ID] = struct{}{}
					continue // a slot/profit veto happens before the native forward
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
				_, err = svc.Forward(ctx, c, selection.Account, body)
				if attempt < 2 {
					requireExcelBPSRateLimitFailover(t, err, c)
				} else {
					require.NoError(t, err)
					require.Equal(t, dual.ID, selection.Account.ID, "restore only the independent native route")
				}
				excluded[selection.Account.ID] = struct{}{}
			}
			require.ElementsMatch(t, []int64{dual.ID, other.ID}, uploadAccounts)
			_, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "never resurrect the same failed native route")
			require.True(t, dual.IsExcelBPSEnabled())
		})
	}
}

type excelBPSQuotaWrite struct {
	accountID int64
	updates   map[string]any
	resetAt   time.Time
	ctxErr    error
	deadline  time.Time
}

type excelBPSQuotaRepo struct {
	AccountRepository
	writes    chan excelBPSQuotaWrite
	updateErr error
	limitErr  error
}

type excelBPSQuotaSettingsRepo struct {
	SettingRepository
	cooldown string
}

func (r *excelBPSQuotaSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyRateLimit429CooldownSettings {
		return r.cooldown, nil
	}
	return "", ErrSettingNotFound
}

func (r *excelBPSQuotaRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	copied := make(map[string]any, len(updates))
	for key, value := range updates {
		copied[key] = value
	}
	deadline, _ := ctx.Deadline()
	r.writes <- excelBPSQuotaWrite{accountID: id, updates: copied, ctxErr: ctx.Err(), deadline: deadline}
	return r.updateErr
}

func (r *excelBPSQuotaRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	deadline, _ := ctx.Deadline()
	r.writes <- excelBPSQuotaWrite{accountID: id, resetAt: resetAt, ctxErr: ctx.Err(), deadline: deadline}
	return r.limitErr
}

func nextExcelBPSQuotaWrite(t *testing.T, repo *excelBPSQuotaRepo) excelBPSQuotaWrite {
	t.Helper()
	select {
	case write := <-repo.writes:
		return write
	case <-time.After(3 * time.Second):
		t.Fatal("expected an account quota update")
		return excelBPSQuotaWrite{}
	}
}

func requireNoExcelBPSQuotaWrite(t *testing.T, repo *excelBPSQuotaRepo) {
	t.Helper()
	select {
	case write := <-repo.writes:
		t.Fatalf("unexpected account quota update: %+v", write)
	case <-time.After(20 * time.Millisecond):
	}
}

func excelBPSQuotaHeaders(used5h, used7d string) http.Header {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", used7d)
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", used5h)
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")
	return headers
}

func TestExcelBPSQuotaSnapshotAtResponseBoundary(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, brokenStream := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/broken=%t", stream, brokenStream), func(t *testing.T) {
				wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_bps_quota\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
				if brokenStream {
					wire = ""
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: excelBPSQuotaHeaders("63", "28"), Body: io.NopCloser(strings.NewReader(wire)),
				}}
				svc := excelBPSGatewayTestService(upstream)
				svc.codexSnapshotThrottle = newAccountWriteThrottle(time.Hour)
				repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
				svc.accountRepo = repo
				account := excelAccount()
				body := []byte(fmt.Sprintf("{\"model\":\"gpt-6-astra\",\"input\":\"test\",\"stream\":%t}", stream))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, err := svc.Forward(context.Background(), c, account, body)
				if brokenStream {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				write := nextExcelBPSQuotaWrite(t, repo)
				require.Equal(t, account.ID, write.accountID)
				require.Equal(t, 63.0, write.updates["codex_5h_used_percent"])
				require.Equal(t, 28.0, write.updates["codex_7d_used_percent"])
				updatedAtValue, ok := write.updates["codex_usage_updated_at"].(string)
				require.True(t, ok)
				updatedAt, err := time.Parse(time.RFC3339, updatedAtValue)
				require.NoError(t, err)
				require.WithinDuration(t, time.Now(), updatedAt, 5*time.Second)
				require.NoError(t, write.ctxErr)
				require.True(t, write.resetAt.IsZero())
				require.NotContains(t, account.Extra, "codex_usage_updated_at", "do not mutate a shared account snapshot")
				// A second response still succeeds but uses the existing snapshot throttle.
				upstream.resp.Body = io.NopCloser(strings.NewReader(wire))
				c, _ = gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, _ = svc.Forward(context.Background(), c, account, body)
				requireNoExcelBPSQuotaWrite(t, repo)
			})
		}
	}
}

func TestExcelBPSQuotaSnapshotRequiresHeaders(t *testing.T) {
	for _, headers := range []http.Header{nil, {"X-Codex-Primary-Used-Percent": {"invalid"}}} {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(""))}}
		svc := excelBPSGatewayTestService(upstream)
		svc.codexSnapshotThrottle = newAccountWriteThrottle(time.Hour)
		repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
		svc.accountRepo = repo
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, _ = svc.Forward(context.Background(), c, excelAccount(), []byte("{\"model\":\"gpt-6-astra\",\"input\":\"test\"}"))
		requireNoExcelBPSQuotaWrite(t, repo)
	}
}

type excelBPSCancelReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r *excelBPSCancelReader) Read(p []byte) (int, error) {
	r.cancel()
	return r.Reader.Read(p)
}

// requireExcelBPSRateLimitFailover checks that a BPS 429 left the response to
// the handler's account switch and kept upstream content out of the error.
func requireExcelBPSRateLimitFailover(t *testing.T, err error, c *gin.Context) {
	t.Helper()
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
	require.Equal(t, ExcelBPSRateLimitedReason, failover.Reason)
	require.Equal(t, http.StatusTooManyRequests, failover.ClientStatusCode)
	require.True(t, failover.ShouldRetryNextAccount())
	require.False(t, failover.RetryableOnSameAccount, "do not retry the throttled account in this request")
	require.False(t, failover.ShouldReportAccountScheduleFailure(), "keep the shared scheduler score")
	require.Empty(t, failover.ResponseBody)
	for key := range failover.ResponseHeaders {
		require.Equal(t, "Retry-After", key)
	}
	require.False(t, c.Writer.Written(), "leave the response to the handler")
	require.False(t, IsResponseCommitted(c))
}

func excelBPSCooldownRemaining(t *testing.T, svc *OpenAIGatewayService, accountID int64) time.Duration {
	t.Helper()
	value, ok := svc.excelBPSCooldownUntil.Load(accountID)
	require.True(t, ok, "expected a BPS cooldown")
	until, ok := value.(time.Time)
	require.True(t, ok)
	return time.Until(until)
}

func TestExcelBPS429FailsOverWithoutChangingCodexState(t *testing.T) {
	retryAt := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	for _, tc := range []struct {
		name           string
		headers        http.Header
		raw            string
		cancelOnRead   bool
		wantRetryAfter string
		wantCooldown   time.Duration
	}{
		{name: "quota headers", headers: excelBPSQuotaHeaders("100", "100"), wantCooldown: 11 * time.Second},
		{name: "unexhausted headers", headers: excelBPSQuotaHeaders("30", "20"), wantCooldown: 11 * time.Second},
		{name: "body reset timestamp", raw: fmt.Sprintf(`{"error":{"type":"usage_limit_reached","resets_at":%d}}`, time.Now().Add(2*time.Hour).Unix()), wantCooldown: 11 * time.Second},
		{name: "body reset duration", raw: `{"error":{"type":"usage_limit_reached","resets_in_seconds":7200}}`, wantCooldown: 11 * time.Second},
		{name: "generic rate limit", wantCooldown: 11 * time.Second},
		{name: "malformed body", raw: "not json", wantCooldown: 11 * time.Second},
		{name: "retry after seconds", headers: http.Header{"Retry-After": {"30"}}, wantRetryAfter: "30", wantCooldown: 30 * time.Second},
		{name: "retry after date", headers: http.Header{"Retry-After": {retryAt}}, wantRetryAfter: retryAt, wantCooldown: time.Minute},
		{name: "retry after beyond cap", headers: http.Header{"Retry-After": {"86400"}}, wantRetryAfter: "86400", wantCooldown: 2 * time.Hour},
		{name: "invalid retry after", headers: http.Header{"Retry-After": {"soon"}}, wantCooldown: 11 * time.Second},
		{name: "client cancellation", headers: excelBPSQuotaHeaders("100", "100"), cancelOnRead: true, wantCooldown: 11 * time.Second},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				raw := tc.raw
				if raw == "" {
					raw = `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"PRIVATE_UPSTREAM"}}`
				}
				var reader io.Reader = strings.NewReader(raw)
				if tc.cancelOnRead {
					reader = &excelBPSCancelReader{Reader: reader, cancel: cancel}
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: tc.headers, Body: io.NopCloser(reader)}}
				svc := excelBPSGatewayTestService(upstream)
				repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
				svc.accountRepo = repo
				svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
				svc.rateLimitService.SetSettingService(NewSettingService(&excelBPSQuotaSettingsRepo{cooldown: `{"enabled":true,"cooldown_seconds":11}`}, svc.cfg))
				svc.rateLimitService.SetAccountRuntimeBlocker(svc)
				account := excelAccount()
				account.Extra["openai_excel_bps_auto_disable_on_403"] = true
				retryStarted := time.Now()
				svc.openaiOAuth429RetryStartedAt.Store(account.ID, retryStarted)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%t}`, stream))

				_, err := svc.Forward(ctx, c, account, body)

				if tc.cancelOnRead {
					require.ErrorIs(t, err, context.Canceled, "a gone client must not start a failover")
					var failover *UpstreamFailoverError
					require.NotErrorAs(t, err, &failover)
					marked, ok := GetOpsStreamError(c)
					require.True(t, ok)
					require.Equal(t, OpsClientCanceledCode, marked.Code)
					require.False(t, c.Writer.Written())
				} else {
					requireExcelBPSRateLimitFailover(t, err, c)
					var failover *UpstreamFailoverError
					require.ErrorAs(t, err, &failover)
					require.Equal(t, tc.wantRetryAfter, failover.ResponseHeaders.Get("Retry-After"))
				}
				require.Len(t, upstream.requests, 1)
				require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
				_, finalUpstreamError := c.Get(OpsUpstreamStatusCodeKey)
				require.False(t, finalUpstreamError, "a failover attempt is not the final upstream error")
				rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
				require.True(t, ok)
				require.Len(t, events, 1)
				require.Equal(t, "failover", events[0].Kind)
				require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)
				require.NotContains(t, events[0].Message+events[0].Detail, "PRIVATE_UPSTREAM")

				// The observed 429 cools only the BPS route, even when the client left.
				require.InDelta(t, tc.wantCooldown.Seconds(), excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 2)
				require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"))
				requireNoExcelBPSQuotaWrite(t, repo)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				require.True(t, account.IsSchedulable())
				require.True(t, account.IsExcelBPSEnabled())
				require.Nil(t, account.RateLimitResetAt)
				retryState, ok := svc.openaiOAuth429RetryStartedAt.Load(account.ID)
				require.True(t, ok)
				require.Equal(t, retryStarted, retryState)
			})
		}
	}
}

func TestExcelBPSNon429DoesNotChangeQuotaState(t *testing.T) {
	// Authentication failures have their own scheduling assertions.
	for _, status := range []int{400, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: excelBPSQuotaHeaders("100", "100"), Body: io.NopCloser(strings.NewReader("{}"))}}
			svc := excelBPSGatewayTestService(upstream)
			repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
			svc.accountRepo = repo
			svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
			svc.rateLimitService.SetAccountRuntimeBlocker(svc)
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, account, []byte("{\"model\":\"gpt-6-astra\",\"input\":\"test\"}"))
			require.Error(t, err)
			if status >= 500 {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, status, failover.StatusCode)
				require.False(t, c.Writer.Written())
			} else {
				require.Equal(t, status, rec.Code)
			}
			requireNoExcelBPSQuotaWrite(t, repo)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.True(t, account.IsSchedulable())
		})
	}
}

func TestExcelBPS429FallbackCooldownFollowsSetting(t *testing.T) {
	for _, tc := range []struct {
		name         string
		setting      string
		noRateLimit  bool
		retryAfter   string
		wantCooldown time.Duration
	}{
		{name: "configured seconds", setting: `{"enabled":true,"cooldown_seconds":11}`, wantCooldown: 11 * time.Second},
		{name: "disabled without retry after", setting: `{"enabled":false,"cooldown_seconds":11}`},
		{name: "retry after wins when disabled", setting: `{"enabled":false,"cooldown_seconds":11}`, retryAfter: "20", wantCooldown: 20 * time.Second},
		{name: "zero retry after keeps a minimum", setting: `{"enabled":true,"cooldown_seconds":11}`, retryAfter: "0", wantCooldown: time.Second},
		{name: "no rate limit service", noRateLimit: true, wantCooldown: defaultRateLimit429CooldownSeconds * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.retryAfter != "" {
				header.Set("Retry-After", tc.retryAfter)
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}}
			svc := excelBPSGatewayTestService(upstream)
			if !tc.noRateLimit {
				repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
				svc.accountRepo = repo
				svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
				svc.rateLimitService.SetSettingService(NewSettingService(&excelBPSQuotaSettingsRepo{cooldown: tc.setting}, svc.cfg))
			}
			account := excelAccount()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

			_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-6-astra","input":"test"}`))

			requireExcelBPSRateLimitFailover(t, err, c)
			if tc.wantCooldown == 0 {
				_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
				require.False(t, cooling, "a disabled fallback only relies on this request's exclusion")
				return
			}
			require.InDelta(t, tc.wantCooldown.Seconds(), excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 1)
		})
	}
}

func TestExcelBPSCooldownNeverShortensAndExpires(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	svc.coolDownExcelBPS(context.Background(), account, "60")
	svc.coolDownExcelBPS(context.Background(), account, "5")
	require.InDelta(t, 60, excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 1, "a shorter cooldown must not win")
	svc.coolDownExcelBPS(context.Background(), account, "120")
	require.InDelta(t, 120, excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 1)

	other := excelAccount()
	other.ID++
	var wg sync.WaitGroup
	for i := 1; i <= 64; i++ {
		wg.Add(1)
		go func(seconds int) {
			defer wg.Done()
			svc.coolDownExcelBPS(context.Background(), other, strconv.Itoa(seconds))
		}(i * 10)
	}
	wg.Wait()
	require.InDelta(t, 640, excelBPSCooldownRemaining(t, svc, other.ID).Seconds(), 1, "concurrent writes keep the longest cooldown")

	svc.excelBPSCooldownUntil.Store(account.ID, time.Now().Add(-time.Second))
	require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
	_, stale := svc.excelBPSCooldownUntil.Load(account.ID)
	require.False(t, stale, "expired cooldowns are dropped")
}

func TestExcelBPSCooldownOnlyAffectsBPSRoutedModels(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	account.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	svc.coolDownExcelBPS(context.Background(), account, "30")

	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"))
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.1"), "native Codex models stay schedulable")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "no account-wide block")

	account.Extra["openai_excel_bps"] = false
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"), "once BPS is off the model is native")
}

func TestExcelBPS429AfterOutputCoolsWithoutReplay(t *testing.T) {
	for _, route := range []string{"tool correction", "unknown tool regeneration"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", route, stream), func(t *testing.T) {
				summary, code := "Run", "text(42);"
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":"test","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`, stream))
				if route == "unknown tool regeneration" {
					summary, code = "Call client tool", `{"name":"unknown","arguments":{}}`
					body = []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":"test","tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}]}`, stream))
				}
				first := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, "throttled", summary, code))}
				rejected := &excelBPSRepairBody{Reader: strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM"}}`)}
				retryAfter := excelBPSQuotaHeaders("100", "100")
				retryAfter.Set("Retry-After", "45")
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: http.StatusOK, Header: http.Header{}, Body: first},
					{StatusCode: http.StatusTooManyRequests, Header: retryAfter, Body: rejected},
				}}
				svc := excelBPSGatewayTestService(upstream)
				repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
				svc.accountRepo = repo
				svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
				svc.rateLimitService.SetAccountRuntimeBlocker(svc)
				account := excelAccount()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

				_, err := svc.Forward(context.Background(), c, account, body)

				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "accepted output must not be replayed on another account")
				require.Len(t, upstream.requests, 2)
				require.True(t, rejected.closed.Load())
				require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
				require.InDelta(t, 45, excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 2)
				requireNoExcelBPSQuotaWrite(t, repo)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			})
		}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ExcelBPSCooldownSkipsOnlyBPSRoutes(t *testing.T) {
	for _, advanced := range []bool{true, false} {
		t.Run(fmt.Sprintf("advanced=%t", advanced), func(t *testing.T) {
			ctx := context.Background()
			groupID := int64(101301)
			newAccount := func(id int64) Account {
				return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "chatgpt", "client_id": openai.ExcelClientID},
					Extra:       map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-astra"}}}
			}
			accounts := []Account{newAccount(38301), newAccount(38302)}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &OpenAIGatewayService{
				settingService:     excelBPSSettingsTestService(),
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
				cache:              &schedulerTestGatewayCache{},
				cfg:                cfg,
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			require.NotNil(t, svc.getOpenAIAccountScheduler(ctx))
			selectModel := func(model string) (*AccountSelectionResult, error) {
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", model, nil, OpenAIUpstreamTransportAny, false)
				if selection != nil && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				return selection, err
			}

			svc.coolDownExcelBPS(ctx, &accounts[0], "60")
			for range 10 {
				selection, err := selectModel("gpt-6-astra")
				require.NoError(t, err)
				require.Equal(t, int64(38302), selection.Account.ID)
			}

			svc.coolDownExcelBPS(ctx, &accounts[1], "60")
			_, err := selectModel("gpt-6-astra")
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Contains(t, err.Error(), excelBPSRateLimitedFilterReason+"=2")
			for i := range accounts {
				delete(accounts[i].Credentials, "client_id")
			}
			selection, err := selectModel("gpt-6-astra")
			require.NoError(t, err, "BPS cooldown must leave the native Codex route schedulable")
			require.NotNil(t, selection.Account)
			selection, err = selectModel("gpt-5.1")
			require.NoError(t, err, "models these accounts forward natively stay schedulable")
			require.NotNil(t, selection.Account)
		})
	}
}

func TestExcelBPS429NativeFallbackPreservesBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"server response", `{"model":"gpt-6-astra","previous_response_id":"resp_other","input":"hello"}`},
		{"encrypted assignment", `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"encrypted_content","encrypted_content":"secret"}]}]}`},
		{"file ownership", `{"model":"gpt-6-astra","input":[{"role":"user","content":[{"type":"input_image","file_id":"file_other"}]}]}`},
		{"unmatched tool result", `{"model":"gpt-6-astra","input":[{"type":"function_call_output","call_id":"unknown","output":"done"}]}`},
		{"hosted execution", `{"model":"gpt-6-astra","input":"hello","tools":[{"type":"code_interpreter","container":{"type":"auto"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := excelBPSGatewayTestService(upstream)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			svc.coolDownExcelBPS(t.Context(), account, "30")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			_, err := svc.Forward(t.Context(), c, account, []byte(tc.body))
			requireExcelBPSRateLimitFailover(t, err, c)
			require.Empty(t, upstream.requests, "do not migrate credential-owned or unresolved history")
		})
	}
	for _, mode := range []string{"excel_only", "probe", "cancel", "attempt_budget", "deadline", "native_rejected", "visible_output"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var rejection io.Reader = strings.NewReader(`{"error":{"code":"rate_limit_exceeded"}}`)
			if mode == "cancel" {
				rejection = &excelBPSCancelReader{Reader: rejection, cancel: cancel}
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(rejection)},
				bpsCompletionResponse(400, `{"error":{"message":"native request rejected","type":"invalid_request_error"}}`),
			}}
			if mode == "visible_output" {
				upstream.responses = []*http.Response{bpsCompletionResponse(200, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"already visible\"}\n\ndata: {\"type\":\"error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"rate_limit_error\"}}\n\n")}
			}
			svc := excelBPSGatewayTestService(upstream)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			if mode == "excel_only" {
				account = excelAccount()
			}
			budget := NewOpenAITurnBudget(svc.cfg)
			if mode == "attempt_budget" {
				budget.limit = 1
			}
			if mode == "deadline" {
				budget.deadline = time.Now().Add(-time.Second)
			}
			ctx = WithOpenAITurnBudget(ctx, budget)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			if mode == "probe" {
				c.Set(bpsAccountProbeRequiredContextKey, true)
			}
			_, err := svc.Forward(ctx, c, account, []byte(`{"model":"gpt-6-astra","stream":true,"input":"hello"}`))
			require.Error(t, err)
			calls := 1
			if mode == "native_rejected" {
				calls = 2
			}
			if mode == "deadline" {
				calls = 0
			}
			require.Len(t, upstream.requests, calls)
			if mode == "visible_output" {
				require.Contains(t, rec.Body.String(), "already visible")
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover)
			}
			if mode == "attempt_budget" {
				require.ErrorIs(t, err, ErrOpenAITurnAttemptBudgetExhausted)
			}
		})
	}
}
