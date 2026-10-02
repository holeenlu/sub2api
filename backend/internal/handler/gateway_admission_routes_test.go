//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAdmissionRealForwardingHandlersRejectBeforeUpstream(t *testing.T) {
	upstream := &openAIResponsesFailoverCancelUpstream{}
	h := newOpenAIResponsesFailoverTestHandler(t, upstream)
	helper, _ := newAPIKeyAdmissionHelper(t)
	h.concurrencyHelper = helper
	ctx, cancel := service.WithAPIKeyAdmissionOwner(context.Background())
	defer cancel()
	release, err := helper.AcquireAPIKeySlot(ctx, 99, 1)
	require.NoError(t, err)
	defer release()
	for _, tc := range []struct {
		name string
		grok bool
		run  func(*gin.Context)
	}{
		{"input_tokens", false, h.ResponsesInputTokens},
		{"count_tokens", false, h.CountTokens},
		{"realtime", true, h.GrokRealtime},
		{"tts", true, func(c *gin.Context) { h.GrokVoice(c, "tts") }},
		{"stt", true, func(c *gin.Context) { h.GrokVoice(c, "stt") }},
		{"custom_voices", true, func(c *gin.Context) { h.GrokVoice(c, "custom-voices") }},
		{"codex_models", false, h.CodexModels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, response := newOpenAIResponsesFailoverTestContext(t, ctx)
			key, _ := middleware2.GetAPIKeyFromContext(c)
			key.ConcurrencyLimit = 1
			key.Group.AllowMessagesDispatch = true
			if tc.grok {
				key.Group.Platform = service.PlatformGrok
			}
			c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"gpt-5.1","input":"hello","messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Connection", "Upgrade")
			c.Request.Header.Set("Upgrade", "websocket")
			tc.run(c)
			require.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "API key")
			require.Empty(t, upstream.calls())
		})
	}
}

// grokMediaSlotKeyCount reports the slots held by grokMediaSlotContext's key.
func grokMediaSlotKeyCount(t *testing.T, cache service.ConcurrencyCache) int {
	t.Helper()
	keyCache, ok := cache.(service.APIKeyConcurrencyCache)
	require.True(t, ok)
	counts, err := keyCache.GetAPIKeyConcurrencyBatch(context.Background(), []int64{20})
	require.NoError(t, err)
	return counts[20]
}

// Grok voice admits the key once, inside AcquireUserSlotWithWait. A second
// admission doubled the key's count and made a limit-1 key reject its only
// request before upstream.
func TestAPIKeyAdmissionGrokVoiceHoldsOneKeySlot(t *testing.T) {
	for _, limit := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			h, slots, _, up := newGrokMediaSlotHandler(t, false, false)
			helper, cache := newAPIKeyAdmissionHelper(t)
			h.concurrencyHelper = helper
			entered := make(chan struct{})
			finish := make(chan struct{})
			finishUpstream := sync.OnceFunc(func() { close(finish) })
			defer finishUpstream()
			var calls atomic.Int32
			up.call = func(*http.Request, int64) (*http.Response, error) {
				// Only the first call parks; an over-admitted request fails fast.
				if calls.Add(1) > 1 {
					return nil, errors.New("unexpected upstream call")
				}
				close(entered)
				<-finish
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"audio/mpeg"}},
					Body: io.NopCloser(strings.NewReader("audio"))}, nil
			}
			newTTS := func() (*gin.Context, *httptest.ResponseRecorder) {
				ctx := context.Background()
				if limit > 0 {
					// Auth installs the admission owner only for limited keys.
					var cancel context.CancelFunc
					ctx, cancel = service.WithAPIKeyAdmissionOwner(ctx)
					t.Cleanup(cancel)
				}
				c, w := grokMediaSlotContext(ctx, true)
				c.Request = httptest.NewRequest(http.MethodPost, "/tts", strings.NewReader(`{"input":"hello","voice":"Ara"}`)).WithContext(ctx)
				key, _ := middleware2.GetAPIKeyFromContext(c)
				key.ConcurrencyLimit = limit
				return c, w
			}

			first, firstW := newTTS()
			done := make(chan struct{})
			go func() { defer close(done); h.GrokVoice(first, "tts") }()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("sole request did not reach upstream: %d %s", firstW.Code, firstW.Body.String())
			case <-time.After(5 * time.Second):
				t.Fatal("sole request did not reach upstream")
			}
			require.Equal(t, 1, grokMediaSlotKeyCount(t, cache), "a forwarding request holds exactly one key slot")
			if limit == 1 {
				second, secondW := newTTS()
				h.GrokVoice(second, "tts")
				require.Equal(t, http.StatusTooManyRequests, secondW.Code, secondW.Body.String())
				require.Contains(t, secondW.Body.String(), "API key")
				require.EqualValues(t, 1, calls.Load(), "the rejected request must not reach upstream")
				require.Equal(t, 1, grokMediaSlotKeyCount(t, cache), "the rejected request must neither keep nor free a key slot")
			}
			finishUpstream()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("sole request did not return")
			}
			require.Equal(t, http.StatusOK, firstW.Code, firstW.Body.String())
			require.Equal(t, "audio", firstW.Body.String())
			require.Zero(t, grokMediaSlotKeyCount(t, cache))
			users, err := cache.GetUserConcurrency(context.Background(), 10)
			require.NoError(t, err)
			require.Zero(t, users)
			slots.assertReleased(t)
		})
	}
}

