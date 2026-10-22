package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const siwcCatalogFixture = `{"version":"catalog-v1","models":[{"slug":"z-model","visibility":"list","display_name":"First","context_window":98765,"supported_reasoning_levels":[{"effort":"high"}],"input_modalities":["text","image"]},{"slug":"hidden-model","visibility":"hide"},{"slug":"a-model","visibility":"list","display_name":"Second","custom":{"future":true}},{"slug":"unspecified-model"}]}`

func newSIWCModelsTestService(t *testing.T, upstream HTTPUpstream) (*OpenAIGatewayService, *OpenAIOAuthService, *siwcTestClient, *Account) {
	t.Helper()
	oauth, _, client, account := siwcFixture(t)
	account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 3
	authorizeSIWC(t, oauth, account)
	service := newCodexModelsAPIKeyTestService(upstream)
	service.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
	return service, oauth, client, account
}

func TestSIWCModelsCatalogUsesIndependentBearerProxyAndSharedRepresentations(t *testing.T) {
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
		calls.Add(1)
		assert.Equal(t, "https://api.openai.com/v1/models", req.URL.String())
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, req.Header.Get("Authorization"))
		assert.Equal(t, "application/json", req.Header.Get("Accept"))
		assert.Equal(t, "http://proxy.example:7890", proxy)
		assert.EqualValues(t, 70, id)
		assert.Equal(t, 3, concurrency)
		for _, header := range []string{"Cookie", "ChatGPT-Account-ID", "Originator", "Version", "X-Admin-Override"} {
			assert.Empty(t, req.Header.Get(header))
		}
		return ordinaryModelsUpstreamResponse(siwcCatalogFixture), nil
	}}
	s, _, _, account := newSIWCModelsTestService(t, upstream)
	proxyID := int64(8)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 7890}
	account.Credentials["header_overrides"] = map[string]any{"Authorization": "Bearer native-override", "X-Admin-Override": "not-for-siwc"}
	account.Credentials["header_override_enabled"] = true
	account.Credentials["base_url"] = "https://not-the-official-host.example/v1"
	catalog, err := s.FetchCodexModelsManifest(t.Context(), account, "ignored-native-version", "")
	require.NoError(t, err)
	var full struct {
		Version string           `json:"version"`
		Models  []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(catalog.Body, &full))
	require.Equal(t, "catalog-v1", full.Version)
	require.Len(t, full.Models, 2)
	require.Equal(t, "z-model", full.Models[0]["slug"])
	require.Equal(t, "a-model", full.Models[1]["slug"])
	require.EqualValues(t, 98765, full.Models[0]["context_window"])
	require.NotEmpty(t, full.Models[0]["supported_reasoning_levels"])
	require.Equal(t, map[string]any{"future": true}, full.Models[1]["custom"])
	public, err := s.FetchOpenAIModelsList(t.Context(), account)
	require.NoError(t, err)
	_, models, err := modelCatalogEntries(public.Body, "data")
	require.NoError(t, err)
	require.Len(t, models, 2)
	require.Contains(t, string(models[0]), `"id":"z-model"`)
	require.Contains(t, string(models[0]), `"display_name":"First"`)
	require.Contains(t, string(models[1]), `"id":"a-model"`)
	require.EqualValues(t, 1, calls.Load(), "both surfaces share one grant-scoped catalog fetch")
	catalog.Body[0] = '!'
	again, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, byte('{'), again.Body[0], "callers cannot modify the cached catalog")
}

func TestSIWCModelsCacheRevalidationNeverServesStaleOnErrors(t *testing.T) {
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		response := ordinaryModelsUpstreamResponse(siwcCatalogFixture)
		response.Header.Set("ETag", `"upstream-etag"`)
		switch calls.Add(1) {
		case 1:
			assert.Empty(t, req.Header.Get("If-None-Match"))
		case 2:
			assert.Equal(t, `"upstream-etag"`, req.Header.Get("If-None-Match"))
			response.StatusCode = http.StatusNotModified
		case 3:
			response.StatusCode = http.StatusServiceUnavailable
		default:
			response.Body = io.NopCloser(strings.NewReader(`{"models":[{"slug":"updated","visibility":"list"}]}`))
		}
		return response, nil
	}}
	s, _, _, account := newSIWCModelsTestService(t, upstream)
	first, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	expireCodexModelsManifestCache(s, 2*time.Minute)
	revalidated, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, first, revalidated)
	require.EqualValues(t, 2, calls.Load(), "stale callers wait for conditional revalidation")
	expireCodexModelsManifestCache(s, 2*time.Minute)
	failed, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.Error(t, err)
	require.Nil(t, failed)
	require.EqualValues(t, 3, calls.Load())
	updated, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	require.Contains(t, string(updated.Body), "updated")
	require.EqualValues(t, 4, calls.Load())
}

