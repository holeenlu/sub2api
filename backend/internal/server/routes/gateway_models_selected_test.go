package routes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type selectedModelsRoutesRepository struct {
	service.AccountRepository
	account service.Account
}

func (r *selectedModelsRoutesRepository) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *selectedModelsRoutesRepository) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *selectedModelsRoutesRepository) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

type selectedModelsRoutesUpstream struct {
	service.HTTPUpstream
	ordinaryCalls atomic.Int32
	codexCalls    atomic.Int32
}

func (u *selectedModelsRoutesUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body := `{"data":[{"id":"ordinary-upstream-model"}]}`
	if req.URL.Query().Has("client_version") {
		u.codexCalls.Add(1)
		body = `{"models":[{"slug":"gpt-5.5"}]}`
	} else {
		u.ordinaryCalls.Add(1)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func newSelectedModelsRoutesRouter(manifest service.GroupCodexModelsManifestConfig) (*gin.Engine, *selectedModelsRoutesUpstream) {
	gin.SetMode(gin.TestMode)
	repo := &selectedModelsRoutesRepository{account: service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-models-key", "base_url": "https://models.example/v1", "model_mapping": map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"}},
	}}
	upstream := &selectedModelsRoutesUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	s := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	gateway := service.NewGatewayService(repo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &handler.Handlers{
		Gateway:       handler.NewGatewayHandler(gateway, s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
		OpenAIGateway: handler.NewOpenAIGatewayHandler(s, nil, nil, nil, nil, nil, nil, nil, cfg),
		AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
	}
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI,
		ModelAllowlist:            service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5"}},
		CodexModelsManifestConfig: manifest}
	router := gin.New()
	RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
		c.Next()
	}), nil, nil, nil, nil, nil, cfg)
	return router, upstream
}

func requireSelectedOrdinaryModels(t *testing.T, router *gin.Engine) {
	t.Helper()
	for _, path := range []string{"/v1/models", "/models", "/v1/models?client_version=", "/models?client_version="} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		var response struct {
			Object string `json:"object"`
			Data   []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, "list", response.Object)
		require.Len(t, response.Data, 1)
		require.Equal(t, "gpt-5.5", response.Data[0].ID)
	}
}

var selectedModelsCodexPaths = []string{"/v1/models?client_version=" + service.CodexCanonicalClientVersion(), "/models?client_version=" + service.CodexCanonicalClientVersion(), "/backend-api/codex/models"}

func TestGatewayRoutesSelectedModelsDispatchesOrdinaryAndCodexRequests(t *testing.T) {
	// A disabled fixed source keeps its saved IDs but has no runtime effect.
	router, upstream := newSelectedModelsRoutesRouter(service.GroupCodexModelsManifestConfig{AccountIDs: []int64{999}})
	requireSelectedOrdinaryModels(t, router)
	for _, path := range selectedModelsCodexPaths {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		require.Contains(t, w.Body.String(), `"slug":"gpt-5.5"`)
		require.NotContains(t, w.Body.String(), `"data"`)
	}
	require.Zero(t, upstream.ordinaryCalls.Load(), "reading group selections never fetches a pinned upstream")
	require.Zero(t, upstream.codexCalls.Load(), "configured capabilities are resolved locally")
}

func TestGatewayRoutesSelectedModelsEnabledFixedSourceOnlyGovernsCodexManifest(t *testing.T) {
	// An enabled fixed source whose accounts left the group fails closed for
	// Codex discovery without fallback, and never touches the ordinary list.
	router, upstream := newSelectedModelsRoutesRouter(service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{999}})
	requireSelectedOrdinaryModels(t, router)
	for _, path := range selectedModelsCodexPaths {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s: %s", path, w.Body.String())
		require.Contains(t, w.Body.String(), "No available pinned OpenAI accounts")
	}
	require.Zero(t, upstream.ordinaryCalls.Load(), "the ordinary list never fetches a pinned upstream")
	require.Zero(t, upstream.codexCalls.Load(), "non-member pinned IDs are never fetched")
}

func TestGatewayRoutesRetrieveSelectedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &selectedModelsRoutesRepository{account: service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-models-key", "base_url": "https://models.example/v1", "model_mapping": map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"}},
	}}
	upstream := &selectedModelsRoutesUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	s := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	gateway := service.NewGatewayService(repo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &handler.Handlers{
		Gateway:       handler.NewGatewayHandler(gateway, s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
		OpenAIGateway: handler.NewOpenAIGatewayHandler(s, nil, nil, nil, nil, nil, nil, nil, cfg),
		AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
	}
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5"}},
		// Obsolete pinned source IDs cannot affect the selected model list.
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{999}}}
	router := gin.New()
	RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer client-key" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
		c.Next()
	}), nil, nil, nil, nil, nil, cfg)
	request := func(path, key, etag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", key)
		req.Header.Set("If-None-Match", etag)
		router.ServeHTTP(w, req)
		return w
	}
	list := request("/v1/models", "Bearer client-key", "")
	require.Equal(t, http.StatusOK, list.Code)
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &catalog))
	require.Len(t, catalog.Data, 1)
	for _, base := range []string{"/v1/models/", "/models/"} {
		path := base + "gpt-5.5?client_version=ignored"
		require.Equal(t, http.StatusUnauthorized, request(path, "", "").Code)
		got := request(path, "Bearer client-key", list.Header().Get("ETag"))
		require.Equal(t, http.StatusOK, got.Code, got.Body.String())
		require.JSONEq(t, string(catalog.Data[0]), got.Body.String())
		require.Empty(t, got.Header().Get("ETag"), "a collection validator cannot validate one model")
		missing := request(base+"unknown-model", "Bearer client-key", "")
		require.Equal(t, http.StatusNotFound, missing.Code)
		require.Contains(t, missing.Body.String(), `"error"`)
	}
	require.Zero(t, upstream.codexCalls.Load(), "retrieve never dispatches a Codex manifest")
}
