package handler

import (
	"context"
	"errors"
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
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Exercise the real concurrency helper and queue, substituting only the cache
// port. Queue counters are observed independently of account-slot releases.
type wsAdmissionMaintenanceCache struct {
	*wsNativeFixtureCapacity
	waits           atomic.Int32
	dequeues        atomic.Int32
	maxWaiting      atomic.Int32
	onWait          func(context.Context)
	queueFull       bool
	afterLeaseCheck func()
}

var _ routing.CapacityLeaseStore = (*wsAdmissionMaintenanceCache)(nil)

func newWSAdmissionMaintenanceCache(t *testing.T) *wsAdmissionMaintenanceCache {
	t.Helper()
	return &wsAdmissionMaintenanceCache{wsNativeFixtureCapacity: newWSNativeFixtureCapacity(t, &concurrencyCacheMock{}, nil)}
}

func (s *wsAdmissionMaintenanceCache) CheckCapacityLease(ctx context.Context, request routing.CapacityRequest) (time.Duration, error) {
	remaining, err := s.CapacityLeaseStore.CheckCapacityLease(ctx, request)
	if err == nil && remaining > 0 && s.afterLeaseCheck != nil {
		s.afterLeaseCheck()
	}
	return remaining, err
}

func (s *wsAdmissionMaintenanceCache) IncrementAccountWaitCount(ctx context.Context, _ int64, limit int) (bool, error) {
	s.waits.Add(1)
	s.maxWaiting.Store(int32(limit))
	if s.onWait != nil {
		s.onWait(ctx)
	}
	return !s.queueFull, nil
}

func (s *wsAdmissionMaintenanceCache) DecrementAccountWaitCount(context.Context, int64) error {
	s.dequeues.Add(1)
	return nil
}

func newWSAdmissionMaintenanceHandler(t *testing.T, cache service.ConcurrencyCache, timeout time.Duration, enabled bool) (*OpenAIGatewayHandler, *service.Account, *openAIWSUsageHandlerAccountRepoStub) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = timeout
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	value := "false"
	if enabled {
		value = "true"
	}
	settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyExcelBPSEnabled: value}}, cfg)
	account := &service.Account{
		ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		GroupIDs:    []int64{wsGroupPricingGroupID},
		Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "synthetic-excel-token", "chatgpt_account_id": "synthetic-owner"},
		Extra:       map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-5.1"}},
	}
	repo := &openAIWSUsageHandlerAccountRepoStub{account: cloneWSNativeFixtureAccount(*account)}
	concurrency := service.NewConcurrencyService(cache)
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil)
	return &OpenAIGatewayHandler{gatewayService: gateway, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Millisecond)}, account, repo
}

func wsAdmissionMaintenanceContext(ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	return c, recorder
}