func TestSIWCModelsStaleRequestsShareSynchronousRefresh(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	s, _, _, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if calls.Add(1) == 2 {
			close(started)
			select {
			case <-release:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		return ordinaryModelsUpstreamResponse(siwcCatalogFixture), nil
	}})
	_, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	expireCodexModelsManifestCache(s, 2*time.Minute)
	results := make(chan error, 8)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	for range 8 {
		go func() { _, err := s.fetchSIWCModelsCatalog(t.Context(), account); results <- err }()
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("catalog refresh did not start")
	}
	select {
	case <-results:
		t.Fatal("stale catalog was returned while revalidation was pending")
	case <-time.After(20 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for range 8 {
		require.NoError(t, <-results)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestSIWCModels401RejectsOnlySIWCGrantAndEvictsCatalog(t *testing.T) {
	var calls atomic.Int32
	s, oauth, client, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		response := ordinaryModelsUpstreamResponse(siwcCatalogFixture)
		if calls.Add(1) > 1 {
			response.StatusCode = http.StatusUnauthorized
		}
		return response, nil
	}})
	primary := siwcHash(account.Credentials)
	_, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.NoError(t, err)
	expireCodexModelsManifestCache(s, 2*time.Minute)
	response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.Error(t, err)
	require.Nil(t, response)
	require.Equal(t, http.StatusUnauthorized, infraerrors.Code(err))
	_, _, _, grant, err := oauth.readSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.True(t, grant.Rejected)
	require.Equal(t, primary, siwcHash(account.Credentials))
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Empty(t, s.openAIModelsCache.entries)
	client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		return nil, nil, &openai.SIWCOAuthError{Code: "invalid_grant"}
	}
	response, err = s.fetchSIWCModelsCatalog(t.Context(), account)
	require.Error(t, err)
	require.Nil(t, response)
	require.EqualValues(t, 2, calls.Load(), "invalid SIWC grants never fall back to native credentials")
}

func TestSIWCModelsFreshCacheRequiresCurrentGrant(t *testing.T) {
	for _, change := range []string{"revoked", "expired", "permission_removed", "reconnected"} {
		t.Run(change, func(t *testing.T) {
			var calls atomic.Int32
			s, oauth, client, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls.Add(1)
				return ordinaryModelsUpstreamResponse(siwcCatalogFixture), nil
			}})
			_, err := s.fetchSIWCModelsCatalog(t.Context(), account)
			require.NoError(t, err)
			registration, oldGrant, r, g, err := oauth.readSIWCRecord(t.Context(), account)
			require.NoError(t, err)
			switch change {
			case "revoked":
				g = nil
			case "expired":
				g.ExpiresAt = time.Now().Unix() - 1
				client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
					return nil, nil, errors.New("refresh unavailable")
				}
			case "permission_removed":
				g.Scope = "openid profile email"
			case "reconnected":
				r.Generation = "new-authorization-with-same-token"
			}
			_, _, err = oauth.saveSIWCRecord(t.Context(), account, registration, oldGrant, r, g)
			require.NoError(t, err)
			response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
			if change == "reconnected" {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.EqualValues(t, 2, calls.Load(), "new authorization never reuses the previous registration's cache")
			} else {
				require.Error(t, err)
				require.Nil(t, response)
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

func TestSIWCModelsRejectsCold304NilAndInvalidCatalogs(t *testing.T) {
	for _, scenario := range []string{"cold304", "nil_response", "nil_body", "transport", `{}`, `{"models":null}`, `{"models":[null]}`, `{"models":[{}]}`, `{"models":[{"slug":"a"},{"slug":"a"}]}`, `{"models":[{"slug":"bad*","visibility":"list"}]}`, `{"models":[],"error":{"message":"denied"}}`} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				switch scenario {
				case "cold304":
					return &http.Response{StatusCode: http.StatusNotModified}, nil
				case "nil_response":
					return nil, nil
				case "nil_body":
					return &http.Response{StatusCode: http.StatusOK}, nil
				case "transport":
					return nil, errors.New("connection failed")
				default:
					return ordinaryModelsUpstreamResponse(scenario), nil
				}
			}})
			response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
			require.Error(t, err)
			require.Nil(t, response)
			require.Empty(t, s.openAIModelsCache.entries)
		})
	}
	empty, err := filterSIWCModelCatalog([]byte(`{"models":[]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[]}`, string(empty))
}

