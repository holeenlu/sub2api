//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type catalogMemoryRepo struct {
	ModelCatalogRepository
	mu        sync.Mutex
	snapshots map[string]*ModelCatalogSnapshot
	leased    map[string]string
	failed    string
}

func (r *catalogMemoryRepo) Current(_ context.Context, key string) (*ModelCatalogSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source := r.snapshots[key]
	if source == nil {
		return nil, nil
	}
	body, _ := json.Marshal(source)
	var out ModelCatalogSnapshot
	_ = json.Unmarshal(body, &out)
	out.ScopeRevision = source.ScopeRevision
	return &out, nil
}
func (r *catalogMemoryRepo) Claim(_ context.Context, key string, _ int64, _, _, token string, _ time.Time, _ bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leased == nil {
		r.leased = map[string]string{}
	}
	if r.leased[key] != "" {
		return false, nil
	}
	r.leased[key] = token
	return true, nil
}
func (r *catalogMemoryRepo) Publish(_ context.Context, key, token string, snapshot *ModelCatalogSnapshot, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leased[key] != token {
		return ErrModelCatalogScopeChanged
	}
	if r.snapshots == nil {
		r.snapshots = map[string]*ModelCatalogSnapshot{}
	}
	r.snapshots[key] = snapshot
	delete(r.leased, key)
	return nil
}
func (r *catalogMemoryRepo) Fail(_ context.Context, key, token, code string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = code
	if r.leased[key] == token {
		delete(r.leased, key)
	}
	return nil
}

type catalogAccountRepository struct {
	AccountRepository
	a *Account
}

func (r *catalogAccountRepository) GetByID(context.Context, int64) (*Account, error) { return r.a, nil }
func (r *catalogAccountRepository) UpdateExtra(_ context.Context, _ int64, extra map[string]any) error {
	if r.a.Extra == nil {
		r.a.Extra = map[string]any{}
	}
	for k, v := range extra {
		r.a.Extra[k] = v
	}
	return nil
}
func (r *catalogAccountRepository) ListActive(context.Context) ([]Account, error) {
	return []Account{*r.a}, nil
}

func catalogResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func newCatalogTestService(responses ...*http.Response) (*ModelCatalogService, *catalogMemoryRepo, *Account, *httpUpstreamRecorder) {
	a := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://upstream.example/v1"}}
	accounts := &catalogAccountRepository{a: a}
	repo := &catalogMemoryRepo{}
	upstream := &httpUpstreamRecorder{responses: responses}
	syncer := &AccountTestService{accountRepo: accounts, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	return NewModelCatalogService(repo, accounts, syncer, nil, nil), repo, a, upstream
}

func TestModelCatalogDiscoversNovelModelWithoutPresetOrRestart(t *testing.T) {
	// Deliberately not a known GPT spelling. Capability identity comes from data.
	body := `{"data":[{"id":"opaque-new-model-731","model_kind":"chat","display_name":"New model","reasoning":true,"supported_reasoning_levels":["medium","high"],"input_modalities":["text","image"],"context_window":123456,"endpoints":["responses"]}]}`
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, body))
	snapshot, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Models, 1)
	require.Equal(t, "opaque-new-model-731", snapshot.Models[0].ID)
	require.Empty(t, snapshot.Models[0].Missing)
	require.Equal(t, "listed", snapshot.Models[0].Access)
	require.NotEmpty(t, snapshot.Revision)
	// A second service process reads the persisted result without fetching.
	restarted := NewModelCatalogService(repo, s.accounts, nil, nil, nil)
	current, err := restarted.Account(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, snapshot.Revision, current.Revision)
	ids, _, ok := restarted.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.Equal(t, []string{"opaque-new-model-731"}, ids)
}

