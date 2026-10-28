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
)

func excelBPSDualGrantTestAccount(t *testing.T, svc *OpenAIGatewayService) *Account {
	t.Helper()
	oauth, _, account := excelGrantService(t)
	account.Credentials["access_token"] = "native-codex-token"
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "bps-grant")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	account.Extra["openai_excel_bps_models"] = []string{"gpt-5.1", "gpt-5.4", "gpt-5.6-sol", "gpt-6-astra", "gpt-6-sol"}
	svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
	return account
}

func TestExcelBPS429NeverSendsNativeWithOpaqueHistory(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, rejection := range []string{"attachment", "http", "cooldown"} {
			t.Run(protocol+"/"+rejection, func(t *testing.T) {
				svc := excelBPSGatewayTestService(nil)
				enableNativeAttachments(svc)
				account := excelBPSDualGrantTestAccount(t, svc)
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
					return bpsCompletionResponse(200, bpsCompletedStream("resp_native_bound", "unexpected native answer")), nil
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
				requireExcelBPSRateLimitFailover(t, err, c)
				wantUploads := 1
				if rejection == "cooldown" {
					wantUploads = 0
				}
				require.Equal(t, wantUploads, uploads)
				require.Zero(t, nativeCalls, "even a valid native grant cannot replay the BPS operation")
				require.Empty(t, rec.Body.String())
				require.False(t, OpenAIResponsesAccountReplayAllowed(body), "same-owner recovery must not permit cross-account replay")
				require.False(t, c.GetBool(OpenAIAccountReplayAllowedContextKey))
				require.True(t, account.IsExcelBPSEnabled())
			})
		}
	}
}