func TestWSAdmissionMaintenanceBPSWait(t *testing.T) {
	for _, outcome := range []string{"available", "dual_grant", "canceled", "deadline", "queue_full", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			available := outcome == "available" || outcome == "dual_grant"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := newWSAdmissionMaintenanceCache(t)
			cache.queueFull = outcome == "queue_full"
			var probes atomic.Int32
			cache.overrides.acquireAccountSlotFn = func(ctx context.Context, id int64, limit int, token string) (bool, error) {
				call := probes.Add(1)
				if err := ctx.Err(); err != nil {
					return false, err
				}
				return available && call >= 3, nil
			}
			queued := make(chan struct{})
			cache.onWait = func(context.Context) { close(queued) }
			timeout := time.Second
			if outcome == "timeout" {
				timeout = 20 * time.Millisecond
			}
			h, account, repo := newWSAdmissionMaintenanceHandler(t, cache, timeout, true)
			if outcome == "dual_grant" {
				delete(account.Credentials, "client_id")
				repo.mu.Lock()
				repo.account = cloneWSNativeFixtureAccount(*account)
				repo.mu.Unlock()
				require.True(t, h.gatewayService.IsExcelBPSRouteEnabled(ctx, account, "gpt-5.1"), "native credentials do not bypass BPS admission")
			}
			groupID := wsGroupPricingGroupID
			if outcome == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 250*time.Millisecond)
				defer stop()
			}
			c, recorder := wsAdmissionMaintenanceContext(ctx)
			type result struct {
				release  func()
				acquired bool
				err      error
			}
			done := make(chan result, 1)
			go func() {
				release, _, acquired, err := h.acquireOpenAIWSContinuationAccountSlot(ctx, c, account, 1, "gpt-5.1", &groupID)
				done <- result{release, acquired, err}
			}()
			select {
			case <-queued:
			case <-time.After(2 * time.Second):
				t.Fatal("BPS did not enter its owner queue")
			}
			if outcome == "canceled" {
				cancel()
			}
			var got result
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("BPS admission exceeded its bound")
			}
			require.EqualValues(t, 1, cache.waits.Load())
			require.EqualValues(t, 2, cache.maxWaiting.Load())
			if available {
				require.NoError(t, got.err)
				require.True(t, got.acquired)
				require.NotNil(t, got.release)
				require.GreaterOrEqual(t, probes.Load(), int32(3))
				got.release()
				require.EqualValues(t, 1, atomic.LoadInt32(&cache.overrides.releaseAccountCalled))
			} else {
				require.False(t, got.acquired)
				require.Nil(t, got.release)
				require.Zero(t, atomic.LoadInt32(&cache.overrides.releaseAccountCalled))
				switch outcome {
				case "canceled":
					require.ErrorIs(t, got.err, context.Canceled)
				case "deadline":
					require.ErrorIs(t, got.err, context.DeadlineExceeded)
				case "queue_full":
					require.NoError(t, got.err)
				case "timeout":
					var capacity *ConcurrencyError
					require.ErrorAs(t, got.err, &capacity)
					require.True(t, capacity.IsTimeout)
				}
			}
			if outcome == "queue_full" {
				require.Zero(t, cache.dequeues.Load(), "cannot release a queue reservation we never acquired")
				require.EqualValues(t, 1, probes.Load())
			} else {
				require.EqualValues(t, 1, cache.dequeues.Load())
			}
			require.False(t, c.Writer.Written(), "WS admission must not write SSE keepalives")
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestWSAdmissionMaintenanceNonBPSIsImmediate(t *testing.T) {
	for _, mode := range []string{"native", "bps_disabled", "global_disabled", "model_native", "cloud", "zero_timeout", "zero_queue"} {
		t.Run(mode, func(t *testing.T) {
			cache := newWSAdmissionMaintenanceCache(t)
			var probes int
			cache.overrides.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) { probes++; return false, nil }
			h, account, _ := newWSAdmissionMaintenanceHandler(t, cache, time.Second, mode != "global_disabled")
			var plan *service.AccountWaitPlan
			switch mode {
			case "native":
				account.Extra["openai_excel_bps"] = false
			case "bps_disabled":
				delete(account.Credentials, "client_id")
				account.Extra["openai_excel_bps"] = false
			case "model_native":
				account.Extra["openai_excel_bps_models"] = []string{"another-model"}
			case "cloud":
				account.Extra[service.OpenAICloudEnabledKey] = true
			case "zero_timeout", "zero_queue":
				plan = h.gatewayService.OpenAIAccountContinuationWaitPlan(context.Background(), account)
				if mode == "zero_timeout" {
					plan.Timeout = 0
				} else {
					plan.MaxWaiting = 0
				}
			}
			c, _ := wsAdmissionMaintenanceContext(context.Background())
			if mode == "bps_disabled" {
				require.False(t, h.gatewayService.IsExcelBPSRouteEnabled(c.Request.Context(), account, "gpt-5.1"), "a fresh request uses the disabled account configuration")
			}
			release, acquired, err := h.acquireOpenAIWSAccountSlot(c.Request.Context(), c, account, 1, "gpt-5.1", plan)
			require.NoError(t, err)
			require.False(t, acquired)
			require.Nil(t, release)
			require.Equal(t, 1, probes)
			require.Zero(t, cache.waits.Load())
			require.Zero(t, cache.dequeues.Load())
			require.False(t, c.GetBool(service.OpsLocalDependencyFailureKey), "a retryable slot miss is not a terminal local cause")
		})
	}
}

