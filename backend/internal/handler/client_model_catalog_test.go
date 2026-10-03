package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClientCatalogProjectionKeepsDiscoveryResponseAndSeparateETag(t *testing.T) {
	body := []byte(`{"models":[{"slug":"gpt-6-sol","extra":{"keep":true}},{"slug":"codex-auto-review","visibility":"hide"},{"slug":"gpt-reserve"},{"slug":"alias","model_purpose":"background"}],"revision":"original"}`)
	manifest := &service.OpenAIModelsResponse{Body: body, ETag: service.CodexModelsManifestETag(body)}
	render := func(client bool, etag string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		url := "/backend-api/codex/models"
		if client {
			url += "?catalog_view=client"
		}
		c.Request = httptest.NewRequest(http.MethodGet, url, nil)
		c.Request.Header.Set("If-None-Match", etag)
		prepareClientCatalogValidation(c)
		if client {
			require.Empty(t, c.GetHeader("If-None-Match"))
		}
		writeOpenAIModelsResponse(c, manifest)
		return rec
	}
	client := render(true, manifest.ETag)
	require.Equal(t, http.StatusOK, client.Code, client.Body.String())
	require.EqualValues(t, 1, gjson.GetBytes(client.Body.Bytes(), "models.#").Int())
	require.True(t, gjson.GetBytes(client.Body.Bytes(), "models.0.extra.keep").Bool())
	require.Equal(t, "original", gjson.GetBytes(client.Body.Bytes(), "revision").String())
	require.NotEqual(t, manifest.ETag, client.Header().Get("ETag"))
	require.Equal(t, http.StatusNotModified, render(true, client.Header().Get("ETag")).Code)
	require.Equal(t, body, manifest.Body, "client projection must not mutate shared upstream cache")
	require.Equal(t, body, render(false, "").Body.Bytes())
}

func TestClientCatalogStandardListLeavesHiddenModelsForCascadeDiscovery(t *testing.T) {
	for _, client := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		url := "/v1/models"
		if client {
			url += "?catalog_view=client"
		}
		c.Request = httptest.NewRequest(http.MethodGet, url, nil)
		writeOpenAIModelsList(c, []string{"gpt-6-sol", "gpt-reserve", "codex-auto-review"})
		require.Equal(t, http.StatusOK, rec.Code)
		if client {
			require.EqualValues(t, 1, gjson.GetBytes(rec.Body.Bytes(), "data.#").Int())
			require.NotContains(t, rec.Body.String(), "codex-auto-review")
		} else {
			require.EqualValues(t, 3, gjson.GetBytes(rec.Body.Bytes(), "data.#").Int())
		}
	}
}

func TestClientCatalogOpenAIHandlerProjectsAfterPolicyAndCaching(t *testing.T) {
	repo := &codexModelsFailoverAccountRepo{accounts: []service.Account{{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://upstream.example/v1"},
	}}}
	upstream := &codexModelsFailoverHTTPUpstream{firstBody: `{"models":[{"slug":"gpt-6-sol"},{"slug":"codex-auto-review"},{"slug":"gpt-reserve"}]}`}
	svc := service.NewOpenAIGatewayService(repo,
		nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil, nil, nil, nil, nil,
		upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &OpenAIGatewayHandler{gatewayService: svc}
	group := &service.Group{ID: 71, Platform: service.PlatformOpenAI}
	request := func(query, etag string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/backend-api/codex/models"+query, nil)
		c.Request.Header.Set("If-None-Match", etag)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group, GroupID: &group.ID})
		h.CodexModels(c)
		return rec
	}
	full := request("", "")
	require.Equal(t, http.StatusOK, full.Code, full.Body.String())
	require.Contains(t, full.Body.String(), "codex-auto-review")
	client := request("?catalog_view=client", full.Header().Get("ETag"))
	require.Equal(t, http.StatusOK, client.Code, client.Body.String())
	require.EqualValues(t, 1, gjson.GetBytes(client.Body.Bytes(), "models.#").Int())
	require.Equal(t, http.StatusNotModified, request("?catalog_view=client", client.Header().Get("ETag")).Code)
	require.Contains(t, request("", "").Body.String(), "codex-auto-review")
	group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-reserve"}}
	denied := request("?catalog_view=client", client.Header().Get("ETag"))
	require.Equal(t, http.StatusOK, denied.Code)
	require.EqualValues(t, 0, gjson.GetBytes(denied.Body.Bytes(), "models.#").Int())
}