func TestExcelBPS429NeverSendsNativeAcrossTransports(t *testing.T) {
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
					account := excelBPSDualGrantTestAccount(t, svc)
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
					requireExcelBPSRateLimitFailover(t, err, c)
					calls := 1
					if rejection == "cooldown" {
						calls = 0
					}
					require.Len(t, upstream.requests, calls)
					require.Equal(t, calls, budget.attemptBudget.Snapshot().Attempts, "failure cannot reserve another native attempt")
					for _, req := range upstream.requests {
						require.Equal(t, "bps.openai.com", req.URL.Host)
						require.Equal(t, "/basispoints/api/responses", req.URL.Path)
						require.NotEqual(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
					}
					require.Empty(t, rec.Body.String(), "the caller owns the final error or same-mode recovery")
					require.True(t, excelBPSExecutionPinned(ctx))
					require.True(t, account.IsExcelBPSEnabled(), "429 must not disable BPS")
					require.True(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
					require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"), "native credentials do not bypass a BPS cooldown")
					if rejection == "stream" {
						require.NotNil(t, result, "retain usage measured before the failed terminal event")
						require.Equal(t, UsageAuthorizationModeBPS, result.AuthorizationMode)
						require.Equal(t, 7, result.Usage.InputTokens)
						require.Equal(t, 1, result.Usage.OutputTokens)
						require.Empty(t, result.PriorAuthorizationResults, "no fabricated native result or duplicated BPS usage")
					} else {
						require.Nil(t, result, "no measured generation usage exists")
					}
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

func TestExcelBPSPinnedSelectionPreservesExclusionsAndBudget(t *testing.T) {
	dependencyErr := fmt.Errorf("storage unavailable")
	for _, tc := range []struct {
		name       string
		operations int
		dependency bool
		wantErr    error
		wantCalls  int
	}{
		{name: "pool exhausted", operations: 40, wantErr: ErrNoAvailableAccounts, wantCalls: 1},
		{name: "storage budget exhausted", operations: selectionbudget.Limit + 1, wantErr: selectionbudget.ErrExhausted, wantCalls: 1},
		{name: "dependency failure", dependency: true, wantErr: dependencyErr, wantCalls: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
			pinExcelBPSExecution(ctx)
			excluded := map[int64]struct{}{1: {}, 3: {}}
			var budgets []*selectionbudget.Budget
			var deadlines []time.Time
			svc := &OpenAIGatewayService{}
			svc.openaiScheduler = excelBPSBudgetScheduler{selectAccount: func(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				require.True(t, excelBPSExecutionPinned(ctx))
				require.Equal(t, map[int64]struct{}{1: {}, 3: {}}, req.ExcludedIDs, "neither route rejection nor admission veto can be erased")
				budgets = append(budgets, selectionbudget.From(ctx))
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				deadlines = append(deadlines, deadline)
				if tc.dependency {
					selectionbudget.RecordError(ctx, dependencyErr)
					return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
				}
				for range tc.operations {
					if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
						return nil, OpenAIAccountScheduleDecision{}, err
					}
				}
				return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
			}}
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
			require.ErrorIs(t, err, tc.wantErr)
			require.Nil(t, selection)
			require.Len(t, budgets, tc.wantCalls, "pool exhaustion cannot open a native selection phase")
			for i := range budgets {
				require.Same(t, budgets[0], budgets[i])
				require.Equal(t, deadlines[0], deadlines[i], "dependency retries cannot renew the deadline")
			}
			require.Equal(t, min(selectionbudget.Limit, tc.operations), budgets[0].Snapshot().Operations)
			require.Equal(t, map[int64]struct{}{1: {}, 3: {}}, excluded)
		})
	}
}

func TestExcelBPS429PoolExhaustionNeverReturnsNative(t *testing.T) {
	for _, loadBatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("loadBatch=%t", loadBatch), func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			dual := excelBPSDualGrantTestAccount(t, svc)
			dual.Status, dual.Schedulable, dual.Concurrency = StatusActive, true, 1
			other := excelAccount()
			other.ID, other.Concurrency = dual.ID+1, 1
			native := *dual
			native.ID = dual.ID + 2
			native.Extra = map[string]any{"openai_excel_bps": false}
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*dual, *other, native}}
			svc.cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatch
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			pinExcelBPSExecution(ctx)
			groupID := int64(123)
			excluded := make(map[int64]struct{})
			body := excelBPSBoundAttachmentBody(t, false)
			var uploadAccounts []int64
			nativeCalls := 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				if req.URL.Path == "/basispoints/api/attachments" {
					uploadAccounts = append(uploadAccounts, id)
					return bpsCompletionResponse(429, "{}"), nil
				}
				nativeCalls++
				return bpsCompletionResponse(200, bpsCompletedStream("resp_native", "unexpected native answer")), nil
			}}
			for range 2 {
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.True(t, selection.Acquired)
				require.NotEqual(t, native.ID, selection.Account.ID)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
				_, err = svc.Forward(ctx, c, selection.Account, body)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				requireExcelBPSRateLimitFailover(t, err, c)
				excluded[selection.Account.ID] = struct{}{}
			}
			require.ElementsMatch(t, []int64{dual.ID, other.ID}, uploadAccounts)
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "neither the dual grant nor a different native-only account may rescue this operation")
			require.Nil(t, selection)
			require.Equal(t, map[int64]struct{}{dual.ID: {}, other.ID: {}}, excluded)
			require.Zero(t, nativeCalls)
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
		{name: "retry after beyond fallback cap", headers: http.Header{"Retry-After": {"86400"}}, wantRetryAfter: "86400", wantCooldown: 24 * time.Hour},
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
			} else if status == http.StatusForbidden {
				// Upstream authorization failures reach the client as 503; see
				// TestExcelBPSForbiddenIsNotForwardedAsClientAuthError.
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			} else {
				require.Equal(t, status, rec.Code)
			}
			requireNoExcelBPSQuotaWrite(t, repo)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.True(t, account.IsSchedulable())
		})
	}
}

func TestExcelBPS429CooldownFollowsSetting(t *testing.T) {
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
				require.False(t, cooling, "a disabled cooldown only relies on this request's exclusion")
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
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.1"), "native Codex models stay schedulable")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "no account-wide block")

	account.Extra["openai_excel_bps"] = false
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"), "once BPS is off the model is native")
}

// This fake represents shared durable state, not an upstream or a real database.
// Repository integration tests separately exercise the PostgreSQL update.
type excelBPSCooldownRepoStub struct {
	AccountRepository
	mu            sync.Mutex
	deadlines     map[excelBPSScopedCooldownKey]time.Time
	readCalls     int
	readErr       error
	scopeReadErrs map[string]error
	writeErr      error
	writeCtxErr   error
	writeDeadline time.Time
}

