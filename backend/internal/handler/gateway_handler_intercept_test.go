package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDetectInterceptType_MaxTokensOneRequiresClaudeCodeClient(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	for _, model := range []string{"claude-haiku-4-5", "claude-sonnet-4-5", "claude-opus-4-6"} {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, InterceptTypeNone, detectInterceptType(body, model, 1, false))
			require.Equal(t, InterceptTypeMaxTokensOne, detectInterceptType(body, model, 1, true))
			require.Equal(t, InterceptTypeNone, detectInterceptType(body, model, 2, true))
		})
	}
}

func TestDetectInterceptType_SuggestionModeUnaffected(t *testing.T) {
	body := []byte(`{
		"messages":[{
			"role":"user",
			"content":[{"type":"text","text":"[SUGGESTION MODE:foo]"}]
		}],
		"system":[]
	}`)

	got := detectInterceptType(body, "claude-sonnet-4-5", 256, false)
	require.Equal(t, InterceptTypeSuggestionMode, got)
}

func TestSendMockInterceptResponse_MaxTokensOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	sendMockInterceptResponse(ctx, "claude-sonnet-4-5", InterceptTypeMaxTokensOne)

	require.Equal(t, http.StatusOK, rec.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "max_tokens", response["stop_reason"])

	id, ok := response["id"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(id, "msg_01"))

	content, ok := response["content"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, content)

	firstBlock, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "#", firstBlock["text"])

	usage, ok := response["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), usage["output_tokens"])
}

func TestSendMockInterceptStream_MaxTokensOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	sendMockInterceptStream(c, "claude-opus-4-6", InterceptTypeMaxTokensOne)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	var text string
	var types []string
	for _, frame := range strings.Split(strings.TrimSuffix(rec.Body.String(), "\n\n"), "\n\n") {
		_, data, ok := strings.Cut(frame, "\ndata: ")
		require.True(t, ok)
		require.True(t, gjson.Valid(data))
		types = append(types, gjson.Get(data, "type").String())
		text += gjson.Get(data, "delta.text").String()
		if gjson.Get(data, "type").String() == "message_delta" {
			require.Equal(t, "max_tokens", gjson.Get(data, "delta.stop_reason").String())
			require.Equal(t, int64(1), gjson.Get(data, "usage.output_tokens").Int())
		}
	}
	require.Equal(t, "#", text)
	require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}, types)
}
