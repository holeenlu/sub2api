package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

type failedIngressBody struct{ err error }

func (b failedIngressBody) Read([]byte) (int, error) { return 0, b.err }
func (b failedIngressBody) Close() error             { return nil }

func TestPrereadMiddlewareClassifiesBodyReadFailures(t *testing.T) {
	for _, stage := range []string{"allowlist", "image admission"} {
		for _, tc := range []struct {
			name    string
			err     error
			status  int
			message string
			skip    bool
		}{
			{"cancel", context.Canceled, 499, "Client closed the connection", true},
			{"h2 cancel", http2.StreamError{Code: http2.ErrCodeCancel}, 499, "Client closed the connection", true},
			{"timeout", os.ErrDeadlineExceeded, http.StatusRequestTimeout, "Timed out while reading", false},
			{"truncated", io.ErrUnexpectedEOF, http.StatusBadRequest, "Request body ended prematurely", false},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				r := gin.New()
				var called, skip bool
				r.Use(func(c *gin.Context) {
					key := allowlistAPIKey(true, "test")
					key.Group.Platform = service.PlatformOpenAI
					c.Set(string(ContextKeyAPIKey), key)
					c.Next()
					skip = ShouldSkipOpsErrorRecord(c)
				})
				if stage == "allowlist" {
					r.Use(GroupModelAllowlist())
				} else {
					r.Use(ExcelBPSImageAdmission(bpsImageTestSettings{enabled: true}, 256<<20))
				}
				r.POST("/v1/responses", func(c *gin.Context) { called = true })
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", failedIngressBody{tc.err})
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, tc.status, rec.Code)
				require.Contains(t, rec.Body.String(), tc.message)
				require.False(t, called)
				require.Equal(t, tc.skip, skip)
			})
		}
	}
}
