//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type securityVoiceConcurrency struct{ *grokMediaSlotsCache }

func (s *securityVoiceConcurrency) AcquireUserSlot(_ context.Context, id int64, limit int, request string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) >= limit {
		return false, nil
	}
	s.users[request] = id
	s.userAcquired++
	return true, nil
}
func (s *securityVoiceConcurrency) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return false, nil
}

func TestSecurityVoiceConcurrencyHeldThroughCanceledUpstream(t *testing.T) {
	for _, endpoint := range []string{"tts", "stt"} {
		t.Run(endpoint, func(t *testing.T) {
			h, slots, _, up := newGrokMediaSlotHandler(t, false, false)
			h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(&securityVoiceConcurrency{slots}), SSEPingFormatComment, 0)
			entered := make(chan struct{})
			finish := make(chan struct{})
			finishUpstream := sync.OnceFunc(func() { close(finish) })
			defer finishUpstream()
			done := make(chan struct{})
			var calls atomic.Int32
			up.call = func(*http.Request, int64) (*http.Response, error) {
				// Only the first call parks; a request admitted through a leaked
				// slot fails fast instead of deadlocking the package.
				if calls.Add(1) > 1 {
					return nil, errors.New("unexpected upstream call")
				}
				close(entered)
				<-finish
				return nil, errors.New("upstream disconnected")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first, _ := grokMediaSlotContext(ctx, true)
			first.Request = httptest.NewRequest(http.MethodPost, "/"+endpoint, strings.NewReader(`{"input":"hello","voice":"Ara"}`)).WithContext(ctx)
			first.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 10, Concurrency: 1})
			go func() { defer close(done); h.GrokVoice(first, endpoint) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("voice did not reach upstream")
			}
			cancel()
			// A cancel-triggered release runs on its own goroutine; one snapshot could miss it.
			require.Never(t, func() bool {
				slots.mu.Lock()
				defer slots.mu.Unlock()
				return len(slots.users) != 1
			}, 50*time.Millisecond, time.Millisecond, "canceling the client must not free the user while upstream is still draining")
			second, w := grokMediaSlotContext(context.Background(), true)
			second.Request = httptest.NewRequest(http.MethodPost, "/"+endpoint, strings.NewReader(`{"input":"hello","voice":"Ara"}`))
			second.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 10, Concurrency: 1})
			h.GrokVoice(second, endpoint)
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
			require.EqualValues(t, 1, calls.Load(), "the rejected request must not reach upstream")
			finishUpstream()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("voice did not release")
			}
			slots.mu.Lock()
			require.Empty(t, slots.users)
			require.Equal(t, 1, slots.userAcquired)
			require.Equal(t, 1, slots.userReleased)
			slots.mu.Unlock()
		})
	}
}

func TestSecurityRealtimeEffectiveDefaultAllowlist(t *testing.T) {
	for _, query := range []string{"", "?model=", "?model=%20%20", "?model=grok-voice-latest"} {
		t.Run(query, func(t *testing.T) {
			h, slots, _, _ := newGrokMediaSlotHandler(t, false, false)
			c, w := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"grok-other"}}
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime"+query, nil)
			c.Request.Header.Set("Connection", "Upgrade")
			c.Request.Header.Set("Upgrade", "websocket")
			h.GrokRealtime(c)
			require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			require.Zero(t, slots.acquired, "default model denial must precede upstream selection")
			slots.assertReleased(t)
		})
	}
	require.Empty(t, blockedModelAllowlistCandidate(&service.Group{ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"grok-voice-latest"}}}, []string{"grok-voice-latest"}))
}

func TestSecurityHTTPDuplicateContinuationBeforeNormalization(t *testing.T) {
	for _, field := range []string{`"previous_response_id":"","previous_response_id":"resp_other"`, `"previous_response_id":"","previous_response_\u0069d":"resp_other"`, `"Previous_Response_ID":"resp_other"`} {
		h := newOpenAIHandlerForPreviousResponseIDValidation(t, nil)
		body := `{"model":"gpt-5.1",` + field + `,"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{}}}}}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		group := int64(2)
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 101, UserID: 1, GroupID: &group, User: &service.User{ID: 1}})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		h.Responses(c)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "ambiguous JSON field")
	}
}
