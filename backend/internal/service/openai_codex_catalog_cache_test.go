//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type codexCatalogCredentialRepo struct {
	AccountRepository
	mu      sync.RWMutex
	account *Account
	err     error
}

func (r *codexCatalogCredentialRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.account, r.err
}

func (r *codexCatalogCredentialRepo) replace(account *Account, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account, r.err = account, err
}

func catalogTestRequest(account *Account, version string) openAIModelsRequest {
	return openAIModelsRequest{url: "https://upstream.example/models?client_version=" + version,
		headers:   http.Header{"Authorization": {"Bearer " + account.GetOpenAIApiKey()}},
		accountID: account.ID, credentialAccountID: account.ID, credentialAccount: account,
		credentialScope: openAICatalogCredentialScope(account), useAPIKeyUpstream: true}
}

func catalogTestFailure(status int) error {
	return &codexModelsManifestUpstreamError{
		err:        infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "stub discovery failure"),
		statusCode: status, retryable: status == 0 || status == 429 || status >= 500,
	}
}

func catalogTestGeneration(s *OpenAIGatewayService, request openAIModelsRequest) (string, uint64) {
	s.openAIModelsCache.mu.Lock()
	defer s.openAIModelsCache.mu.Unlock()
	key := buildOpenAIModelsCacheKey(request)
	return key, s.openAIModelsCache.entries[key].generation
}

func TestCodexCatalogLateCredentialRefreshCannotPublish(t *testing.T) {
	for _, lateStatus := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(http.StatusText(lateStatus), func(t *testing.T) {
			old := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			current := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			current.Credentials["api_key"] = "rotated"
			repo := &codexCatalogCredentialRepo{account: old}
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var calls atomic.Int32
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls.Add(1)
				status, body := http.StatusOK, `{"models":[{"slug":"current"}]}`
				if req.Header.Get("Authorization") != "Bearer rotated" {
					close(started)
					<-release
					status, body = lateStatus, `{"models":[{"slug":"obsolete"}]}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			s := newCodexModelsAPIKeyTestService(upstream)
			s.accountRepo = repo
			done := make(chan error, 1)
			go func() { _, err := s.FetchCodexModelsManifest(t.Context(), old, "0.150.0", ""); done <- err }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("old fetch did not start")
			}
			repo.replace(current, nil)
			manifest, err := s.FetchCodexModelsManifest(t.Context(), current, "0.150.0", "")
			require.NoError(t, err)
			require.Contains(t, string(manifest.Body), "current")
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-done:
				require.ErrorIs(t, err, errOpenAICatalogChanged)
			case <-time.After(time.Second):
				t.Fatal("late fetch did not finish")
			}
			manifest, err = s.FetchCodexModelsManifest(t.Context(), current, "0.150.0", "")
			require.NoError(t, err)
			require.Contains(t, string(manifest.Body), "current")
			require.EqualValues(t, 2, calls.Load())
			_, err = s.FetchCodexModelsManifest(t.Context(), old, "0.150.0", "")
			require.ErrorIs(t, err, errOpenAICatalogChanged, "an old caller must not roll the owner back")
		})
	}
}

func TestCodexCatalogCredentialRevalidatedWithoutAnotherRequest(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	repo := &codexCatalogCredentialRepo{account: account}
	s := &OpenAIGatewayService{accountRepo: repo}
	request := catalogTestRequest(account, "v1")
	fetch := func(context.Context, string) (*OpenAIModelsResponse, error) {
		repo.replace(nil, ErrAccountNotFound)
		return &OpenAIModelsResponse{Body: []byte(`{"models":[]}`)}, nil
	}
	_, err := s.fetchCachedOpenAIModels(t.Context(), request, fetch, "")
	require.ErrorIs(t, err, errOpenAICatalogChanged)
	s.openAIModelsCache.mu.Lock()
	defer s.openAIModelsCache.mu.Unlock()
	require.Empty(t, s.openAIModelsCache.entries)
}

func TestCodexCatalogLateAuthFailureDoesNotDisableRotatedAccount(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	current := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	current.Credentials["api_key"] = "new-credential"
	repo := &codexCatalogCredentialRepo{account: account}
	svc := &OpenAIGatewayService{accountRepo: repo}
	request := catalogTestRequest(account, "v1")
	var notifications atomic.Int32
	request.authFailure = func(context.Context, error) { notifications.Add(1) }
	_, err := svc.fetchCachedOpenAIModels(t.Context(), request, func(context.Context, string) (*OpenAIModelsResponse, error) {
		repo.replace(current, nil)
		return nil, catalogTestFailure(401)
	}, "")
	require.ErrorIs(t, err, errOpenAICatalogChanged)
	require.Zero(t, notifications.Load(), "a late old credential failure cannot reach account disabling")
}

func TestCodexCatalogAuthFailureFencesSiblingRefresh(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			s := &OpenAIGatewayService{}
			a, b := catalogTestRequest(account, "v1"), catalogTestRequest(account, "v2")
			success := func(context.Context, string) (*OpenAIModelsResponse, error) {
				return &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"old"}]}`)}, nil
			}
			_, err := s.fetchCachedOpenAIModels(t.Context(), a, success, "")
			require.NoError(t, err)
			_, err = s.fetchCachedOpenAIModels(t.Context(), b, success, "")
			require.NoError(t, err)
			keyA, generationA := catalogTestGeneration(s, a)
			keyB, generationB := catalogTestGeneration(s, b)
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			late := s.refreshCachedOpenAIModels(keyB, generationB, b, func(context.Context, string) (*OpenAIModelsResponse, error) {
				close(started)
				<-release
				return &OpenAIModelsResponse{NotModified: true}, nil
			})
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("sibling did not start")
			}
			failure := catalogTestFailure(status)
			failed := <-s.refreshCachedOpenAIModels(keyA, generationA, a, func(context.Context, string) (*OpenAIModelsResponse, error) { return nil, failure })
			require.ErrorIs(t, failed.Err, failure)
			releaseOnce.Do(func() { close(release) })
			require.ErrorIs(t, (<-late).Err, errOpenAICatalogChanged)
			for _, request := range []openAIModelsRequest{a, b, catalogTestRequest(account, "v3")} {
				_, err = s.fetchCachedOpenAIModels(t.Context(), request, func(context.Context, string) (*OpenAIModelsResponse, error) {
					t.Error("negative cache bypassed")
					return nil, failure
				}, "")
				require.ErrorIs(t, err, failure)
				manifest, _ := s.openAIModelsCache.get(buildOpenAIModelsCacheKey(request), time.Now())
				require.Nil(t, manifest)
			}
			other := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			other.ID++
			_, err = s.fetchCachedOpenAIModels(t.Context(), catalogTestRequest(other, "v1"), success, "")
			require.NoError(t, err, "discovery failure is not a platform-wide revocation")
		})
	}
}