func TestWSAdmissionMaintenanceQueuedCancelPreservesNextTurn(t *testing.T) {
	cache := newWSAdmissionMaintenanceCache(t)
	var available atomic.Bool
	cache.overrides.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) { return available.Load(), nil }
	queued := make(chan struct{})
	cache.onWait = func(context.Context) { close(queued) }
	h, account, _ := newWSAdmissionMaintenanceHandler(t, cache, 5*time.Second, true)
	groupID := wsGroupPricingGroupID
	admissions := &wsNativeFixtureAdmission{}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	serverDone := make(chan struct{})
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			results <- err
			return
		}
		defer conn.CloseNow()
		_ = service.RunOpenAIWSMultiplexer(host.WithTurnAdmission(ctx, admissions), conn, nil, func(laneCtx context.Context, ds *service.OpenAIWSDownstream) {
			_, _, err := ds.Read(laneCtx)
			if err != nil {
				results <- err
				return
			}
			c, _ := wsAdmissionMaintenanceContext(laneCtx)
			c.Set("openai_ws_downstream_owner", ds)
			release, _, acquired, err := h.acquireOpenAIWSContinuationAccountSlot(laneCtx, c, account, 1, "gpt-5.1", &groupID)
			if release != nil {
				release()
			}
			if acquired {
				sends.Add(1)
			}
			results <- err
			if !errors.Is(err, service.ErrOpenAIWSAdmissionCancelled) {
				return
			}
			failure := markOpenAIWSTurnFailure(c, &service.UpstreamFailoverError{Cause: err})
			next, err := ds.FinishTurnAndReadNext(laneCtx, nil, failure.Code, failure.Message)
			if err != nil {
				results <- err
				return
			}
			if gjson.GetBytes(next, "event_id").String() != "next" {
				results <- errors.New("admission consumed the next create")
				return
			}
			release, _, acquired, err = h.acquireOpenAIWSContinuationAccountSlot(laneCtx, c, account, 1, "gpt-5.1", &groupID)
			if err == nil && acquired {
				sends.Add(1)
				err = ds.Write(laneCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"next_response","status":"completed"}}`))
			} else if err == nil {
				err = errors.New("next turn failed to acquire available capacity")
			}
			if release != nil {
				release()
			}
			results <- err
		})
	}))
	defer server.Close()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","stream_id":"a","event_id":"first","model":"gpt-5.1","input":"first"}`)))
	select {
	case <-queued:
	case err := <-results:
		t.Fatalf("admission failed before the queue: %v", err)
	case <-ctx.Done():
		t.Fatal("BPS did not enter its queue")
	}
	// A response ID from a prior turn must not cancel the queued operation.
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.cancel","stream_id":"a","response_id":"old","event_id":"wrong"}`)))
	_, event, err := client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "response_not_active", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "wrong", gjson.GetBytes(event, "event_id").String())
	require.Zero(t, cache.dequeues.Load())
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.cancel","stream_id":"a","event_id":"stop"}`)))
	_, event, err = client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "request_cancelled", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "stop", gjson.GetBytes(event, "event_id").String())
	require.Equal(t, "a", gjson.GetBytes(event, "stream_id").String())
	select {
	case err := <-results:
		require.ErrorIs(t, err, service.ErrOpenAIWSAdmissionCancelled)
	case <-ctx.Done():
		t.Fatal("cancel did not stop queue admission")
	}
	require.Zero(t, sends.Load(), "cancelled admission must never dispatch")
	require.EqualValues(t, 1, cache.waits.Load())
	require.EqualValues(t, 1, cache.dequeues.Load())
	available.Store(true)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","stream_id":"a","event_id":"next","model":"gpt-5.1","input":"next"}`)))
	_, event, err = client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), "no duplicate terminal may precede the next turn")
	select {
	case err := <-results:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("next turn did not finish")
	}
	require.EqualValues(t, 1, sends.Load())
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.overrides.releaseAccountCalled))
	require.NoError(t, client.CloseNow())
	waitWSHandlerFixture(t, serverDone, "queued cancellation owner")
	admissions.assertClosed(t, true)
}

