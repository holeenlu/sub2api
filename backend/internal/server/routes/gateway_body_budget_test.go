package routes

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func encodeGatewayBudgetBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	switch encoding {
	case "gzip":
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, err := w.Write(body)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return buf.Bytes()
	case "zstd":
		w, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(1024))
		require.NoError(t, err)
		defer w.Close()
		return w.EncodeAll(body, nil)
	default:
		return body
	}
}

func readGatewayBudgetResponse(c *gin.Context) {
	_, err := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, 4096)
	if err != nil {
		status, _, _ := pkghttputil.DescribeRequestBodyReadFailure(c.Request, err).Response()
		c.AbortWithStatus(status)
		return
	}
	c.AbortWithStatus(http.StatusNoContent)
}

// Inspect at the first authentication hook of the real registered routes. The
// route budget must already exist before allowlist/composite preread can run.
func TestGatewayRoutesBodyBudgetBeforeAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterGatewayRoutes(router, &handler.Handlers{
		Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{},
		AsyncImage: handler.NewAsyncImageHandler(nil, nil),
	}, middleware.APIKeyAuthMiddleware(readGatewayBudgetResponse), nil, nil, nil, nil, nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 4096, TextMaxBodySize: 512}})
	payload := []byte("{\"input\":\"" + strings.Repeat("a", 700) + "\"}")
	wire := encodeGatewayBudgetBody(t, "gzip", payload)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/v1/systemone", 413},
		{http.MethodPost, "/v1/alpha/search", 413},
		{http.MethodPost, "/alpha/search", 413},
		{http.MethodPost, "/backend-api/codex/alpha/search", 413},
		{http.MethodPost, "/v1/embeddings", 413},
		{http.MethodPost, "/embeddings", 413},
		{http.MethodPost, "/v1/responses", 204},
		{http.MethodPost, "/images/generations", 204},
		{http.MethodGet, "/models/embeddings", 204},
		{http.MethodPost, "/responses/embeddings", 204},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(wire))
			req.Header.Set("Content-Encoding", "gzip")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}

func TestGatewayBodyBudgetAcrossPrereadModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const prefix = "{\"model\":\"gpt-5\",\"input\":\""
	const suffix = "\"}"
	exact := prefix + strings.Repeat("a", 512-len(prefix)-len(suffix)) + suffix
	for _, mode := range []struct {
		name, platform string
		allowlist      bool
	}{
		{"handler", service.PlatformOpenAI, false},
		{"allowlist", service.PlatformOpenAI, true},
		{"composite", service.PlatformComposite, false},
		{"allowlist_composite", service.PlatformComposite, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			router := gin.New()
			router.Use(gatewayRequestBodyLimit(4096, 512), func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
					Group: allowlistGroup(mode.platform, mode.allowlist, "gpt-5"),
				})
				c.Next()
			}, middleware.GroupModelAllowlistWithBodyLimit(4096), compositeTargetPlatformMiddleware(nil, 4096))
			router.POST("/v1/embeddings", readGatewayBudgetResponse)
			for _, encoding := range []string{"identity", "gzip", "zstd"} {
				for _, tc := range []struct {
					name, body string
					status     int
				}{
					{"boundary", exact, 204},
					{"overflow", exact + " ", 413},
					{"normalization_overflow", prefix + strings.Repeat("\n", 256) + suffix, 413},
				} {
					t.Run(encoding+"/"+tc.name, func(t *testing.T) {
						req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(encodeGatewayBudgetBody(t, encoding, []byte(tc.body))))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Content-Encoding", encoding)
						rec := httptest.NewRecorder()
						router.ServeHTTP(rec, req)
						require.Equal(t, tc.status, rec.Code, rec.Body.String())
					})
				}
			}
		})
	}
}