func TestCodexCatalogTransientFailureKeepsBoundedLKG(t *testing.T) {
	for _, status := range []int{0, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			s := &OpenAIGatewayService{}
			request := catalogTestRequest(account, "v1")
			success := func(context.Context, string) (*OpenAIModelsResponse, error) {
				return &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"lkg"}]}`), upstreamETag: `"original"`}, nil
			}
			_, err := s.fetchCachedOpenAIModels(t.Context(), request, success, "")
			require.NoError(t, err)
			expireCodexModelsManifestCache(s, 2*time.Minute)
			key, generation := catalogTestGeneration(s, request)
			failure := catalogTestFailure(status)
			var calls atomic.Int32
			fetch := func(context.Context, string) (*OpenAIModelsResponse, error) { calls.Add(1); return nil, failure }
			result := <-s.refreshCachedOpenAIModels(key, generation, request, fetch)
			require.ErrorIs(t, result.Err, failure)
			manifest, err := s.fetchCachedOpenAIModels(t.Context(), request, fetch, "")
			require.NoError(t, err)
			require.Contains(t, string(manifest.Body), "lkg")
			require.ErrorIs(t, (<-s.refreshCachedOpenAIModels(key, generation, request, fetch)).Err, failure)
			require.EqualValues(t, 1, calls.Load(), "short negative cache must suppress repeated network calls")
			expireCodexModelsManifestCache(s, 6*time.Minute)
			manifest, err = s.fetchCachedOpenAIModels(t.Context(), request, fetch, "")
			require.Error(t, err, "network failure must not extend the maximum stale lifetime")
			require.Nil(t, manifest)
		})
	}
}

func TestCodexCatalogRepositoryOutageDoesNotRevokeLKG(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	repo := &codexCatalogCredentialRepo{account: account}
	s := &OpenAIGatewayService{accountRepo: repo}
	request := catalogTestRequest(account, "v1")
	fetch := func(context.Context, string) (*OpenAIModelsResponse, error) {
		return &OpenAIModelsResponse{Body: []byte(`{"models":[]}`)}, nil
	}
	_, err := s.fetchCachedOpenAIModels(t.Context(), request, fetch, "")
	require.NoError(t, err)
	outage := errors.New("database unavailable")
	repo.replace(nil, outage)
	_, err = s.fetchCachedOpenAIModels(t.Context(), request, fetch, "")
	require.ErrorIs(t, err, outage)
	manifest, state := s.openAIModelsCache.get(buildOpenAIModelsCacheKey(request), time.Now())
	require.NotNil(t, manifest)
	require.Equal(t, openAIModelsCacheFresh, state)
}

func TestCodexCatalogSuccessfulEmptyListWithdrawsModels(t *testing.T) {
	var empty atomic.Bool
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls.Add(1)
		body := `{"object":"list","data":[{"id":"custom"}]}`
		if empty.Load() {
			body = `{"object":"list","data":[]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	first, err := s.FetchCodexModelsManifest(t.Context(), account, "0.150.0", "")
	require.NoError(t, err)
	require.Contains(t, string(first.Body), "custom")
	empty.Store(true)
	expireCodexModelsManifestCache(s, 6*time.Minute)
	manifest, err := s.FetchCodexModelsManifest(t.Context(), account, "0.150.0", first.ETag)
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[]}`, string(manifest.Body))
	require.False(t, manifest.NotModified)
	again, err := s.FetchCodexModelsManifest(t.Context(), account, "0.150.0", "")
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[]}`, string(again.Body))
	require.EqualValues(t, 2, calls.Load())
}
