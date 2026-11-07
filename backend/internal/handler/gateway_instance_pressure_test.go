package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayOverloadProtocolsAndCatalog(t *testing.T) {
	for _, test := range []struct{ path, expected string }{{"/v1/messages", "overloaded_error"}, {"/v1/responses", "gateway_overloaded"}, {"/v1beta/models/gemini:streamGenerateContent", "UNAVAILABLE"}} {
		t.Run(test.path, func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			require.False(t, rejectGatewayInstanceOverload(c, false), "observe-only replay must not reject")
			require.True(t, rejectGatewayInstanceOverload(c, true))
			require.Equal(t, 503, writer.Code)
			require.Equal(t, "5", writer.Header().Get("Retry-After"))
			require.Contains(t, writer.Body.String(), test.expected)
		})
	}
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	require.False(t, rejectGatewayInstanceOverload(c, true), "read-only catalog remains reachable")
}

func TestGatewayOverloadKeepsStartedStreamAndRejectsNewLoad(t *testing.T) {
	var overloaded atomic.Bool
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if rejectGatewayInstanceOverload(c, overloaded.Load()) {
			return
		}
		c.Next()
	})
	router.POST("/v1/responses", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: started\n\n")
		c.Writer.Flush()
		<-release
		_, _ = c.Writer.WriteString("data: completed\n\n")
	})
	server := httptest.NewServer(router)
	defer func() { finish(); server.Close() }()
	response, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, 200, response.StatusCode)
	overloaded.Store(true)
	results := make(chan int, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			r, requestErr := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader("{}"))
			if requestErr != nil {
				results <- 0
				return
			}
			defer func() { _ = r.Body.Close() }()
			_, _ = io.Copy(io.Discard, r.Body)
			results <- r.StatusCode
		})
	}
	wg.Wait()
	close(results)
	for status := range results {
		require.Equal(t, 503, status)
	}
	finish()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "data: completed")
}