func TestWSAdmissionMaintenanceRechecksAfterAcquire(t *testing.T) {
	for _, change := range []string{"revoked", "credentials", "profit", "canceled"} {
		t.Run(change, func(t *testing.T) {
			cache := newWSAdmissionMaintenanceCache(t)
			h, account, repo := newWSAdmissionMaintenanceHandler(t, cache, time.Second, true)
			group := &service.Group{ID: wsGroupPricingGroupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 3, ProfitControlEnabled: true, ProfitMinMargin: 0.1}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxkey.Group, group))
			defer cancel()
			if change == "canceled" {
				// Cancel after Redis proves a real acquired token, so rollback
				// must release that receipt rather than a mock-only success.
				cache.afterLeaseCheck = cancel
			}
			ctx, _ = h.gatewayService.WithOpenAITurnPricingContext(ctx, &group.ID)
			c, _ := wsAdmissionMaintenanceContext(ctx)
			var probes int
			cache.overrides.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) {
				probes++
				if probes == 1 {
					return false, nil
				}
				repo.mu.Lock()
				switch change {
				case "revoked":
					repo.account.Schedulable = false
				case "credentials":
					repo.account.Credentials["access_token"] = "rotated-synthetic-token"
				case "profit":
					rate := 100.0
					repo.account.RateMultiplier = &rate
				}
				repo.mu.Unlock()
				return true, nil
			}
			release, _, acquired, err := h.acquireOpenAIWSContinuationAccountSlot(ctx, c, account, 1, "gpt-5.1", &group.ID)
			require.Error(t, err)
			require.False(t, acquired)
			require.Nil(t, release)
			require.EqualValues(t, 1, atomic.LoadInt32(&cache.overrides.releaseAccountCalled))
			require.EqualValues(t, 1, cache.dequeues.Load())
			if change == "credentials" {
				var changed *service.OpenAIWSAccountChangedError
				require.ErrorAs(t, err, &changed)
			}
			if change == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.False(t, c.GetBool(service.OpsLocalDependencyFailureKey), "eligibility errors are not capacity failures")
		})
	}
}