func (r *excelBPSCooldownRepoStub) ExtendExcelBPSCooldown(ctx context.Context, accountID int64, scope string, until time.Time) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeCtxErr = ctx.Err()
	r.writeDeadline, _ = ctx.Deadline()
	if r.writeErr != nil {
		return time.Time{}, r.writeErr
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if r.deadlines == nil {
		r.deadlines = make(map[excelBPSScopedCooldownKey]time.Time)
	}
	key := excelBPSScopedCooldownKey{accountID: accountID, scope: scope}
	if until.After(r.deadlines[key]) {
		r.deadlines[key] = until
	}
	return r.deadlines[key], nil
}

func (r *excelBPSCooldownRepoStub) GetExcelBPSCooldown(ctx context.Context, accountID int64, scopes []string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readCalls++
	if r.readErr != nil {
		return time.Time{}, r.readErr
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	var until time.Time
	for _, scope := range scopes {
		if err := r.scopeReadErrs[scope]; err != nil {
			return time.Time{}, err
		}
		if saved := r.deadlines[excelBPSScopedCooldownKey{accountID: accountID, scope: scope}]; saved.After(until) {
			until = saved
		}
	}
	return until, nil
}

func TestExcelBPSRetryAfterPreservesAbsoluteNotBefore(t *testing.T) {
	now := time.Date(2026, time.October, 27, 12, 0, 0, 123456789, time.UTC)
	farFuture := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		value string
		want  time.Time
	}{
		{name: "long delta", value: "86400", want: now.Add(24 * time.Hour)},
		{name: "delta exceeds uint32", value: "5000000000", want: time.Unix(now.Unix()+5000000000, int64(now.Nanosecond()))},
		{name: "delta exceeds duration", value: "10000000000", want: time.Unix(now.Unix()+10000000000, int64(now.Nanosecond()))},
		{name: "long HTTP date", value: farFuture.Format(http.TimeFormat), want: farFuture},
		{name: "zero", value: "0", want: now},
		{name: "negative", value: "-30"},
		{name: "expired date", value: now.Add(-time.Minute).Format(http.TimeFormat)},
		{name: "unrepresentable delta", value: "18446744073709551615"},
		{name: "malformed", value: "tomorrow"},
		{name: "header injection", value: "30\r\nX-Test: yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			until, ok := ExcelBPSRetryAfterDeadline(tc.value, now)
			require.Equal(t, !tc.want.IsZero(), ok)
			require.True(t, tc.want.Equal(until), "got %s, want %s", until, tc.want)
		})
	}
}

func TestExcelBPSScopeCooldownPersistsAcrossServicesWithoutShortening(t *testing.T) {
	repo := &excelBPSCooldownRepoStub{}
	first := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	second := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	want := time.Now().Add(72 * time.Hour)
	scope := excelBPSRateLimitScope{model: "gpt-6-astra"}
	saved, err := first.coolDownExcelBPSScope(t.Context(), account, scope, "1", want)
	require.NoError(t, err)
	require.Equal(t, want, saved, "trusted reset is not capped by fallback or shortened by Retry-After")
	saved, err = second.coolDownExcelBPSScope(t.Context(), account, scope, "5", time.Time{})
	require.NoError(t, err)
	require.Equal(t, want, saved, "a different instance cannot shorten stored state")

	restarted := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	until, err := restarted.excelBPSRateLimitDeadline(t.Context(), account.ID, scope)
	require.NoError(t, err)
	require.Equal(t, want, until, "an empty local map still respects persisted not-before")
	require.False(t, restarted.isExcelBPSCoolingDown(account, "gpt-6-astra"), "a stale candidate snapshot cannot load shared state")
	snapshot := *account
	setAccountModelRateLimitSnapshot(&snapshot, scope.key(), want, string(ExcelBPSRateLimitedReason), time.Now())
	require.True(t, restarted.isExcelBPSCoolingDown(&snapshot, "gpt-6-astra"), "the outbox-refreshed snapshot supplies the candidate hint")
	require.False(t, restarted.isExcelBPSCoolingDown(account, "gpt-5.6-sol"))
	require.Equal(t, 1, repo.readCalls, "only the explicit send-time read reaches shared state")
	require.True(t, account.IsExcelBPSEnabled())
	require.Nil(t, account.RateLimitResetAt)
	require.Nil(t, account.Extra[modelRateLimitsKey], "do not mutate shared request/scheduler snapshots")
}

