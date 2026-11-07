//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	coderws "github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// excelBPSImageAdmissionSettingRepo enables image support with a single
// in-flight BPS request.
type excelBPSImageAdmissionSettingRepo struct {
	service.SettingRepository
}

func (excelBPSImageAdmissionSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{
		service.SettingKeyExcelBPSEnabled:           "true",
		service.SettingKeyExcelBPSImageRelayEnabled: "true",
		service.SettingKeyExcelBPSImageBaseURL:      "https://images.example",
		service.SettingKeyExcelBPSImageMaxRequests:  "1",
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r excelBPSImageAdmissionSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	values, err := r.GetMultiple(ctx, []string{key})
	return values[key], err
}

// The lower priority value is scheduled first.
func excelBPSImageAdmissionAccount(id, groupID int64, bps bool, priority int) service.Account {
	extra := map[string]any{}
	if bps {
		extra["openai_excel_bps"] = true
		extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	}
	account := service.Account{
		ID: id, Name: fmt.Sprintf("account-%d", id), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{groupID}, Priority: priority,
		Credentials: map[string]any{"access_token": fmt.Sprintf("token-%d", id), "chatgpt_account_id": fmt.Sprintf("chatgpt-%d", id)},
		Extra:       extra,
	}
	if bps {
		account.Credentials["client_id"] = openai.ExcelClientID
	}
	return account
}

func newExcelBPSImageAdmissionTestHandler(t *testing.T, upstream service.HTTPUpstream, loadBatch bool, accounts []service.Account) *OpenAIGatewayHandler {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 32 << 20}}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	concurrencyService := service.NewConcurrencyService(nil)
	var schedulingConcurrency *service.ConcurrencyService
	if loadBatch {
		cfg.Gateway.Scheduling.LoadBatchEnabled = true
		schedulingConcurrency = concurrencyService
	}
	settingService := service.NewSettingService(excelBPSImageAdmissionSettingRepo{}, cfg)
	require.NotEmpty(t, accounts)
	require.Len(t, accounts[0].GroupIDs, 1)
	authority := newExcelBPSHandlerAuthority(t, cfg, accounts, accounts[0].GroupIDs[0])
	gatewayService := service.NewOpenAIGatewayService(
		authority.accounts,
		nil, nil, authority.users, nil, nil, nil,
		cfg,
		nil, schedulingConcurrency, nil, nil, nil,
		&excelBPSHandlerUpstream{HTTPUpstream: upstream, t: t},
		nil, nil, nil, nil, nil, nil, settingService, nil,
	)
	t.Cleanup(func() { _ = gatewayService.CloseExcelBPSImages() })
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cleanupHandlerBillingCache(t, billingService)
	require.NoError(t, billingService.Start(t.Context()))
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		authority.keys,
		nil, nil, nil, nil,
		cfg,
	)
	handler.maxAccountSwitches = 10
	return handler
}

// newExcelBPSImageAdmissionTestContext builds a client request as the gateway
// routes would mark it.
func newExcelBPSImageAdmissionTestContext(groupID int64) (*gin.Context, *httptest.ResponseRecorder) {
	c, rec := newExcelBPSFailoverTestContext(context.Background(), false)
	c.Set(string(middleware2.ContextKeyAPIKey), newExcelBPSHandlerAPIKey(99, groupID))
	service.MarkExcelBPSImageAdmission(c)
	return c, rec
}

