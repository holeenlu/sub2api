package routes

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ingressWireReviewBody struct {
	io.Reader
	read int
}

func (b *ingressWireReviewBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (*ingressWireReviewBody) Close() error { return nil }

// This exercises RegisterGatewayRoutes and its actual middleware order. The
// real selected handler stops at its missing dependency guard, deliberately
// before its body read. Capture the body that this actual root would receive;
// no reconstructed middleware chain or direct body-helper invocation is used.
// It proves ingress transformation, not an upstream send or journal receipt.
func TestNativeIngressWireReviewRegisteredRouterPrereadEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []struct {
		name, platform     string
		allowlist, preread bool
	}{
		{"image_mark_only", service.PlatformOpenAI, false, false},
		{"allowlist", service.PlatformOpenAI, true, true},
		{"composite", service.PlatformComposite, false, true},
		{"allowlist_composite", service.PlatformComposite, true, true},
	} {
		for _, encoding := range []string{"identity", "gzip", "zstd"} {
			for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
				t.Run(mode.name+"/"+encoding+path, func(t *testing.T) {
					// BOM and a literal string newline expose normalization as
					// well as decompression. These are fixed local fixture bytes.
					payload := append([]byte{0xef, 0xbb, 0xbf}, []byte("{\"model\":\"gpt-5\",\"input\":\"a\nb\"}")...)
					wire := encodeGatewayBudgetBody(t, encoding, payload)
					normalized, err := pkghttputil.NormalizeLenientJSONRequestBody(payload, 4096)
					require.NoError(t, err)
					source := &ingressWireReviewBody{Reader: bytes.NewReader(wire)}
					groupID := int64(8821)
					key := &service.APIKey{GroupID: &groupID, Group: allowlistGroup(mode.platform, mode.allowlist, "gpt-5")}
					key.Group.ID = groupID
					router := gin.New()
					visited := false
					RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
						middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
							_, initiallyKnown := pkghttputil.RequestWireDigest(c.Request)
							require.False(t, initiallyKnown, "installing an edge observer is not proof of EOF")
							c.Set(string(middleware.ContextKeyAPIKey), key)
							c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
							c.Next()
							visited = true
							require.True(t, service.IsExcelBPSImageAdmissionMarked(c), "real image admission ran on this alias")
							body, wasPreread := c.Request.Body.(*pkghttputil.PrereadBody)
							require.Equal(t, mode.preread, wasPreread)
							if mode.preread {
								require.Equal(t, normalized, body.Bytes())
								require.Equal(t, len(wire), source.read, "the original body was consumed by actual admission")
								require.Empty(t, c.GetHeader("Content-Encoding"))
								require.NotEqual(t, sha256.Sum256(wire), sha256.Sum256(body.Bytes()), "the body now handed to Native root is no longer the original wire")
								digest, known := pkghttputil.RequestWireDigest(c.Request)
								require.True(t, known)
								require.Equal(t, sha256.Sum256(wire), digest, "preread retains the original edge observation")
							} else {
								require.Zero(t, source.read, "image marker alone must not preread")
								_, known := pkghttputil.RequestWireDigest(c.Request)
								require.False(t, known)
							}
							// Complete the actual common read at this probe for the
							// no-preread route; preread routes use its zero-copy branch.
							observedBody, readErr := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, 4096)
							require.NoError(t, readErr)
							require.Equal(t, normalized, observedBody)
							digest, known := pkghttputil.RequestWireDigest(c.Request)
							require.True(t, known)
							require.Equal(t, sha256.Sum256(wire), digest)
							require.NotEqual(t, sha256.Sum256(normalized), digest)
							require.Equal(t, len(wire), source.read)
							replacement := bytes.Replace(normalized, []byte("gpt-5"), []byte("gpt-4.1"), 1)
							requestmodel.ResetRequestBody(c.Request, replacement)
							repeated, readErr := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, 4096)
							require.NoError(t, readErr)
							require.Equal(t, replacement, repeated)
							afterReset, stillKnown := pkghttputil.RequestWireDigest(c.Request)
							require.True(t, stillKnown)
							require.Equal(t, digest, afterReset, "ResetBody must not hash replacement bytes")
							require.Equal(t, len(wire), source.read, "only one read of the original source")
						}), nil, nil, nil, nil, nil, &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 4096, TextMaxBodySize: 4096}})
					req := httptest.NewRequest(http.MethodPost, path, source)
					req.ContentLength = int64(len(wire))
					req.Header.Set("Content-Type", "application/json")
					if encoding != "identity" {
						req.Header.Set("Content-Encoding", encoding)
					}
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					require.True(t, visited)
					require.Equal(t, http.StatusServiceUnavailable, rec.Code, "the actual root must reach its dependency guard: %s", rec.Body.String())
				})
			}
		}
	}
}

type ingressWireReviewEOFBody struct{ data []byte }

func (b *ingressWireReviewEOFBody) Read(p []byte) (int, error) {
	n := copy(p, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}
func (*ingressWireReviewEOFBody) Close() error { return nil }

func TestNativeIngressWireReviewLimitErrorDoesNotCertifyInnerEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	visited := false
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
		middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			visited = true
			_, err := io.ReadAll(c.Request.Body)
			var limit *http.MaxBytesError
			require.ErrorAs(t, err, &limit)
			_, known := pkghttputil.RequestWireDigest(c.Request)
			require.False(t, known, "an inner n>0,EOF cannot certify an outer route-limit read failure")
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		}), nil, nil, nil, nil, nil, &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 4, TextMaxBodySize: 4}})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", &ingressWireReviewEOFBody{data: []byte("12345")})
	req.ContentLength = 5
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.True(t, visited)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