func TestExcelBPSScopeCooldownConcurrentExtensions(t *testing.T) {
	repo := &excelBPSCooldownRepoStub{}
	services := []*OpenAIGatewayService{{accountRepo: repo}, {accountRepo: repo}}
	account := excelAccount()
	scope := excelBPSRateLimitScope{endpoint: "/basispoints/api/attachments"}
	base := time.Now().Add(24 * time.Hour)
	start := make(chan struct{})
	errors := make(chan error, 32)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := services[i%len(services)].coolDownExcelBPSScope(t.Context(), account, scope, "", base.Add(time.Duration(i)*time.Hour))
			errors <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	restarted := &OpenAIGatewayService{accountRepo: repo}
	until, err := restarted.excelBPSRateLimitDeadline(t.Context(), account.ID, scope)
	require.NoError(t, err)
	require.Equal(t, base.Add(31*time.Hour), until)
	until, err = restarted.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{endpoint: "/basispoints/api/responses"})
	require.NoError(t, err)
	require.True(t, until.IsZero(), "attachment scope cannot throttle generation without evidence")
}

func TestExcelBPSScopeCooldownMatchesOnlyConfirmedScope(t *testing.T) {
	for _, limited := range []excelBPSRateLimitScope{
		{},
		{model: "gpt-6-astra"},
		{endpoint: "/basispoints/api/attachments"},
		{model: "gpt-6-astra", endpoint: "/basispoints/api/attachments"},
	} {
		t.Run(limited.key(), func(t *testing.T) {
			svc := &OpenAIGatewayService{accountRepo: &excelBPSCooldownRepoStub{}}
			account := excelAccount()
			_, err := svc.coolDownExcelBPSScope(t.Context(), account, limited, "86400", time.Time{})
			require.NoError(t, err)
			for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
				for _, endpoint := range []string{"/basispoints/api/attachments", "/basispoints/api/responses"} {
					scope := excelBPSRateLimitScope{model: model, endpoint: endpoint}
					until, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, scope)
					require.NoError(t, err)
					wantBlocked := (limited.model == "" || limited.model == model) && (limited.endpoint == "" || limited.endpoint == endpoint)
					require.Equal(t, wantBlocked, !until.IsZero(), "%+v", scope)
					other, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID+1, scope)
					require.NoError(t, err)
					require.True(t, other.IsZero(), "one account's failure is not a pool quota")
				}
			}
		})
	}
}

func TestExcelBPSCooldownReadFailureNeverGrantsEarlyRetry(t *testing.T) {
	readErr := fmt.Errorf("cooldown database unavailable")
	repo := &excelBPSCooldownRepoStub{readErr: readErr}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
	account := excelAccount()
	_, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{})
	require.ErrorIs(t, err, readErr, "the sending owner must report a dependency error, not an upstream 429")
	reads := repo.readCalls
	require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"), "a candidate filter must not manufacture a 429 from a dependency error")
	require.False(t, svc.isExcelBPSModelCoolingDown(account, "gpt-6-astra"), "unknown model state is not an upstream model denial")
	require.False(t, svc.isExcelBPSUpstreamModelCoolingDown(account.ID, "gpt-6-astra"))
	require.True(t, svc.excelBPSCooldownDeadline(account.ID).IsZero(), "the no-context helper is local-only")
	require.Equal(t, reads, repo.readCalls, "candidate and legacy helpers must not read the database")
	until, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{})
	require.ErrorIs(t, err, readErr, "the send boundary still blocks on the dependency error")
	require.True(t, until.IsZero(), "no invented one-second upstream cooldown")
	account.Extra["openai_cloud"] = true
	require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"), "BPS storage failure cannot close Cloud")
	delete(account.Extra, "openai_cloud")
	account.Extra["openai_excel_bps"] = false
	require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"), "BPS storage failure cannot close Native")
	require.Nil(t, account.RateLimitResetAt)
	require.Nil(t, repo.deadlines, "a read failure must not manufacture persisted cooldowns")
}