func TestWSAdmissionMaintenanceJSONSSEBoundary(t *testing.T) {
	for _, code := range []string{"", "gateway_queue_full"} {
		for _, state := range []string{"prepared", "stream_started", "headers_committed"} {
			t.Run(state+"/"+code, func(t *testing.T) {
				c, recorder := wsAdmissionMaintenanceContext(context.Background())
				c.Header("Content-Type", "text/event-stream")
				started := state == "stream_started"
				if state != "prepared" {
					c.Writer.Flush()
				}
				(&OpenAIGatewayHandler{}).handleStreamingAwareErrorWithCode(c, http.StatusTooManyRequests, "rate_limit_error", code, "Account queue is full", started, false)
				if state == "prepared" {
					require.Equal(t, http.StatusTooManyRequests, recorder.Code)
					require.Equal(t, "application/json", recorder.Result().Header.Get("Content-Type"))
					require.True(t, gjson.ValidBytes(recorder.Body.Bytes()))
					require.Equal(t, code, gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
				} else {
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Equal(t, "text/event-stream", recorder.Result().Header.Get("Content-Type"))
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
					require.NotContains(t, recorder.Body.String(), "event: error")
					require.False(t, gjson.ValidBytes(recorder.Body.Bytes()))
				}
			})
		}
	}
}

func TestWSAdmissionMaintenanceLocalCauseAtHTTPConsumer(t *testing.T) {
	for _, failure := range []string{"queue_full", "wait_timeout", "dependency", "provider"} {
		t.Run(failure, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 4)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			cache := newWSAdmissionMaintenanceCache(t)
			cache.queueFull = failure == "queue_full"
			cache.overrides.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) {
				if failure == "dependency" {
					return false, errors.New("cache unavailable")
				}
				return false, nil
			}
			h, account, _ := newWSAdmissionMaintenanceHandler(t, cache, 20*time.Millisecond, true)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				setOpsRequestContext(c, "gpt-5.1", false)
				setOpsSelectedAccount(c, account.ID, account.Platform)
				setActualUpstreamEndpoint(c, "/v1/responses")
				service.SetOpsUpstreamError(c, 502, "provider failed", "provider detail")
				c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{Kind: "http_error", UpstreamStatusCode: 502, Message: "provider failed"}})
				if failure == "provider" {
					// A provider-controlled error string/code cannot forge local provenance.
					h.handleStreamingAwareErrorWithCode(c, 502, "upstream_error", gatewayQueueFullCode, "Too many pending requests, please retry later", false, true)
					return
				}
				selection := &service.AccountSelectionResult{Account: account, WaitPlan: &service.AccountWaitPlan{AccountID: account.ID, MaxConcurrency: 1, Timeout: 20 * time.Millisecond, MaxWaiting: 2}}
				started := false
				release, result := h.acquireResponsesAccountSlot(c, nil, "", selection, false, &started, zap.NewNop())
				require.Nil(t, release)
				require.Equal(t, openAISlotAcquireFailed, result)
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Len(t, opsErrorLogQueue, 1)
			entry := (<-opsErrorLogQueue).entry
			if failure != "provider" {
				require.Equal(t, "platform", entry.ErrorOwner)
				require.Equal(t, "gateway", entry.ErrorSource)
				require.False(t, entry.IsBusinessLimited)
				require.Nil(t, entry.UpstreamStatusCode)
				require.Nil(t, entry.UpstreamErrorMessage)
				require.Empty(t, entry.UpstreamEndpoint)
				require.NotNil(t, entry.UpstreamErrorsJSON)
				require.Contains(t, *entry.UpstreamErrorsJSON, "provider failed")
			} else {
				require.Equal(t, "provider", entry.ErrorOwner, "provider-controlled text cannot establish local provenance")
				require.NotNil(t, entry.UpstreamStatusCode)
				require.Equal(t, 502, *entry.UpstreamStatusCode)
			}
		})
	}
}

// Real WS ingress -> provider -> Native evidence journal -> ReleaseTurn. The
// fixture journal verifies the actual capacity receipts before accepting usage;
// early release on cancellation therefore cannot satisfy these assertions.
func TestWSAdmissionMaintenanceNativeReleaseAfterFinalization(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		for _, canceled := range []bool{false, true} {
			name := mode + "/completed"
			if canceled {
				name = mode + "/canceled"
			}
			t.Run(name, func(t *testing.T) {
				cache := &concurrencyCacheMock{}
				tc := openAIResponsesWSUsageLogCase{ingressMode: mode, firstPayload: wsGroupPricingFirst, concurrencyCache: cache, disconnectAfterUsage: canceled}
				turns := 1
				if canceled {
					// response.created usage without a terminal is partial evidence.
					zero := 0
					tc.expectedUsageCount = &zero
				} else {
					tc.secondPayload = wsGroupPricingSecond
					turns = 2
				}
				got := runOpenAIResponsesWebSocketUsageLogCase(t, tc)
				if canceled {
					require.Empty(t, got.logs, "partial usage must not fabricate a Final UsageLog")
					records := nativeWSFixtureRecoveryRecords(t, got.nativeJournal)
					require.Len(t, records, 1)
					record := records[0].Finalization
					evidence := record.Evidence.Evidence
					require.Equal(t, billing.NativeUsagePartial, evidence.Confidence)
					require.False(t, evidence.TerminalConfirmed)
					require.NotNil(t, evidence.Usage)
					require.NotNil(t, evidence.Usage.InputTokens)
					require.NotNil(t, evidence.Usage.OutputTokens)
					require.EqualValues(t, 8, *evidence.Usage.InputTokens)
					require.EqualValues(t, 4, *evidence.Usage.OutputTokens)
					require.Nil(t, record.Evidence.SettlementSequence)
					require.Nil(t, record.Evidence.SettlementUsage)
					require.False(t, record.Evidence.Receipt.Durable(), "partial facts are not a canonical settlement")
					require.Empty(t, got.nativeJournal.identities())
				} else {
					require.Len(t, got.logs, turns)
				}
				require.EqualValues(t, turns, atomic.LoadInt32(&cache.releaseAccountCalled))
				require.EqualValues(t, turns, atomic.LoadInt32(&cache.releaseUserCalled))
				got.nativeJournal.mu.Lock()
				records := len(got.nativeJournal.finalizations)
				got.nativeJournal.mu.Unlock()
				require.Equal(t, turns, records)
			})
		}
	}
}

