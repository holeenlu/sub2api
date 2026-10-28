//go:build unit

package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Port the fixed-main continuation assertions to the real Native authority
// fixture and explicit billing lifecycle. Shared fixture definitions stay owned
// by MAIN; this test supplies only its own account/key state and response cache.
func TestOpenAIResponses_ContinuationOwnerUnavailableHasOneTerminalResponse(t *testing.T) {
	for _, mode := range []string{"json", "stream_before_headers", "stream_after_keepalive"} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			groupID := int64(4220)
			cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 32 << 20}}
			accounts := []service.Account{{
				ID: 9920, Name: "unavailable-continuation-owner", Platform: service.PlatformOpenAI,
				Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: false, Concurrency: 1,
				Credentials: map[string]any{"api_key": "fixture-token"},
			}, {
				ID: 9921, Name: "available-unrelated-account", Platform: service.PlatformOpenAI,
				Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "other-fixture-token"},
			}}
			cache, _, _ := newWSStickyTestCache(t)
			responseKey := fmt.Sprintf("openai:response:%x", sha256.Sum256([]byte("resp_unavailable_owner")))
			require.NoError(t, cache.SetSessionAccountID(t.Context(), groupID, responseKey, 9920, time.Hour))
			upstream := &openAIHTTPPassthroughFailoverUpstream{}
			key := newNativeHandlerAPIKey()
			key.ID, key.UserID, key.User.ID = 1820, 1720, 1720
			key.GroupID, key.Group.ID = &groupID, groupID
			for i := range accounts {
				accounts[i].GroupIDs = []int64{groupID}
			}
			store := &nativeHandlerStore{key: key, accounts: accounts}
			accountRepo := &nativeHandlerAccounts{store: store}
			userRepo := &nativeHandlerUsers{store: store}
			keyRepo := &nativeHandlerKeys{store: store}
			transport := &nativeHandlerTransport{responder: upstream}
			concurrency := service.NewConcurrencyService(nil)
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			cleanupHandlerBillingCache(t, billing)
			require.NoError(t, billing.Start(t.Context()))
			gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, userRepo, nil, nil, cache.GatewayCache, cfg, nil, concurrency,
				service.NewBillingService(cfg, nil), nil, billing, transport, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			t.Cleanup(gateway.CloseOpenAIWSPool)
			h := NewOpenAIGatewayHandler(gateway, concurrency, billing,
				service.NewAPIKeyService(keyRepo, userRepo, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			// The caller owns this handle, but its bound API-key account cannot
			// serve it. The actual Responses handler must return a recovery signal
			// instead of switching this opaque parent to the other healthy account.
			require.NoError(t, gateway.BindOpenAIHTTPResponseOwner(t.Context(), groupID, "resp_unavailable_owner", 1720, 1820))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(
				`{"model":"gpt-5.1","stream":%t,"previous_response_id":"resp_unavailable_owner","input":"continue"}`, mode != "json")))
			c.Request.Header.Set("Content-Type", "application/json")
			ingressKey, keyErr := keyRepo.GetByID(t.Context(), key.ID)
			require.NoError(t, keyErr)
			c.Set(string(middleware.ContextKeyAPIKey), ingressKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1720})
			if mode == "stream_after_keepalive" {
				c.Header("Content-Type", "text/event-stream")
				_, err := c.Writer.WriteString(": keepalive\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
			}
			h.Responses(c)
			require.Zero(t, transport.prepared.Load(), "owner rejection precedes Native preparation")
			require.Zero(t, transport.opened.Load())
			require.Zero(t, transport.legacy.Load())
			require.Empty(t, upstream.calls(), "local context loss must not send or switch accounts")
			require.True(t, service.IsResponseCommitted(c), "the handler must recognize the already-written rejection: %s", recorder.Body.String())
			body := recorder.Body.String()
			require.Equal(t, 1, strings.Count(body, `"code":"previous_response_not_found"`), body)
			if mode == "stream_after_keepalive" {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, 1, strings.Count(body, `"type":"response.failed"`), body)
				for _, line := range strings.Split(body, "\n") {
					if strings.HasPrefix(line, "data: ") {
						require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))), line)
					}
				}
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.True(t, json.Valid(recorder.Body.Bytes()), "a handler fallback must not append SSE to JSON: %s", body)
				require.Equal(t, "previous_response_id", gjson.GetBytes(recorder.Body.Bytes(), "error.param").String())
				require.NotContains(t, body, "event:")
			}
			failure, ok := service.GetOpsStreamError(c)
			require.True(t, ok)
			require.Equal(t, http.StatusBadRequest, failure.IntendedStatus)
			require.False(t, failure.CountTowardsSLA)
			require.True(t, failure.RequestScoped)
		})
	}
}

func TestOpenAIContinuationErrorDoesNotAppendHandlerFallback(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streaming), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if streaming {
				c.Header("Content-Type", "text/event-stream")
				_, err := c.Writer.WriteString(": keepalive\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
			}
			before := c.Writer.Size()
			service.WriteOpenAIContinuationError(c)
			response := recorder.Body.String()
			h := &OpenAIGatewayHandler{}
			if !openAIForwardErrorAlreadyCommunicated(c, before, service.ErrOpenAIContinuationUnavailable) {
				require.False(t, h.ensureForwardErrorResponse(c, streaming))
			}
			require.Equal(t, response, recorder.Body.String(), "the ordinary Forward error tail must leave a structured continuation rejection unchanged")
			if streaming {
				require.Equal(t, 1, strings.Count(response, `"type":"response.failed"`))
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.True(t, json.Valid(recorder.Body.Bytes()), response)
			}
		})
	}
}