type excelBPSCandidateSettingsRepo struct {
	SettingRepository
	reads int
}

func (r *excelBPSCandidateSettingsRepo) GetValue(context.Context, string) (string, error) {
	r.reads++
	return "", fmt.Errorf("unexpected candidate settings read")
}

func TestExcelBPSCandidateCooldownsUseOnlySnapshotsAndLocalState(t *testing.T) {
	for _, source := range []string{"missing", "snapshot", "local", "expired"} {
		t.Run(source, func(t *testing.T) {
			repo := &excelBPSCooldownRepoStub{readErr: fmt.Errorf("shared store unavailable")}
			settings := &excelBPSCandidateSettingsRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: &SettingService{settingRepo: settings}}
			account := excelAccount()
			account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "second-alias": "gpt-6-astra"}
			account.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra", "gpt-5.6-sol"}
			until := time.Now().Add(time.Hour)
			if source == "expired" {
				until = time.Now().Add(-time.Second)
			}
			rateScope := (excelBPSRateLimitScope{model: "gpt-6-astra"}).key()
			permissionScope := excelBPSModelPermissionScope("gpt-6-astra")
			if source == "snapshot" || source == "expired" {
				setAccountModelRateLimitSnapshot(account, rateScope, until, string(ExcelBPSRateLimitedReason), time.Now())
				setAccountModelRateLimitSnapshot(account, permissionScope, until, string(ExcelBPSModelPermissionReason), time.Now())
			}
			if source == "local" || source == "expired" {
				svc.extendExcelBPSLocalCooldown(account.ID, rateScope, until)
				svc.extendExcelBPSModelLocalCooldown(account.ID, "gpt-6-astra", until)
			}
			blocked := source == "snapshot" || source == "local"
			for _, model := range []string{"alias", "second-alias"} {
				require.Equal(t, blocked, svc.isExcelBPSCoolingDown(account, model))
				require.Equal(t, blocked, svc.isExcelBPSModelCoolingDown(account, model))
			}
			require.Equal(t, source == "local", svc.isExcelBPSUpstreamModelCoolingDown(account.ID, "gpt-6-astra"), "the ID-only helper reads only its local model cache")
			for _, model := range []string{"gpt-5.6-sol", "gpt-5.1"} {
				require.False(t, svc.isExcelBPSCoolingDown(account, model), "other BPS and Native models stay eligible")
				require.False(t, svc.isExcelBPSModelCoolingDown(account, model))
			}
			account.Extra["openai_cloud"] = true
			require.False(t, svc.isExcelBPSCoolingDown(account, "alias"))
			require.False(t, svc.isExcelBPSModelCoolingDown(account, "alias"))
			delete(account.Extra, "openai_cloud")
			account.Extra["openai_excel_bps"] = false
			require.False(t, svc.isExcelBPSCoolingDown(account, "alias"))
			require.False(t, svc.isExcelBPSModelCoolingDown(account, "alias"))
			require.Zero(t, repo.readCalls)
			require.Zero(t, settings.reads, "a cold settings cache cannot trigger Background I/O")
		})
	}
}

func TestExcelBPSCandidateCooldownsUseLoadedGlobalPolicyWithoutIO(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			settings := &excelBPSCandidateSettingsRepo{}
			svc := &OpenAIGatewayService{settingService: &SettingService{settingRepo: settings}}
			svc.settingService.excelBPSEnabledCache.Store(&cachedExcelBPSEnabled{enabled: enabled, expiresAt: time.Now().Add(-time.Second)})
			account := excelAccount()
			until := time.Now().Add(time.Hour)
			svc.extendExcelBPSLocalCooldown(account.ID, excelBPSAccountCooldownScope, until)
			svc.extendExcelBPSModelLocalCooldown(account.ID, "gpt-6-astra", until)
			require.Equal(t, enabled, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
			require.Equal(t, enabled, svc.isExcelBPSModelCoolingDown(account, "gpt-6-astra"))
			require.Equal(t, until, svc.excelBPSCooldownDeadline(account.ID), "the local deadline helper makes no route decision")
			svc.settingService.invalidateExcelBPSEnabledCache()
			require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"), "policy invalidation must not fabricate a BPS route")
			require.False(t, svc.isExcelBPSModelCoolingDown(account, "gpt-6-astra"))
			require.Zero(t, settings.reads)
		})
	}
}