func TestModelCatalogRetirementAndPartialMediaPersistIndependently(t *testing.T) {
	body := `{"data":[{"id":"custom-old","shutdown_date":"2000-01-01"},{"id":"vendor-picture-731","model_kind":"image","output_modalities":["image"],"endpoints":["images/generations"]}]}`
	s, _, a, _ := newCatalogTestService(catalogResponse(200, body), catalogResponse(503, `{"error":"registry unavailable"}`))
	snapshot, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Models, 2)
	byID := map[string]ModelCatalogEntry{}
	for _, m := range snapshot.Models {
		byID[m.ID] = m
	}
	require.Equal(t, "retired", byID["custom-old"].Lifecycle)
	require.Equal(t, "image", byID["vendor-picture-731"].Kind)
	require.Empty(t, byID["vendor-picture-731"].Missing, "media does not require text context or reasoning")
	ids, _, ok := s.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.Equal(t, []string{"vendor-picture-731"}, ids)
}

func TestModelCatalogCredentialChangesInvalidateButPolicyEditsDoNot(t *testing.T) {
	body := `{"data":[{"id":"gpt-image-novel","output_modalities":["image"]}]}`
	s, _, a, _ := newCatalogTestService(catalogResponse(200, body))
	_, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	a.Credentials["model_mapping"] = map[string]any{"public-image": "gpt-image-novel"}
	ids, _, ok := s.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.Equal(t, []string{"public-image"}, ids)
	a.Credentials["api_key"] = "new-scope"
	_, _, ok = s.ModelCatalogSnapshot(context.Background(), a)
	require.False(t, ok)
	current, err := s.Account(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "unavailable", current.Status)
}

func TestModelCatalogFailedRefreshKeepsSnapshotAndEmptySuccessClearsVisibility(t *testing.T) {
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-novel"}]}`), catalogResponse(503, `{}`), catalogResponse(200, `{"data":[]}`))
	first, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	_, err = s.Refresh(context.Background(), a.ID, true)
	require.ErrorIs(t, err, ErrModelCatalogUnavailable)
	current, err := s.Account(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, first.Revision, current.Revision)
	require.Equal(t, "upstream_unavailable", repo.failed)
	empty, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	require.Len(t, empty.Models, 1)
	require.Equal(t, "unlisted", empty.Models[0].Access)
	ids, _, ok := s.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.Empty(t, ids)
}

func TestModelCatalogPaginationRetainsPagesAndRejectsMissingCursor(t *testing.T) {
	s, _, a, upstream := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-a"}],"has_more":true,"last_id":"gpt-image-a"}`), catalogResponse(200, `{"data":[{"id":"gpt-image-b"}],"has_more":false}`))
	got, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	require.Len(t, got.Models, 2)
	require.Equal(t, "gpt-image-a", upstream.lastReq.URL.Query().Get("after_id"))
	bad, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"x"}],"has_more":true}`))
	_, err = bad.Refresh(context.Background(), a.ID, true)
	require.Error(t, err)
	require.Empty(t, repo.snapshots)
}

func TestNativeModelCatalogParsesClaudeAndGeminiCapabilities(t *testing.T) {
	_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"claude-new","max_input_tokens":150000,"max_tokens":32000,"capabilities":{"image_input":{"supported":true},"effort":{"supported":true,"medium":{"supported":true},"high":{"supported":true}}}},{"name":"models/gemini-next","inputTokenLimit":200000,"outputTokenLimit":12000,"supportedGenerationMethods":["generateContent"]}]}`), false)
	require.NoError(t, err)
	require.Equal(t, int64(150000), metadata["claude-new"].ContextWindow)
	require.Equal(t, []string{"medium", "high"}, metadata["claude-new"].SupportedReasoningLevels)
	require.Equal(t, []string{"text", "image"}, metadata["claude-new"].InputModalities)
	require.Equal(t, int64(200000), metadata["gemini-next"].ContextWindow)
	require.Equal(t, []string{"generateContent"}, metadata["gemini-next"].Endpoints)
}

func (r *catalogMemoryRepo) MediaObservations(context.Context, int64, string) ([]CatalogMediaObservation, error) {
	return nil, nil
}
func (r *catalogMemoryRepo) ObserveMedia(context.Context, int64, string, CatalogMediaObservation) error {
	return nil
}
