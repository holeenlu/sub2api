//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const nativeHandlerSuccessStream = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native_fixture\",\"model\":\"gpt-5.1\",\"status\":\"in_progress\"}}\n\n" +
	"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"OK\"}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_fixture\",\"model\":\"gpt-5.1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n"

type nativeHandlerSuccessUpstream struct {
	service.HTTPUpstream
	calls atomic.Int32
}

func (u *nativeHandlerSuccessUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(nativeHandlerSuccessStream))}, nil
}

func nativeHandlerFixtureAccounts(limit int) []service.Account {
	return []service.Account{{ID: 1, Name: "native-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: limit,
		Credentials: map[string]any{"access_token": "handler-oauth-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}}}
}

type nativeHandlerEntry struct {
	name, path, body string
	call             func(*OpenAIGatewayHandler, *gin.Context)
}

func nativeHandlerEntries() []nativeHandlerEntry {
	return []nativeHandlerEntry{
		{name: "responses", path: "/v1/responses", body: "{\"model\":\"gpt-5.1\",\"input\":\"hello\",\"stream\":false}", call: func(h *OpenAIGatewayHandler, c *gin.Context) { h.Responses(c) }},
		{name: "messages", path: "/v1/messages", body: "{\"model\":\"gpt-5.1\",\"max_tokens\":100,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":false}", call: func(h *OpenAIGatewayHandler, c *gin.Context) { h.Messages(c) }},
		{name: "chat", path: "/v1/chat/completions", body: "{\"model\":\"gpt-5.1\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":false}", call: func(h *OpenAIGatewayHandler, c *gin.Context) { h.ChatCompletions(c) }},
	}
}

func nativeEntryContext(t *testing.T, ctx context.Context, entry nativeHandlerEntry, limit int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx = logger.IntoContext(ctx, newWSNativeFixtureLogger(t))
	c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)
	c.Request = httptest.NewRequest(http.MethodPost, entry.path, strings.NewReader(entry.body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.User.Concurrency = limit
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: limit})
	return c, rec
}

func TestNativeHTTPHandlerEntriesUseActualCapacityAndPermit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range nativeHandlerEntries() {
		for _, limit := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/concurrency_%d", entry.name, limit), func(t *testing.T) {
				h, store, transport := newNativeHTTPHandlerFixture(t, nativeHandlerFixtureAccounts(limit), &nativeHandlerSuccessUpstream{})
				store.update(func(s *nativeHandlerStore) { s.key.User.Concurrency = limit })
				transport.onOpen = func(req *http.Request, options native.TransportOptions) {
					scope := routing.CapacityScopeFrom(req.Context())
					require.NotNil(t, scope)
					for _, policy := range []routing.CapacityPolicy{{ResourceID: 100, User: true, Limit: limit}, {ResourceID: options.AccountID, Limit: limit}} {
						receipt := scope.Lookup(policy.User, policy.ResourceID)
						require.NotNil(t, receipt)
						require.Equal(t, limit == 0, receipt.Unlimited)
						evidence, err := receipt.Verify(req.Context(), policy)
						require.NoError(t, err)
						require.NotEqual(t, [32]byte{}, evidence.Fingerprint())
						require.Equal(t, limit == 0, evidence.ValidUntil().IsZero())
					}
				}
				c, rec := nativeEntryContext(t, t.Context(), entry, limit)
				entry.call(h, c)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "OK")
				require.EqualValues(t, 1, transport.prepared.Load())
				require.EqualValues(t, 1, transport.opened.Load())
				require.Zero(t, transport.legacy.Load())
				require.GreaterOrEqual(t, store.keyReads.Load(), int32(4), "preparation, Admit, Consume and connection acquisition must read current key data")
				require.GreaterOrEqual(t, store.userReads.Load(), int32(2), "both capacity checks must read current user policy")
			})
		}
	}
}

func TestNativeHTTPHandlerRevocationAcrossAuthorityAwaitStopsSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range nativeHandlerEntries() {
		for _, read := range []int32{2, 3, 4} {
			t.Run(fmt.Sprintf("%s/key_read_%d", entry.name, read), func(t *testing.T) {
				upstream := &nativeHandlerSuccessUpstream{}
				h, store, transport := newNativeHTTPHandlerFixture(t, nativeHandlerFixtureAccounts(1), upstream)
				store.update(func(s *nativeHandlerStore) { s.key.User.Concurrency = 1 })
				ready, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var resumeOnce sync.Once
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(func() {
					cancel()
					resumeOnce.Do(func() { close(resume) })
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("handler did not join before fixture cleanup")
					}
				})
				store.beforeKeyRead = func(ctx context.Context, count int32) {
					if count == read {
						close(ready)
						select {
						case <-resume:
						case <-ctx.Done():
						}
					}
				}
				c, rec := nativeEntryContext(t, ctx, entry, 1)
				go func() { defer close(done); entry.call(h, c) }()
				select {
				case <-ready:
				case <-done:
					t.Fatal("handler returned before the authority await")
				case <-time.After(5 * time.Second):
					t.Fatal("authority await was not reached")
				}
				store.update(func(s *nativeHandlerStore) { s.key.Status = service.StatusDisabled })
				resumeOnce.Do(func() { close(resume) })
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("revoked handler did not finish")
				}
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				require.EqualValues(t, 1, transport.prepared.Load())
				if read == 4 {
					require.EqualValues(t, 1, transport.opened.Load(), "connection acquisition follows Consume")
				} else {
					require.Zero(t, transport.opened.Load())
				}
				require.Zero(t, upstream.calls.Load(), "revocation must prevent request transmission")
				require.Zero(t, transport.legacy.Load())
				_, upstreamStatus := c.Get(service.OpsUpstreamStatusCodeKey)
				require.False(t, upstreamStatus, "local revocation must not be an upstream health failure")
			})
		}
	}
}