func TestExcelBPSCooldownSendDependencyFailureRemains503(t *testing.T) {
	for _, kind := range []string{"rate_limit", "model_permission"} {
		t.Run(kind, func(t *testing.T) {
			dependencyErr := fmt.Errorf("cooldown store unavailable")
			scope, reason, reads := excelBPSAccountCooldownScope, "basispoints_cooldown_unavailable", 2
			if kind == "model_permission" {
				scope, reason, reads = excelBPSModelPermissionScope("gpt-6-astra"), "basispoints_model_state_unavailable", 1
			}
			repo := &excelBPSCooldownRepoStub{scopeReadErrs: map[string]error{scope: dependencyErr}}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: excelBPSSettingsTestService()}
			account := excelAccount()
			ctx := withExcelBPSUpstreamModel(t.Context(), "gpt-6-astra")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			built := false
			response, _, err := svc.doExcelBPSRequestTo(ctx, c, account, "cooldown-test", "https://bps.openai.com/basispoints/api/responses", func(context.Context) (*http.Request, error) {
				built = true
				return nil, fmt.Errorf("unexpected request build")
			})
			require.ErrorIs(t, err, dependencyErr)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
			require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode)
			require.Equal(t, "selection", string(failure.Stage))
			require.Equal(t, reason, string(failure.Reason))
			require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
			require.Equal(t, NextAccountStop, failure.NextAccountAction)
			require.True(t, failure.RequestNotSent)
			require.Nil(t, response)
			require.False(t, built, "failed authoritative reads stop before preparing or sending upstream")
			require.Equal(t, reads, repo.readCalls)
		})
	}
}

func TestExcelBPSCooldownWriteSurvivesClientCancellationAndReportsFailure(t *testing.T) {
	repo := &excelBPSCooldownRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := excelAccount()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	until, err := svc.coolDownExcelBPSScope(ctx, account, excelBPSRateLimitScope{}, "86400", time.Time{})
	require.NoError(t, err)
	require.Nil(t, repo.writeCtxErr, "observed 429 state is saved after client cancellation")
	require.True(t, repo.writeDeadline.After(time.Now()))
	require.True(t, repo.writeDeadline.Before(time.Now().Add(openAIAccountStateUpdateTimeout+time.Second)))
	_, err = svc.excelBPSRateLimitDeadline(ctx, account.ID, excelBPSRateLimitScope{})
	require.ErrorIs(t, err, context.Canceled, "admission reads preserve cancellation")

	writeErr := fmt.Errorf("write confirmation unavailable")
	repo.writeErr = writeErr
	later, err := svc.coolDownExcelBPSScope(t.Context(), account, excelBPSRateLimitScope{}, "172800", time.Time{})
	require.ErrorIs(t, err, writeErr)
	require.True(t, later.After(until))
	require.Equal(t, later, svc.excelBPSLocalCooldownDeadline(account.ID, excelBPSAccountCooldownScope))
	require.Equal(t, until, repo.deadlines[excelBPSScopedCooldownKey{accountID: account.ID, scope: excelBPSAccountCooldownScope}], "an unconfirmed write cannot be claimed durable")
}

func TestExcelBPSTrustedResetWinsWithFallbackDisabled(t *testing.T) {
	repo := &excelBPSCooldownRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, nil, nil, nil)
	svc.rateLimitService.SetSettingService(NewSettingService(&excelBPSQuotaSettingsRepo{cooldown: `{"enabled":false,"cooldown_seconds":11}`}, nil))
	account := excelAccount()
	resetAt := time.Now().Add(48 * time.Hour)
	until, err := svc.coolDownExcelBPSScope(t.Context(), account, excelBPSRateLimitScope{}, "invalid", resetAt)
	require.NoError(t, err)
	require.Equal(t, resetAt, until)
	until, err = svc.coolDownExcelBPSScope(t.Context(), account, excelBPSRateLimitScope{}, "", time.Time{})
	require.NoError(t, err)
	require.Equal(t, resetAt, until, "disabled fallback never cancels an already saved not-before")
}

