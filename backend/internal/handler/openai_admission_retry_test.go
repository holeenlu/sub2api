//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type initialAdmissionRepo struct {
	nativeAdmissionFailoverAccountRepo
	changed      map[int64]bool
	readErr      error
	onRead       func()
	initialReads []int64
	slots        *helperConcurrencyCacheStub
}

func (r *initialAdmissionRepo) GetOpenAITurnAdmission(ctx context.Context, id int64) (*service.Account, *service.Account, error) {
	r.initialReads = append(r.initialReads, id)
	if r.onRead != nil {
		r.onRead()
	}
	if r.readErr != nil {
		return nil, nil, r.readErr
	}
	a, err := r.GetByID(ctx, id)
	if a != nil && r.changed[id] {
		a.Extra = maps.Clone(a.Extra)
		if a.Extra == nil {
			a.Extra = make(map[string]any)
		}
		a.Extra["openai_ws_force_http"] = true
	}
	return a, nil, err
}

func newInitialAdmissionHandler(t *testing.T, repo *initialAdmissionRepo, upstream service.HTTPUpstream) *OpenAIGatewayHandler {
	t.Helper()
	for id := int64(1); id <= 2; id++ {
		repo.accounts = append(repo.accounts, service.Account{
			ID: id, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{3131}, Priority: int(id), Concurrency: 1,
			Credentials: map[string]any{"access_token": fmt.Sprintf("test-token-%d", id), "chatgpt_account_id": fmt.Sprintf("test-account-%d", id)},
			Extra:       map[string]any{},
		})
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	repo.slots = &helperConcurrencyCacheStub{accountSeq: []bool{true, true, true}}
	concurrency := service.NewConcurrencyService(repo.slots)
	gw := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, concurrency, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := NewOpenAIGatewayHandler(gw, concurrency, billing,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 2
	return h
}

func TestOpenAIInitialAdmissionReselectsBeforeSending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, chat := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("chat=%t/stream=%t", chat, stream), func(t *testing.T) {
				repo := &initialAdmissionRepo{changed: map[int64]bool{1: true}}
				upstream := &nativeAdmissionFailoverUpstream{answer: func(int) *http.Response {
					resp := nativeAdmissionCompleted()
					wire, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					delta := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"done\",\"output_index\":0,\"content_index\":0}\n\n"
					resp.Body = io.NopCloser(strings.NewReader(delta + string(wire)))
					return resp
				}}
				h := newInitialAdmissionHandler(t, repo, upstream)
				c, rec := newNativeAdmissionFailoverTestContext(context.Background(), stream)
				if chat {
					// Exercise the same Chat Completions messages shape as model tests.
					body := fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream)
					c.Request.Body = io.NopCloser(strings.NewReader(body))
					c.Request.ContentLength = int64(len(body))
					c.Request.URL.Path = "/v1/chat/completions"
					h.ChatCompletions(c)
				} else {
					h.Responses(c)
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "done")
				require.Equal(t, []int64{2}, upstream.calls(), "the stale route must never be sent")
				require.Equal(t, int64(1), repo.initialReads[0])
				repo.slots.mu.Lock()
				acquired, released := repo.slots.accountAcquireCalls, repo.slots.accountReleaseCalls
				repo.slots.mu.Unlock()
				require.Equal(t, 2, acquired)
				require.Equal(t, acquired, released, "both rejected and successful attempts release their slots")
				require.NotContains(t, rec.Body.String(), "admission_unavailable")
				_, recordedUpstreamError := c.Get(service.OpsUpstreamErrorsKey)
				require.False(t, recordedUpstreamError, "a local route change is not an upstream failure")
			})
		}
	}
}

func TestOpenAIInitialAdmissionRetryBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, chat := range []bool{false, true} {
		for _, kind := range []string{"budget", "all_changed", "database_error", "cancel", "downstream_written", "conversation"} {
			t.Run(fmt.Sprintf("chat=%t/%s", chat, kind), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				repo := &initialAdmissionRepo{changed: map[int64]bool{1: true}}
				upstream := &nativeAdmissionFailoverUpstream{answer: func(int) *http.Response { return nativeAdmissionCompleted() }}
				h := newInitialAdmissionHandler(t, repo, upstream)
				c, rec := newNativeAdmissionFailoverTestContext(ctx, false)
				switch kind {
				case "budget":
					h.maxAccountSwitches = 0
				case "all_changed":
					repo.changed[2] = true
				case "database_error":
					repo.readErr = errors.New("test database unavailable")
				case "cancel":
					repo.onRead = cancel
				case "downstream_written":
					repo.onRead = func() { c.Writer.WriteHeaderNow() }
				case "conversation":
					c.Request.Body = http.NoBody
					body := `{"model":"gpt-6-astra","input":"hello","conversation":"conv_test"}`
					c.Request.Body = io.NopCloser(strings.NewReader(body))
					c.Request.ContentLength = int64(len(body))
				}
				if chat {
					c.Request.URL.Path = "/v1/chat/completions"
					h.ChatCompletions(c)
				} else {
					h.Responses(c)
				}
				require.Empty(t, upstream.calls(), "unsafe/exhausted admission must not send")
				if kind == "all_changed" {
					require.Equal(t, []int64{1, 2}, repo.initialReads)
				} else {
					require.Equal(t, []int64{1}, repo.initialReads)
				}
				if kind != "cancel" && kind != "downstream_written" {
					require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), "admission_unavailable")
				}
			})
		}
	}
}

