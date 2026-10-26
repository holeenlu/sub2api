//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResponsesStrictSelectedAccount(t *testing.T) {
	body := []byte(`{"model":"gpt-6-luna","input":"x","store":true,"max_output_tokens":42,"text":{"format":{"type":"json_schema","schema":{"type":"array","uniqueItems":true,"items":{"type":"integer"}}}}}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	native := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Nil(t, svc.strictResponsesError(c, native, body))
	c.Request.Header.Set("X-Codex2API-Strict-Compatibility", "true")
	rejected := svc.strictResponsesError(c, native, body)
	require.NotNil(t, rejected)
	require.Equal(t, []string{"max_output_tokens", "store", "text.format.schema.uniqueItems"}, rejected.Fields)
	require.Equal(t, "unsupported_feature", gjson.GetBytes(rejected.websocketEvent(), "error.code").String())
	require.Equal(t, int64(400), gjson.GetBytes(rejected.websocketEvent(), "status").Int())
	require.Nil(t, svc.strictResponsesError(c, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body))
	require.Nil(t, svc.strictResponsesError(c, native, []byte(`{"model":"gpt-6-luna","input":"x","store":false,"metadata":null}`)))
	aliasLoss := svc.strictResponsesError(c, native, []byte(`{"model":"gpt-6-luna","input":"x","reasoning_effort":"minimal"}`))
	require.NotNil(t, aliasLoss)
	require.Contains(t, aliasLoss.Fields, "reasoning.effort")

	require.ErrorIs(t, rejected, ErrOpenAIWSLocalClientPolicy)
	limit := []byte(`{"model":"gpt-6-luna","max_output_tokens":42}`)
	require.Nil(t, svc.strictResponsesError(c, native, limit, OpenAIWSIngressModePassthrough))
	require.Nil(t, svc.strictResponsesError(c, native, limit, OpenAIWSIngressModeCtxPool))
	minimal := []byte(`{"model":"gpt-6-luna","reasoning":{"effort":"minimal"}}`)
	require.Nil(t, svc.strictResponsesError(c, native, minimal, OpenAIWSIngressModeCtxPool))

}

func TestResponsesStrictHTTPAndWebSocketFailBeforeDispatch(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"gpt-6-luna","input":"x","store":true}`)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("X-Codex2API-Strict-Compatibility", "true")
	_, err := svc.forwardOpenAIResponses(c.Request.Context(), c, account, body, body, "", time.Now())
	require.Error(t, err)
	require.Equal(t, 400, recorder.Code)
	require.Equal(t, "unsupported_feature", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	conn := newQueuedWSDownstreamTestConn()
	conn.writes = make(chan []byte, 2)
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	err = svc.ProxyResponsesWebSocketFromClient(context.Background(), c, ds, account, "test-token", body, nil)
	require.Error(t, err)
	select {
	case event := <-conn.writes:
		require.Equal(t, "unsupported_feature", gjson.GetBytes(event, "error.code").String())
	case <-time.After(time.Second):
		t.Fatal("missing WS strict rejection")
	}
}
