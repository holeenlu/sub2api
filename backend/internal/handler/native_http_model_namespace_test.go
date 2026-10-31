//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativePublicModelUpstream struct {
	nativeHandlerSuccessUpstream
	receivedModel string
}

func (u *nativePublicModelUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.receivedModel = gjson.GetBytes(body, "model").String()
	return u.nativeHandlerSuccessUpstream.Do(req, proxy, accountID, concurrency)
}

// Composite routing rewrites the working body before the HTTP handler. The
// fresh authority must still check the public model originally admitted.
func TestNativeHTTPHandlerAuthorizationKeepsOriginalPublicModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range nativeHandlerEntries() {
		for _, revoke := range []bool{false, true} {
			name := entry.name + "/allowed"
			if revoke {
				name = entry.name + "/revoked_before_dispatch"
			}
			t.Run(name, func(t *testing.T) {
				upstream := &nativePublicModelUpstream{}
				// An API-key account keeps this test independent of the separate
				// Codex OAuth model-alias normalization contract.
				accounts := nativeHandlerFixtureAccounts(0)
				accounts[0].Type = service.AccountTypeAPIKey
				accounts[0].Credentials = map[string]any{"api_key": "namespace-fixture-key", "base_url": "https://api.openai.com"}
				h, store, transport := newNativeHTTPHandlerFixture(t, accounts, upstream)
				store.update(func(s *nativeHandlerStore) {
					s.key.Group.Platform = service.PlatformComposite
					s.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"customer-alias"}}
					if revoke {
						s.key.Group.ModelAllowlist.Models = append(s.key.Group.ModelAllowlist.Models, "gpt-5.1")
					}
				})
				ctx := service.WithCompositeRouteDecision(t.Context(), service.CompositeRouteDecision{
					Matched: true, GroupID: nativeHandlerFixtureGroupID, PublicModel: "customer-alias",
					UpstreamModel: "gpt-5.1", TargetPlatform: service.PlatformOpenAI, Source: "fixture-resolver",
				})
				c, rec := nativeEntryContext(t, ctx, entry, 0)
				key, err := (&nativeHandlerKeys{store: store}).GetByID(ctx, 99)
				require.NoError(t, err)
				c.Set(string(middleware.ContextKeyAPIKey), key)
				var revoked atomic.Bool
				store.beforeKeyRead = func(_ context.Context, read int32) {
					if revoke && read == 3 {
						store.update(func(s *nativeHandlerStore) {
							// The wire name remains allowed, but the admitted alias
							// no longer is. They are different policy identities.
							s.key.Group.ModelAllowlist.Models = []string{"gpt-5.1"}
						})
						revoked.Store(true)
					}
				}
				entry.call(h, c)
				if revoke {
					require.True(t, revoked.Load())
					require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
					require.Zero(t, transport.opened.Load())
					require.Zero(t, upstream.calls.Load())
				} else {
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					require.EqualValues(t, 1, transport.opened.Load())
					require.EqualValues(t, 1, upstream.calls.Load())
					require.Equal(t, "gpt-5.1", upstream.receivedModel)
				}
			})
		}
	}
}
