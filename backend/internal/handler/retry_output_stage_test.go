package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetryOutputStageDiscardsRejectedIdentity(t *testing.T) {
	for _, metadata := range []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"failed_identity\"}}\n\n",
		"data: {\"id\":\"failed_identity\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n",
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		finish := beginRetryOutputStage(c)
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString(metadata)
		require.NoError(t, err)
		c.Writer.Flush()
		require.False(t, c.Writer.Written())
		failure := &service.UpstreamFailoverError{StatusCode: 529}
		require.Same(t, failure, finish(failure))
		require.Empty(t, rec.Body.String())
		finish = beginRetryOutputStage(c)
		_, err = c.Writer.WriteString("data: {\"id\":\"success_identity\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		require.NoError(t, err)
		require.NoError(t, finish(nil))
		require.NotContains(t, rec.Body.String(), "failed_identity")
		require.Contains(t, rec.Body.String(), "success_identity")
	}
}

func TestRetryOutputStageCommitBoundaries(t *testing.T) {
	for _, kind := range []string{"tool", "usage", "time", "bytes", "json"} {
		t.Run(kind, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			finish := beginRetryOutputStage(c)
			c.Header("Content-Type", "text/event-stream")
			switch kind {
			case "usage":
				_, _ = c.Writer.WriteString("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n")
			case "tool":
				_, _ = c.Writer.WriteString("data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\"}}\n\n")
			case "time":
				_, _ = c.Writer.WriteString("data: {\"type\":\"message_start\"}\n\n")
				require.Eventually(t, c.Writer.Written, retryMetadataHold+time.Second, 10*time.Millisecond)
			case "bytes":
				_, _ = c.Writer.WriteString(":" + strings.Repeat(" ", retryMetadataLimit))
			case "json":
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			}
			require.True(t, c.Writer.Written())
			require.Error(t, finish(&service.UpstreamFailoverError{StatusCode: 503}))
			require.NotEmpty(t, rec.Body.String(), "committed content cannot be rolled back for failover")
		})
	}
}
