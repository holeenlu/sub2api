//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 多行 data 事件：单独一行不是合法 JSON，必须先按规范拼接再判断，
// 否则暂存层与服务层对同一个事件的理解不一致。
func TestRetryStageHasContent_JoinsMultiLineData(t *testing.T) {
	multiline := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\n" +
		"data: \"message\":{\"usage\":{\"input_tokens\":11}}}\n\n")
	require.True(t, retryStageHasContent(multiline), "多行 message_start 携带 usage，应判为已有内容")

	// 对照：同一事件压成单行时本来就能判对，拼接不得改变该结论。
	singleline := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n")
	require.True(t, retryStageHasContent(singleline))
}

func TestRetryStageHasContent_DoesNotMergeAcrossEventBoundary(t *testing.T) {
	// 两个各自合法的单行事件不得被拼成一个非法值。
	p := []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n")
	require.True(t, retryStageHasContent(p))

	// 纯 ping 仍然判为无内容，不能因为拼接逻辑把它误判成有内容。
	require.False(t, retryStageHasContent([]byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")))
}

func TestRetryStageEventPayloads_Boundaries(t *testing.T) {
	payloads := retryStageEventPayloads([]byte(
		"event: a\ndata: 1\ndata: 2\n\nevent: b\ndata: 3\n\ndata: 4"))
	require.Equal(t, []string{"1\n2", "3", "4"}, func() []string {
		out := make([]string, 0, len(payloads))
		for _, p := range payloads {
			out = append(out, string(p))
		}
		return out
	}())
}

func TestRetryStageEventPayloads_MatchesServiceFields(t *testing.T) {
	for name, frame := range map[string]string{
		"indented":         " \tdata: 1\n data: 2\n\n",
		"whitespace_field": "data: 1\n \t\ndata: 2\n\n",
		"crlf":             "data: 1\r\ndata: 2\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, [][]byte{[]byte("1\n2")}, retryStageEventPayloads([]byte(frame)))
		})
	}
	require.Equal(t, [][]byte{[]byte("\nx")}, retryStageEventPayloads([]byte("data\ndata: x\n\n")))
}

func TestRetryOutputStage_MultilineSemanticOutputCannotBeDiscarded(t *testing.T) {
	for name, payload := range map[string]string{
		"text":            `{"type":"content_block_delta",` + "\n" + `"delta":{"type":"text_delta","text":"hello"}}`,
		"tool":            `{"type":"content_block_start",` + "\n" + `"content_block":{"type":"tool_use","id":"tool_1"}}`,
		"usage":           `{"type":"message_start",` + "\n" + `"message":{"usage":{"input_tokens":11}}}`,
		"cache_breakdown": `{"type":"message_start",` + "\n" + `"message":{"usage":{"cache_creation":{"ephemeral_1h_input_tokens":9}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			finish := beginRetryOutputStage(c)
			t.Cleanup(func() { _ = finish(nil) })
			c.Header("Content-Type", "text/event-stream")
			frame := " \tdata: " + strings.ReplaceAll(payload, "\n", "\n \tdata: ") + "\n\n"
			before := c.Writer.Size()
			// Exercise Write boundaries independently of SSE event boundaries.
			for _, part := range strings.SplitAfter(frame, "\n") {
				_, err := c.Writer.WriteString(part)
				require.NoError(t, err)
			}
			require.True(t, c.Writer.Written())
			failure := &service.UpstreamFailoverError{StatusCode: 529}
			require.Error(t, finish(failure))
			require.NotEqual(t, before, c.Writer.Size(), "the handler no-replay guard must fire")
			require.Equal(t, frame, rec.Body.String())
			_ = finish(failure)
			require.Equal(t, frame, rec.Body.String(), "finalization must not duplicate output")
		})
	}
}