// holdExcelBPSImageSlot sends a BPS request whose upstream call blocks until
// the returned release runs, so it keeps the only image admission slot.
func holdExcelBPSImageSlot(t *testing.T, handler *OpenAIGatewayHandler, groupID int64, entered <-chan struct{}, release func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c, rec := newExcelBPSImageAdmissionTestContext(groupID)
	c.Request = c.Request.WithContext(ctx)
	done := make(chan struct{})
	reachedUpstream := false
	t.Cleanup(func() {
		release()
		if !reachedUpstream {
			cancel()
		}
		<-done
		cancel()
		if reachedUpstream {
			require.Equal(t, http.StatusOK, rec.Code, "the holder must finish normally")
		}
	})
	go func() {
		defer close(done)
		handler.Responses(c)
	}()
	select {
	case <-entered:
		reachedUpstream = true
	case <-done:
		t.Fatalf("holder finished before reaching BPS: HTTP %d %s", rec.Code, rec.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("holder request did not reach the BPS upstream")
	}
}

func blockingFirstBPSCall() (*excelBPSFailoverUpstream, <-chan struct{}, func()) {
	entered := make(chan struct{})
	blocked := make(chan struct{})
	upstream := &excelBPSFailoverUpstream{
		answer: func(int) *http.Response { return excelBPSCompleted() },
		onDo: func(call int) {
			if call == 0 {
				close(entered)
				<-blocked
			}
		},
	}
	return upstream, entered, sync.OnceFunc(func() { close(blocked) })
}

func TestOpenAIResponsesExcelBPSImageBusyDoesNotSwitchToNative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DATA_DIR", t.TempDir())
	for _, loadBatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("load_batch=%t", loadBatch), func(t *testing.T) {
			upstream, entered, release := blockingFirstBPSCall()
			// The holder occupies BPS capacity; an available Native candidate
			// cannot bypass that limit for an operation already selecting BPS.
			handler := newExcelBPSImageAdmissionTestHandler(t, upstream, loadBatch, []service.Account{
				excelBPSImageAdmissionAccount(2, 4242, true, 1),
				excelBPSImageAdmissionAccount(3, 4242, false, 2),
			})
			holdExcelBPSImageSlot(t, handler, 4242, entered, release)
			require.Equal(t, []int64{2}, upstream.calls())

			c, rec := newExcelBPSImageAdmissionTestContext(4242)
			handler.Responses(c)

			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "1", rec.Header().Get("Retry-After"))
			require.Equal(t, string(service.ExcelBPSImageRequestBusyReason), gjson.Get(rec.Body.String(), "error.code").String())
			require.Equal(t, []int64{2}, upstream.calls(), "only the holder may send; neither the busy BPS attempt nor Native may dispatch")
			require.Equal(t, basispoints.ResponsesURL, upstream.urls[0])
		})
	}
}

func TestExcelBPSImageBusyExhaustionAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DATA_DIR", t.TempDir())
	for _, protocol := range []string{"json", "sse", "messages-json", "messages", "chat-json", "chat", "ws"} {
		t.Run(protocol, func(t *testing.T) {
			upstream, entered, release := blockingFirstBPSCall()
			h := newExcelBPSImageAdmissionTestHandler(t, upstream, false, []service.Account{
				excelBPSImageAdmissionAccount(1, 3131, true, 1), excelBPSImageAdmissionAccount(2, 3131, true, 1),
			})
			holdExcelBPSImageSlot(t, h, 3131, entered, release)
			c, rec := newExcelBPSImageAdmissionTestContext(3131)
			stream := protocol != "json" && !strings.HasSuffix(protocol, "-json")
			body := map[string]any{"model": "gpt-6-astra", "input": "hello", "stream": stream}
			path := "/v1/responses"
			if strings.HasPrefix(protocol, "messages") || strings.HasPrefix(protocol, "chat") {
				delete(body, "input")
				body["messages"] = []any{map[string]any{"role": "user", "content": "hello"}}
				body["max_tokens"] = 100
				path = "/v1/chat/completions"
				if strings.HasPrefix(protocol, "messages") {
					path = "/v1/messages"
				}
			}
			if protocol == "ws" {
				body["type"] = "response.create"
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
			c.MustGet(string(middleware2.ContextKeyAPIKey)).(*service.APIKey).Group.AllowMessagesDispatch = true
			if protocol == "ws" {
				failure := make(chan service.OpsStreamError, 1)
				router := gin.New()
				router.Use(middleware2.ExcelBPSImageAdmission())
				router.GET(path, func(wsCtx *gin.Context) {
					wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
					wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
					h.ResponsesWebSocket(wsCtx)
					if e, ok := service.GetOpsStreamError(wsCtx); ok {
						failure <- e
					}
				})
				server := httptest.NewServer(router)
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, nil)
				require.NoError(t, err)
				defer client.CloseNow()
				require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(event), "Image relay request capacity is busy")
				require.Contains(t, string(event), `"type":"error"`)
				require.NoError(t, client.CloseNow())
				select {
				case e := <-failure:
					require.Equal(t, http.StatusServiceUnavailable, e.IntendedStatus)
					require.Equal(t, string(service.ExcelBPSImageRequestBusyReason), e.Code)
					require.True(t, e.RequestScoped)
					require.Zero(t, e.UpstreamStatus, "local rejection must not invent an upstream HTTP response")
				case <-ctx.Done():
					t.Fatal("missing local capacity diagnostic")
				}
			} else {
				switch {
				case strings.HasPrefix(protocol, "messages"):
					h.Messages(c)
				case strings.HasPrefix(protocol, "chat"):
					h.ChatCompletions(c)
				default:
					h.Responses(c)
				}
				require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
				require.Equal(t, "1", rec.Header().Get("Retry-After"))
				require.Equal(t, string(service.ExcelBPSImageRequestBusyReason), gjson.Get(rec.Body.String(), "error.code").String())
				require.Contains(t, rec.Body.String(), "Image relay request capacity is busy")
			}
			require.Len(t, upstream.calls(), 1, "rejected attempts never reach upstream")
		})
	}
}