// Realtime shares the single admission: a limit-1 key's only session must get
// past it. Non-Grok accounts stop the request at account selection, after
// admission and before any upstream dial.
func TestAPIKeyAdmissionGrokRealtimeAdmitsSoleRequest(t *testing.T) {
	h, slots, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	helper, cache := newAPIKeyAdmissionHelper(t)
	h.concurrencyHelper = helper
	ctx, cancel := service.WithAPIKeyAdmissionOwner(context.Background())
	defer cancel()
	c, w := grokMediaSlotContext(ctx, false)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil).WithContext(ctx)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")
	key, _ := middleware2.GetAPIKeyFromContext(c)
	key.ConcurrencyLimit = 1

	h.GrokRealtime(c)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "No available Grok accounts")
	require.Zero(t, grokMediaSlotKeyCount(t, cache))
	slots.assertReleased(t)
}

func TestAPIKeyAdmissionPrecedesUserQueueAndSSE(t *testing.T) {
	helper, _ := newAPIKeyAdmissionHelper(t)
	helper.pingFormat = SSEPingFormatComment
	ctx, cancel := service.WithAPIKeyAdmissionOwner(context.Background())
	defer cancel()
	release, acquired, err := helper.TryAcquireUserSlotForAPIKey(ctx, 1, 1, 77, 1)
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()
	c, recorder := newHelperTestContext(http.MethodPost, "/v1/responses")
	c.Request = c.Request.WithContext(ctx)
	started := false
	start := time.Now()
	next, err := helper.acquireUserSlotWithWaitTimeout(c, 1, 1, 77, 1, time.Second, true, &started)
	require.Nil(t, next)
	status, _, _, message := concurrencyErrorResponse(err, "user")
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Contains(t, message, "API key")
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.False(t, started)
	require.Empty(t, recorder.Body.String(), "no heartbeat may commit HTTP 200 before admission")
}

func TestAPIKeyAdmissionCanceledUserWaitReturnsReservation(t *testing.T) {
	helper, cache := newAPIKeyAdmissionHelper(t)
	userRelease, acquired, err := helper.TryAcquireUserSlot(context.Background(), 1, 1)
	require.NoError(t, err)
	require.True(t, acquired)
	defer userRelease()
	parent, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	ctx, cancelOwner := service.WithAPIKeyAdmissionOwner(parent)
	defer cancelOwner()
	c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
	c.Request = c.Request.WithContext(ctx)
	done := make(chan error, 1)
	go func() {
		started := false
		release, err := helper.acquireUserSlotWithWaitTimeout(c, 1, 1, 77, 1, time.Second, false, &started)
		if release != nil {
			release()
		}
		done <- err
	}()
	keyCache := cache.(service.APIKeyConcurrencyCache)
	require.Eventually(t, func() bool {
		counts, err := keyCache.GetAPIKeyConcurrencyBatch(context.Background(), []int64{77})
		return err == nil && counts[77] == 1
	}, time.Second, time.Millisecond)
	cancelRequest()
	require.ErrorIs(t, <-done, context.Canceled)
	counts, err := keyCache.GetAPIKeyConcurrencyBatch(context.Background(), []int64{77})
	require.NoError(t, err)
	require.Zero(t, counts[77])
	userCount, err := cache.GetUserConcurrency(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, userCount, "the existing request's user slot must be preserved")
}