func TestWSAdmissionMaintenanceBudgetPreservesDeliveredFailure(t *testing.T) {
	for _, state := range []string{"terminal", "committed_json", "heartbeat", "text", "prepared"} {
		t.Run(state, func(t *testing.T) {
			c, recorder := wsAdmissionMaintenanceContext(t.Context())
			terminal := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"prior\",\"status\":\"failed\"}}\n\n"
			switch state {
			case "terminal":
				c.Header("Content-Type", "text/event-stream")
				_, err := c.Writer.WriteString(terminal)
				require.NoError(t, err)
				c.Writer.Flush()
				// Exercise the handler consumer of the service's delivery marker.
				c.Set("openai_stream_terminal_failure_delivered", true)
			case "committed_json":
				c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "original_failure"}})
				service.MarkResponseCommitted(c)
			case "heartbeat", "text":
				c.Header("Content-Type", "text/event-stream")
				body := ": ping\n\n"
				if state == "text" {
					body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
				}
				_, err := c.Writer.WriteString(body)
				require.NoError(t, err)
				c.Writer.Flush()
			case "prepared":
				c.Header("Content-Type", "text/event-stream")
			}
			before := recorder.Body.String()
			require.True(t, (&OpenAIGatewayHandler{}).handleOpenAITurnBudgetError(c, service.ErrOpenAITurnAttemptBudgetExhausted, c.Writer.Written()))
			switch state {
			case "terminal", "committed_json":
				require.Equal(t, before, recorder.Body.String(), "must preserve the delivered failure byte for byte")
			case "heartbeat", "text":
				require.True(t, strings.HasPrefix(recorder.Body.String(), before))
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
				require.NotContains(t, recorder.Body.String(), "event: error")
			case "prepared":
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Equal(t, "application/json", recorder.Result().Header.Get("Content-Type"))
				require.True(t, gjson.ValidBytes(recorder.Body.Bytes()))
			}
		})
	}
}

