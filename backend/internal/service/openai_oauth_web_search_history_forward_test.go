//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestForward_OAuthWebSearchHistoryDeclaresTool drives the Codex compaction
// shape (web_search_call history, tools:[]) through Forward and asserts the
// body reaching chatgpt.com declares web_search (#7927): top-level for the
// standard wire, input additional_tools for Responses Lite.
func TestForward_OAuthWebSearchHistoryDeclaresTool(t *testing.T) {
	for _, tc := range []struct {
		name        string
		passthrough bool
		lite        bool
		standalone  bool
	}{
		{"transform", false, false, false},
		{"passthrough", true, false, false},
		{"transform lite", false, true, false},
		{"passthrough lite", true, true, false},
		{"transform standalone", false, false, true},
		{"passthrough standalone", true, false, true},
		{"transform lite standalone", false, true, true},
		{"passthrough lite standalone", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newAstraOAuthSetup(t, tc.passthrough)
			if tc.lite {
				s.c.Request.Header.Set(responsesLiteHeader, "true")
			}
			inner := `{"id":"resp_test","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
			if tc.standalone {
				inner = strings.ReplaceAll(inner, "gpt-5.5", "gpt-6-luna")
			}
			s.upstream.resp = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(codexCompletedSSE(inner))),
			}
			body, err := sjson.SetRawBytes([]byte(openAIWebSearchHistoryCompactionBody), "input.-1", []byte(`{"type":"compaction_trigger"}`))
			require.NoError(t, err)
			const standaloneTools = `[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]}]`
			if tc.standalone {
				body, err = sjson.SetRawBytes(body, "tools", []byte(standaloneTools))
				require.NoError(t, err)
				body, err = sjson.SetBytes(body, "model", "gpt-6-luna")
				require.NoError(t, err)
			}

			result, err := s.svc.Forward(context.Background(), s.c, s.account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, s.upstream.lastReq)
			require.Equal(t, astraProCodexResponsesURL, s.upstream.lastReq.URL.String())

			forwarded := s.upstream.lastBody
			require.Equal(t, "web_search_call", gjson.GetBytes(forwarded, "input.1.type").String())
			items := gjson.GetBytes(forwarded, "input").Array()
			require.Equal(t, "compaction_trigger", items[len(items)-1].Get("type").String())
			if tc.standalone {
				if tc.lite {
					// Lite moves namespaces to its existing additional_tools carrier.
					require.False(t, gjson.GetBytes(forwarded, "tools").Exists())
					require.Len(t, items, 6)
					require.JSONEq(t, standaloneTools, items[len(items)-2].Get("tools").Raw)
				} else {
					require.JSONEq(t, standaloneTools, gjson.GetBytes(forwarded, "tools").Raw)
					require.Len(t, items, 5, "do not inject hosted tools into replayed history")
				}
				require.Equal(t, "auto", gjson.GetBytes(forwarded, "tool_choice").String())
				return
			}
			require.Equal(t, "none", gjson.GetBytes(forwarded, "tool_choice").String())
			if !tc.lite {
				tools := gjson.GetBytes(forwarded, "tools").Array()
				require.Len(t, tools, 1)
				require.Equal(t, "web_search", tools[0].Get("type").String())
				return
			}
			require.False(t, gjsonToolsContainWebSearch(gjson.GetBytes(forwarded, "tools"), ""))
			additional := items[len(items)-2]
			require.Equal(t, "additional_tools", additional.Get("type").String())
			require.Equal(t, "web_search", additional.Get("tools.0.type").String())
		})
	}
}