func TestOpenAIInitialAdmissionCannotReplayContinuationOrOutput(t *testing.T) {
	for _, kind := range []string{"previous_response_id", "conversation", "partial_result", "written", "canceled", "budget", "already_excluded", "later_send"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &initialAdmissionRepo{changed: map[int64]bool{1: true}}
			upstream := &nativeAdmissionFailoverUpstream{answer: func(int) *http.Response { t.Fatal("unexpected upstream call"); return nil }}
			h := newInitialAdmissionHandler(t, repo, upstream)
			c, _ := newNativeAdmissionFailoverTestContext(ctx, false)
			account, err := repo.GetByID(ctx, 1)
			require.NoError(t, err)
			_, admissionErr := h.gatewayService.Forward(ctx, c, account, []byte(`{"model":"gpt-6-astra"}`))
			require.True(t, service.IsOpenAIInitialAdmissionRejection(admissionErr))
			body := []byte(`{"model":"gpt-6-astra","input":"hello"}`)
			excluded := map[int64]struct{}{}
			switches, budget := 0, 2
			var result *service.OpenAIForwardResult
			switch kind {
			case "previous_response_id":
				body = []byte(`{"previous_response_id":"resp_test"}`)
			case "conversation":
				body = []byte(`{"conversation":{"id":"conv_test"}}`)
			case "partial_result":
				result = &service.OpenAIForwardResult{}
			case "written":
				_, err = c.Writer.Write([]byte(": heartbeat\n\n"))
				require.NoError(t, err)
			case "canceled":
				cancel()
			case "budget":
				budget = 0
			case "already_excluded":
				excluded[1] = struct{}{}
			case "later_send":
				admissionErr = &service.OpenAITurnAdmissionError{Reason: "account_binding_changed"}
			}
			require.False(t, retryOpenAIInitialAdmission(c, admissionErr, result, body, 1, excluded, &switches, budget))
			require.Zero(t, switches)
			require.Empty(t, upstream.calls())
		})
	}
}

// The client history carries a completed tool round trip; a switched account
// must receive exactly the same converted history.
const nativeAdmissionFailoverRequestBody = `{"model":"gpt-6-astra","stream":%t,"input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"list the files"}]},` +
	`{"type":"function_call","call_id":"call_ls","name":"shell","arguments":"{\"cmd\":\"ls\"}"},` +
	`{"type":"function_call_output","call_id":"call_ls","output":"a.txt"}],` +
	`"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}`

// nativeAdmissionFailoverUpstream answers in call order and records the account of
// every upstream attempt.
type nativeAdmissionFailoverUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	bodies     [][]byte
	urls       []string
	answer     func(call int) *http.Response
	onDo       func(call int)
}

func (u *nativeAdmissionFailoverUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	call := len(u.accountIDs)
	u.accountIDs = append(u.accountIDs, accountID)
	u.bodies = append(u.bodies, body)
	u.urls = append(u.urls, req.URL.String())
	u.mu.Unlock()
	if u.onDo != nil {
		u.onDo(call)
	}
	return u.answer(call), nil
}

func (u *nativeAdmissionFailoverUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func nativeAdmissionCompleted() *http.Response {
	wire := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_failover\",\"status\":\"completed\",\"model\":\"gpt-6-astra\"," +
		"\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
}

// nativeAdmissionFailoverAccountRepo also answers the no-account diagnosis query.
type nativeAdmissionFailoverAccountRepo struct {
	openAIImagesFailoverAccountRepo
}

func (r nativeAdmissionFailoverAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	var out []service.Account
	for _, platform := range platforms {
		out = append(out, r.accountsForPlatform(platform)...)
	}
	return out, nil
}

// loadBatch selects the production default (load-aware) scheduling path;
// otherwise the simple priority path is used.

func newNativeAdmissionFailoverTestContext(ctx context.Context, stream bool) (*gin.Context, *httptest.ResponseRecorder) {
	groupID := int64(3131)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(fmt.Sprintf(nativeAdmissionFailoverRequestBody, stream))))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
		User:    &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	return c, rec
}