func TestWSAdmissionMaintenanceLocalCauseAtWSConsumer(t *testing.T) {
	for _, kind := range []string{"dependency", "queue", "provider", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := wsAdmissionMaintenanceContext(t.Context())
			service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
			service.BeginOpsStreamTurn(c, 2)
			service.SetOpsUpstreamError(c, 502, "previous provider attempt", "detail")
			original := errors.New("cache unavailable")
			failure := &service.UpstreamFailoverError{StatusCode: 503, Cause: original}
			switch kind {
			case "dependency":
				cache := newWSAdmissionMaintenanceCache(t)
				cache.overrides.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) { return false, original }
				h, account, _ := newWSAdmissionMaintenanceHandler(t, cache, time.Second, true)
				groupID := wsGroupPricingGroupID
				release, _, acquired, err := h.acquireOpenAIWSContinuationAccountSlot(t.Context(), c, account, 1, "gpt-5.1", &groupID)
				require.Nil(t, release)
				require.False(t, acquired)
				require.ErrorIs(t, err, original)
				require.ErrorIs(t, err, routing.ErrCapacityDependencyUnavailable)
				failure.Cause = err
			case "queue":
				failure.Cause = &ConcurrencyError{SlotType: "account", IsTimeout: true}
			case "cancel":
				failure.Cause = service.ErrOpenAIWSAdmissionCancelled
			}
			markOpenAIWSTurnFailure(c, failure)
			projection, ok := service.GetOpsStreamError(c)
			require.True(t, ok)
			require.Equal(t, kind == "dependency" || kind == "queue", projection.LocalDependencyFailure)
			if kind == "dependency" || kind == "queue" {
				require.Zero(t, projection.UpstreamStatus)
				require.Empty(t, projection.UpstreamMessage)
			}
			if kind == "cancel" {
				require.False(t, projection.CountTowardsSLA)
				require.Equal(t, "request_cancelled", projection.Code)
			}
		})
	}
}

func TestWSAdmissionMaintenanceRetryAcquirePreservesLocalCause(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 4)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var probes atomic.Int32
	cache := &concurrencyCacheMock{acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
		if probes.Add(1) == 1 {
			return true, nil
		}
		return false, errors.New("capacity authority unavailable")
	}}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond,
		ingressMode: service.OpenAIWSIngressModeHTTPBridge, concurrencyCache: cache,
		opsService: ops, secondTurnErrorExpected: true,
		// The changed-account retry skips BeforeTurn and reaches the real
		// ensureAttemptSlots consumer before opening another bridge attempt.
		updateAccountAfterFirstTurn: func(account *service.Account) { account.Extra["openai_responses_flatten_namespaces"] = true },
	})
	require.Len(t, got.logs, 1)
	require.Len(t, got.upstreamPayloads, 1, "retry admission failure must not send the second turn")
	require.EqualValues(t, 2, probes.Load())
	require.Len(t, opsErrorLogQueue, 1)
	entry := (<-opsErrorLogQueue).entry
	require.Equal(t, "platform", entry.ErrorOwner)
	require.Equal(t, "gateway", entry.ErrorSource)
	require.False(t, entry.IsBusinessLimited)
	require.Nil(t, entry.UpstreamStatusCode)
	require.Empty(t, entry.UpstreamEndpoint)
}

func TestWSAdmissionMaintenanceNativeBusyDoesNotWait(t *testing.T) {
	var probes atomic.Int32
	cache := &concurrencyCacheMock{acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return probes.Add(1) == 1, nil }}
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond,
		ingressMode: service.OpenAIWSIngressModeCtxPool, concurrencyCache: cache, secondTurnErrorExpected: true,
	})
	require.Len(t, got.logs, 1)
	require.Len(t, got.upstreamPayloads, 1, "busy native continuation must not dispatch")
	require.EqualValues(t, 2, probes.Load(), "native continuation must not enter the BPS retry queue")
}

func TestWSAdmissionMaintenanceDirectErrorJSONSSEBoundary(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "committed"}[committed], func(t *testing.T) {
			c, recorder := wsAdmissionMaintenanceContext(t.Context())
			c.Header("Content-Type", "text/event-stream")
			if committed {
				c.Writer.Flush()
			}
			(&OpenAIGatewayHandler{}).errorResponse(c, http.StatusServiceUnavailable, "api_error", "Local admission unavailable")
			if committed {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, "text/event-stream", recorder.Result().Header.Get("Content-Type"))
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
				require.NotContains(t, recorder.Body.String(), "event: error")
				require.False(t, gjson.ValidBytes(recorder.Body.Bytes()))
			} else {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Equal(t, "application/json", recorder.Result().Header.Get("Content-Type"))
				require.True(t, gjson.ValidBytes(recorder.Body.Bytes()))
			}
		})
	}
}