func TestSIWCModelsRejectsMissingAuthorizationAndProxyWithoutNativeFallback(t *testing.T) {
	for _, scenario := range []string{"missing_service", "missing_grant", "disabled", "missing_proxy"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls.Add(1)
				return ordinaryModelsUpstreamResponse(siwcCatalogFixture), nil
			}}
			oauth, _, _, account := siwcFixture(t)
			s := newCodexModelsAPIKeyTestService(upstream)
			if scenario != "missing_service" {
				s.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
			}
			if scenario != "missing_grant" {
				authorizeSIWC(t, oauth, account)
			}
			if scenario == "disabled" {
				account.Extra[OpenAISIWCEnabledKey] = false
			}
			if scenario == "missing_proxy" {
				id := int64(12)
				account.ProxyID = &id
			}
			response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
			require.Error(t, err)
			require.Nil(t, response)
			require.Zero(t, calls.Load())
			require.Equal(t, "native-secret", account.Credentials["access_token"])
		})
	}
	var nilGateway *OpenAIGatewayService
	response, err := nilGateway.fetchSIWCModelsCatalog(t.Context(), nil)
	require.Error(t, err)
	require.Nil(t, response)
}

func TestSIWCModelsRevocationDuringFetchCannotPopulateCache(t *testing.T) {
	var invalidate func()
	s, oauth, _, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		invalidate()
		return ordinaryModelsUpstreamResponse(siwcCatalogFixture), nil
	}})
	invalidate = func() {
		registration, oldGrant, r, _, err := oauth.readSIWCRecord(t.Context(), account)
		require.NoError(t, err)
		_, _, err = oauth.saveSIWCRecord(t.Context(), account, registration, oldGrant, r, nil)
		require.NoError(t, err)
	}
	response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
	require.Error(t, err)
	require.Nil(t, response)
	require.Empty(t, s.openAIModelsCache.entries)
}

func TestSIWCModelsPreservesPermissionRateLimitAndServiceErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, oauth, _, account := newSIWCModelsTestService(t, &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private upstream body"))}, nil
			}})
			response, err := s.fetchSIWCModelsCatalog(t.Context(), account)
			require.Error(t, err)
			require.Nil(t, response)
			require.Equal(t, status, infraerrors.Code(err))
			require.NotContains(t, err.Error(), "private upstream body")
			_, _, _, grant, err := oauth.readSIWCRecord(t.Context(), account)
			require.NoError(t, err)
			require.False(t, grant.Rejected)
		})
	}
}

func TestSIWCInferenceGrantRechecksExactDurableSnapshotWithoutRefreshing(t *testing.T) {
	for _, change := range []string{"token_rotated", "rejected", "expired", "scope_removed", "generation_changed", "revoked"} {
		t.Run(change, func(t *testing.T) {
			oauth, _, client, account := siwcFixture(t)
			authorizeSIWC(t, oauth, account)
			token, owner, err := oauth.SIWCAccessTokenWithIdentity(t.Context(), account, nil)
			require.NoError(t, err)
			client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
				t.Error("validation must not refresh or substitute a credential")
				return nil, nil, errors.New("unexpected refresh")
			}
			require.NoError(t, validateSIWCInferenceGrant(t.Context(), oauth, account, token, owner))
			oldReg, oldGrant, registration, grant, err := oauth.readSIWCRecord(t.Context(), account)
			require.NoError(t, err)
			switch change {
			case "token_rotated":
				grant.AccessToken = "rotated-token-same-owner"
			case "rejected":
				grant.Rejected = true
			case "expired":
				grant.ExpiresAt = time.Now().Unix() - 1
			case "scope_removed":
				grant.Scope = "openid profile email"
			case "generation_changed":
				registration.Generation = "new-authorization-same-token"
			case "revoked":
				grant = nil
			}
			_, _, err = oauth.saveSIWCRecord(t.Context(), account, oldReg, oldGrant, registration, grant)
			require.NoError(t, err)
			require.Error(t, validateSIWCInferenceGrant(t.Context(), oauth, account, token, owner))
			if change == "token_rotated" {
				require.NoError(t, validateSIWCInferenceGrant(t.Context(), oauth, account, grant.AccessToken, owner))
			}
		})
	}
}

func TestSIWCInferenceGrantRejectsChangedSelectionAndParentEligibility(t *testing.T) {
	oauth, _, _, account := siwcFixture(t)
	authorizeSIWC(t, oauth, account)
	token, owner, err := oauth.SIWCAccessTokenWithIdentity(t.Context(), account, nil)
	require.NoError(t, err)
	require.Error(t, validateSIWCInferenceGrant(t.Context(), nil, account, token, owner))
	require.Error(t, validateSIWCInferenceGrant(t.Context(), oauth, nil, token, owner))
	account.Extra[OpenAISIWCEnabledKey] = false
	require.Error(t, validateSIWCInferenceGrant(t.Context(), oauth, account, token, owner))
	account.Extra[OpenAISIWCEnabledKey] = true
	vendorID := int64(12)
	account.VendorID = &vendorID
	require.Error(t, validateSIWCInferenceGrant(t.Context(), oauth, account, token, owner))
	account.VendorID = nil
	account.Credentials["chatgpt_account_id"] = "rebound-native-parent"
	require.Error(t, validateSIWCInferenceGrant(t.Context(), oauth, account, token, owner))
}
