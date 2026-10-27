package service

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCloudOversizedHistoryRejectsBeforeCreatingUpstreamThread(t *testing.T) {
	svc, input, store, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var threads atomic.Int32
	served := make(chan struct{})
	go func() {
		defer close(served)
		for {
			select {
			case raw := <-wire.writes:
				var msg cloudRPCMessage
				if json.Unmarshal(raw, &msg) != nil {
					return
				}
				switch msg.Method {
				case "initialize":
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				case "thread/start":
					threads.Add(1)
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
				case "thread/archive":
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { cancel(); <-served }()
	requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, input)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"model": "client-model", "input": []any{map[string]any{"role": "user", "content": strings.Repeat("x", cloudInjectionMaxBytes+2048)}, map[string]any{"role": "user", "content": "Continue"}}})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(requestCtx)
	c.Set("api_key", input.APIKey)
	SetOpenAIHTTPResponseOwner(c, input.User.ID, input.APIKey.ID)
	_, err = svc.forwardOpenAICloud(requestCtx, c, input.Account, body)
	require.Equal(t, 413, rec.Code)
	require.Zero(t, threads.Load(), "known unencodable history must not create an upstream thread")
	require.Empty(t, store.snapshot().ThreadID)
}
