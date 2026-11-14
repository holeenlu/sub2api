//go:build unit

package handler

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

// The registered-router order has separate routes-package coverage. This test
// follows its real bounded/preread middleware through the actual three handlers,
// sealing, permit and finalizer to the existing in-memory journal port.
func TestNativeHTTPPrereadWireProofReachesActualJournal(t *testing.T) {
	for _, entry := range nativeHandlerEntries() {
		for _, encoding := range []string{"identity", "gzip", "zstd"} {
			t.Run(entry.name+"/"+encoding, func(t *testing.T) {
				raw := append([]byte{0xef, 0xbb, 0xbf}, []byte(" \n"+entry.body+" \n")...)
				wire := raw
				var out bytes.Buffer
				switch encoding {
				case "gzip":
					w := gzip.NewWriter(&out)
					_, err := w.Write(raw)
					require.NoError(t, err)
					require.NoError(t, w.Close())
					wire = out.Bytes()
				case "zstd":
					w, err := zstd.NewWriter(&out, zstd.WithEncoderConcurrency(1))
					require.NoError(t, err)
					_, err = w.Write(raw)
					require.NoError(t, err)
					require.NoError(t, w.Close())
					wire = out.Bytes()
				}
				logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
				journal := &wsNativeFixtureJournal{logSink: logs}
				cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}}
				cfg.Default.RateMultiplier = 1
				accounts := nativeHandlerFixtureAccounts(0)
				accounts[0].Type = service.AccountTypeAPIKey
				accounts[0].Credentials = map[string]any{"api_key": "wire-fixture-key", "base_url": "https://api.openai.com"}
				upstream := &nativeHandlerSuccessUpstream{}
				h, store, transport := newConfiguredNativeHTTPHandlerFixture(t, accounts, upstream, cfg, service.NewBillingService(cfg, nil), journal)
				store.update(func(s *nativeHandlerStore) {
					s.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"gpt-5.1"}}
				})
				router := gin.New()
				router.Use(middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize), middleware.ClientRequestID())
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), newWSNativeFixtureLogger(t)))
					key, err := (&nativeHandlerKeys{store: store}).GetByID(c.Request.Context(), 99)
					require.NoError(t, err)
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
					c.Next()
				})
				router.Use(middleware.GroupModelAllowlistWithBodyLimit(cfg.Gateway.MaxBodySize))
				router.POST(entry.path, func(c *gin.Context) {
					_, preread := c.Request.Body.(*httputil.PrereadBody)
					require.True(t, preread, "actual allowlist middleware must read/decode before the handler")
					entry.call(h, c)
				})
				req := httptest.NewRequest(http.MethodPost, entry.path, bytes.NewReader(wire))
				req.Header.Set("Content-Type", "application/json")
				if encoding != "identity" {
					req.Header.Set("Content-Encoding", encoding)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.EqualValues(t, 1, upstream.calls.Load())
				require.EqualValues(t, 1, transport.opened.Load())
				ids := journal.identities()
				require.Len(t, ids, 1)
				saved, err := journal.LoadNativeUsageEvidence(t.Context(), ids[0])
				require.NoError(t, err)
				require.Equal(t, sha256.Sum256(wire), saved.Evidence.OriginalWireDigest)
				require.NotEqual(t, sha256.Sum256([]byte(entry.body)), saved.Evidence.OriginalWireDigest)
				require.NotNil(t, saved.SettlementUsage)
				require.Len(t, logs.created, 1)
				journal.assertUsageLogs(t, []*service.UsageLog{<-logs.created})
				journal.assertNoLegacyApply(t)
			})
		}
	}
}