func TestExcelBPSRateLimitFailureUsesOnlyExplicitScopeEvidence(t *testing.T) {
	now := time.Now().UTC()
	const endpoint = "https://bps.openai.com/basispoints/api/responses"
	for _, tc := range []struct {
		name   string
		status int
		raw    string
		scope  excelBPSRateLimitScope
		ok     bool
	}{
		{name: "generic 429", status: 429, raw: `{"error":{"code":"rate_limit_exceeded","model":"gpt-6-astra","param":"model"}}`, ok: true},
		{name: "message does not set scope", status: 429, raw: `{"error":{"message":"model gpt-6-astra exceeded endpoint limit"}}`, ok: true},
		{name: "explicit model", status: 429, raw: `{"error":{"scope":"model","model":"gpt-6-astra"}}`, scope: excelBPSRateLimitScope{model: "gpt-6-astra"}, ok: true},
		{name: "explicit endpoint", status: 429, raw: `{"error":{"scope":"endpoint","endpoint":"/basispoints/api/responses"}}`, scope: excelBPSRateLimitScope{endpoint: "/basispoints/api/responses"}, ok: true},
		{name: "intersection", status: 429, raw: `{"error":{"scope":{"type":"model_endpoint","model":"gpt-6-astra","endpoint":"/basispoints/api/responses"}}}`, scope: excelBPSRateLimitScope{model: "gpt-6-astra", endpoint: "/basispoints/api/responses"}, ok: true},
		{name: "stream failure", status: 200, raw: `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","scope":"model","model":"gpt-6-astra"}}}`, scope: excelBPSRateLimitScope{model: "gpt-6-astra"}, ok: true},
		{name: "unknown scope never creates shared quota", status: 429, raw: `{"error":{"scope":"organization","quota_id":"shared"}}`, ok: true},
		{name: "conflicting model does not widen", status: 429, raw: `{"error":{"scope":"model","model":"other"}}`},
		{name: "conflicting endpoint does not widen", status: 429, raw: `{"error":{"scope":"endpoint","endpoint":"/basispoints/api/attachments"}}`},
		{name: "unrelated origin", status: 429, raw: `{"error":{"scope":"endpoint","endpoint":"https://other.example/basispoints/api/responses"}}`},
		{name: "malformed body", status: 429, raw: `{`, ok: true},
		{name: "real HTTP status wins", status: 500, raw: `{"error":{"code":"rate_limit_exceeded","scope":"model"}}`},
		{name: "permission is not 429", status: 403, raw: `{"error":{"code":"model_not_allowed","scope":"model"}}`},
		{name: "successful output is not an error", status: 200, raw: `{"type":"response.completed","response":{"output_text":"rate_limit_exceeded","scope":"model"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts, ok := parseExcelBPSRateLimitFailure(tc.status, http.Header{"Retry-After": {"86400"}}, []byte(tc.raw), "gpt-6-astra", endpoint, now)
			require.Equal(t, tc.ok, ok)
			if !ok {
				return
			}
			require.Equal(t, tc.scope, facts.scope)
			require.Equal(t, "86400", facts.retryAfter)
		})
	}
}

func TestExcelBPSRateLimitFailureResetNeedsScopeEvidence(t *testing.T) {
	now := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	reset := now.Add(72 * time.Hour)
	for _, scoped := range []bool{false, true} {
		t.Run(strconv.FormatBool(scoped), func(t *testing.T) {
			fields := map[string]any{
				"code": "usage_limit_reached", "resets_at": reset.Unix(),
				"resets_in_seconds": 86400, "retry_after": "172800",
			}
			if scoped {
				fields["scope"] = "model"
			}
			raw, err := json.Marshal(map[string]any{"error": fields})
			require.NoError(t, err)
			headers := excelBPSQuotaHeaders("100", "100")
			headers.Set("Retry-After", "30")
			facts, ok := parseExcelBPSRateLimitFailure(429, headers, raw, "gpt-6-astra", "/basispoints/api/responses", now)
			require.True(t, ok)
			if scoped {
				require.Equal(t, "172800", facts.retryAfter)
				require.True(t, reset.Equal(facts.resetAt), "reset deadline must represent the same instant")
				require.Equal(t, "gpt-6-astra", facts.scope.model)
			} else {
				require.Equal(t, "30", facts.retryAfter, "native-shaped body fields are not BPS reset proof")
				require.True(t, facts.resetAt.IsZero())
				require.Equal(t, excelBPSRateLimitScope{}, facts.scope)
			}
		})
	}
}

func TestExcelBPSEndpointCooldownMatchesTransportURL(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: &excelBPSCooldownRepoStub{}}
	account := excelAccount()
	_, err := svc.coolDownExcelBPSScope(t.Context(), account, excelBPSRateLimitScope{endpoint: "/basispoints/api/attachments"}, "86400", time.Time{})
	require.NoError(t, err)
	until, err := svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{endpoint: "https://bps.openai.com/basispoints/api/attachments"})
	require.NoError(t, err)
	require.False(t, until.IsZero())
	until, err = svc.excelBPSRateLimitDeadline(t.Context(), account.ID, excelBPSRateLimitScope{endpoint: "https://other.example/basispoints/api/attachments"})
	require.NoError(t, err)
	require.True(t, until.IsZero(), "different origins are not the same BPS endpoint")
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

func TestExcelBPS429NoNativePreservesBoundaries(t *testing.T) {
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
			account := excelBPSDualGrantTestAccount(t, svc)
			svc.coolDownExcelBPS(t.Context(), account, "30")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			_, err := svc.Forward(t.Context(), c, account, []byte(tc.body))
			requireExcelBPSRateLimitFailover(t, err, c)
			require.Empty(t, upstream.requests, "do not migrate credential-owned or unresolved history")
		})
	}
	for _, mode := range []string{"excel_only", "probe", "cancel", "attempt_budget", "deadline", "visible_output"} {
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
			account := excelBPSDualGrantTestAccount(t, svc)
			if mode == "excel_only" {
				account = excelAccount()
			}
			budget := NewOpenAITurnBudget(svc.cfg)
			if mode == "attempt_budget" {
				budget.attemptBudget = newOpenAITurnAttemptBudget(1)
			}
			if mode == "deadline" {
				setTurnOutputDeadlineForTest(budget, time.Now().Add(-time.Second))
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
				requireExcelBPSRateLimitFailover(t, err, c)
				require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts, "do not consume a second attempt for native")
			}
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			for _, req := range upstream.requests {
				require.Equal(t, "/basispoints/api/responses", req.URL.Path)
			}
		})
	}
}

func TestExcelBPSPinnedBusySelectionKeepsBPSWaitPlan(t *testing.T) {
	for _, acquired := range []bool{false, true} {
		for _, previous := range []string{"", "resp_owner"} {
			t.Run(fmt.Sprintf("acquired=%t/previous=%s", acquired, previous), func(t *testing.T) {
				ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
				pinExcelBPSExecution(ctx)
				excluded := map[int64]struct{}{1: {}, 3: {}}
				account := excelAccount()
				account.ID = 40
				waitPlan := &AccountWaitPlan{AccountID: account.ID, MaxWaiting: 3, Timeout: time.Second}
				calls := 0
				svc := &OpenAIGatewayService{}
				svc.openaiScheduler = excelBPSBudgetScheduler{selectAccount: func(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
					calls++
					require.True(t, excelBPSExecutionPinned(ctx))
					require.Equal(t, excluded, req.ExcludedIDs)
					return &AccountSelectionResult{Account: account, Acquired: acquired, WaitPlan: waitPlan}, OpenAIAccountScheduleDecision{}, nil
				}}
				got, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, previous, "sticky", "gpt-6-astra", excluded, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true)
				require.NoError(t, err)
				require.NotNil(t, got)
				require.Same(t, account, got.Account)
				require.Same(t, waitPlan, got.WaitPlan, "a busy BPS candidate does not authorize switching modes")
				require.Equal(t, acquired, got.Acquired)
				require.Equal(t, 1, calls)
				require.Equal(t, map[int64]struct{}{1: {}, 3: {}}, excluded)
			})
		}
	}
}
