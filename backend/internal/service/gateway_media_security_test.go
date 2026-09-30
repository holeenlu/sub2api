package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type securityMediaRepo struct {
	mu           sync.Mutex
	voices       map[string]GatewayMediaVoice
	jobs         map[string][]byte
	failTerminal bool
}

func newSecurityMediaRepo() *securityMediaRepo {
	return &securityMediaRepo{voices: map[string]GatewayMediaVoice{}, jobs: map[string][]byte{}}
}
func (r *securityMediaRepo) PutVoice(_ context.Context, v *GatewayMediaVoice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.voices[v.ID]; ok && (old.UserID != v.UserID || old.GroupID != v.GroupID) {
		return ErrMediaNotOwned
	}
	r.voices[v.ID] = *v
	return nil
}
func (r *securityMediaRepo) GetVoice(_ context.Context, g, u int64, id string) (*GatewayMediaVoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.voices[id]
	if !ok || v.UserID != u || v.GroupID != g {
		return nil, ErrMediaNotOwned
	}
	return &v, nil
}
func (r *securityMediaRepo) ListVoices(_ context.Context, g, u int64) ([]GatewayMediaVoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []GatewayMediaVoice{}
	for _, v := range r.voices {
		if v.GroupID == g && v.UserID == u {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *securityMediaRepo) DeleteVoice(_ context.Context, g, u int64, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.voices[id]
	if v.GroupID == g && v.UserID == u {
		delete(r.voices, id)
	}
	return nil
}
func (r *securityMediaRepo) CreateJob(_ context.Context, j *GatewayMediaJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, raw := range r.jobs {
		var v GatewayMediaJob
		_ = json.Unmarshal(raw, &v)
		if v.UserID == j.UserID && v.State != "settled" && v.State != "failed" {
			return ErrMediaPendingCapacity
		}
	}
	raw, err := json.Marshal(j)
	r.jobs[j.ID] = raw
	return err
}
func (r *securityMediaRepo) SaveJob(_ context.Context, j *GatewayMediaJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.State == "settled" && r.failTerminal {
		r.failTerminal = false
		return errors.New("simulated crash after billing")
	}
	var old GatewayMediaJob
	_ = json.Unmarshal(r.jobs[j.ID], &old)
	if j.Version != old.Version {
		return errors.New("stale version")
	}
	j.Version++
	raw, err := json.Marshal(j)
	r.jobs[j.ID] = raw
	return err
}
func (r *securityMediaRepo) GetJob(_ context.Context, g, u int64, id string) (*GatewayMediaJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, raw := range r.jobs {
		var v GatewayMediaJob
		_ = json.Unmarshal(raw, &v)
		if v.GroupID == g && v.UserID == u && v.TaskID == id {
			return &v, nil
		}
	}
	return nil, ErrMediaNotOwned
}
func (r *securityMediaRepo) ClaimJobs(_ context.Context, n int) ([]GatewayMediaJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []GatewayMediaJob{}
	for _, raw := range r.jobs {
		var v GatewayMediaJob
		_ = json.Unmarshal(raw, &v)
		if v.State == "pending" || v.State == "billing" || v.State == "releasing" || v.State == "reserving" {
			out = append(out, v)
			if len(out) >= n {
				break
			}
		}
	}
	return out, nil
}

type securityMediaBilling struct {
	UsageBillingRepository
	seen                              map[string]string
	charged, held, captured, released float64
	calls                             int
}

func (b *securityMediaBilling) Apply(_ context.Context, c *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	c.Normalize()
	b.calls++
	if fingerprint, ok := b.seen[c.RequestID]; ok {
		if fingerprint != c.RequestFingerprint {
			return nil, ErrUsageBillingRequestConflict
		}
		return &UsageBillingApplyResult{}, nil
	}
	b.seen[c.RequestID] = c.RequestFingerprint
	b.charged += c.BalanceCost + c.SubscriptionCost
	return &UsageBillingApplyResult{Applied: true}, nil
}
func (b *securityMediaBilling) hold(c *BatchImageBalanceHoldCommand, phase string) (*BatchImageBalanceHoldResult, error) {
	c.Normalize()
	if _, ok := b.seen[c.RequestID]; ok {
		return &BatchImageBalanceHoldResult{}, nil
	}
	b.seen[c.RequestID] = c.RequestFingerprint
	switch phase {
	case "reserve":
		b.held += c.HoldAmount
	case "capture":
		b.captured += c.ActualAmount
	case "release":
		b.released += c.HoldAmount
	}
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}
func (b *securityMediaBilling) ReserveBatchImageBalance(_ context.Context, c *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return b.hold(c, "reserve")
}
func (b *securityMediaBilling) CaptureBatchImageBalance(_ context.Context, c *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return b.hold(c, "capture")
}
func (b *securityMediaBilling) ReleaseBatchImageBalance(_ context.Context, c *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return b.hold(c, "release")
}

type securityMediaHTTP struct {
	HTTPUpstream
	response string
	calls    int
}

func (u *securityMediaHTTP) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.response))}, nil
}

func securityVoiceContext(user, key int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/tts", nil)
	g := int64(10)
	c.Set("api_key", &APIKey{ID: key, UserID: user, GroupID: &g})
	return c
}

func TestSecurityCustomVoiceOwnerAllReferences(t *testing.T) {
	repo := newSecurityMediaRepo()
	svc := &OpenAIGatewayService{mediaRepo: repo}
	ctx := context.Background()
	require.NoError(t, repo.PutVoice(ctx, &GatewayMediaVoice{ID: "private", UserID: 1, GroupID: 10, AccountID: 77, Metadata: json.RawMessage(`{"voice_id":"private"}`)}))
	for _, endpoint := range []string{"custom-voices/private", "custom-voices/private/audio"} {
		_, err := svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(2, 20), endpoint, nil)
		require.ErrorIs(t, err, ErrMediaNotOwned)
		id, err := svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(1, 99), endpoint, nil)
		require.NoError(t, err)
		require.EqualValues(t, 77, id)
	}
	for _, body := range []string{`{"voice_id":"private"}`, `{"voice":"private"}`, `{"session":{"voice":"private"}}`, `{"response":{"audio":{"output":{"voice":{"id":"private"}}}}}`, `{"voice":"Ara","voice_id":"private"}`, `{"voice":"Ara","voice":"private"}`, `{"voice":"Ara","vo\u0069ce":"private"}`} {
		for _, endpoint := range []string{"tts", "realtime"} {
			_, err := svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(2, 20), endpoint, []byte(body))
			require.Error(t, err, body)
		}
	}
	for _, endpoint := range []string{"tts", "realtime"} {
		id, err := svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(1, 99), endpoint, []byte(`{"voice":"private"}`))
		require.NoError(t, err)
		require.EqualValues(t, 77, id)
		id, err = svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(2, 20), endpoint, []byte(`{"voice":"Ara"}`))
		require.NoError(t, err)
		require.Zero(t, id)
	}
	voices, err := svc.ListOwnedGrokVoices(ctx, securityVoiceContext(2, 20))
	require.NoError(t, err)
	require.Empty(t, voices)
	voices, err = svc.ListOwnedGrokVoices(ctx, securityVoiceContext(1, 99))
	require.NoError(t, err)
	require.Len(t, voices, 1)
	_, err = svc.ResolveGrokVoiceAccount(ctx, securityVoiceContext(1, 99), "tts", []byte(`{"voice":"unowned_historical"}`))
	require.ErrorIs(t, err, ErrMediaNotOwned)
}

func TestSecurityVideoServerDrivenRestartAndDedup(t *testing.T) {
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions, SeedanceEndpointCreate} {
		t.Run(string(endpoint), func(t *testing.T) {
			ctx := context.Background()
			repo := newSecurityMediaRepo()
			billing := &securityMediaBilling{seen: map[string]string{}}
			usage := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usage, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			account := &Account{ID: 77, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream"}}
			upstream := &securityMediaHTTP{response: `{"status":"done","video":{"url":"https://cdn.example/video.mp4","duration":8}}`}
			model := "grok-imagine-video"
			if endpoint.IsSeedance() {
				account.Platform = PlatformOpenAI
				account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/v3"
				account.Credentials["openai_capabilities"] = []string{"seedance"}
				upstream.response = `{"status":"succeeded","usage":{"completion_tokens":1200}}`
				model = "gpt-5.1"
			}
			svc.accountRepo = &openAIRecordUsageAccountRepoStub{account: account}
			svc.mediaRepo = repo
			svc.usageBillingRepo = billing
			svc.httpUpstream = upstream
			key := &APIKey{ID: 9, UserID: 1, User: &User{ID: 1, PasswordHash: "must-not-persist"}, Key: "must-not-persist"}
			j, err := svc.PrepareGatewayVideoJob(ctx, key, nil, account, endpoint, GrokMediaRequestInfo{Model: model, DurationSeconds: 8}, model, "/videos", account.Platform)
			require.NoError(t, err)
			key2 := *key
			key2.ID = 10
			_, err = svc.PrepareGatewayVideoJob(ctx, &key2, nil, account, endpoint, GrokMediaRequestInfo{Model: model}, model, "/videos", account.Platform)
			require.ErrorIs(t, err, ErrMediaPendingCapacity)
			task := "task1"
			if endpoint.IsSeedance() {
				task = SeedanceTaskKey(task)
			}
			j.CreatedAt = time.Now().Add(-48 * time.Hour)
			require.NoError(t, svc.acceptGatewayVideo(WithGatewayMediaJob(ctx, j), task))
			stored := repo.jobs[j.ID]
			require.NotContains(t, string(stored), "must-not-persist")
			// A fresh worker loads only the durable record; no client status request.
			jobs, err := repo.ClaimJobs(ctx, 1)
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			repo.failTerminal = true
			err = svc.settleGatewayMediaJob(ctx, &jobs[0])
			require.ErrorContains(t, err, "simulated crash")
			require.Greater(t, billing.charged+billing.captured, 0.0)
			cost := billing.charged + billing.captured
			jobs, err = repo.ClaimJobs(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, "billing", jobs[0].State)
			require.NoError(t, svc.settleGatewayMediaJob(ctx, &jobs[0]))
			require.Equal(t, cost, billing.charged+billing.captured)
			require.Equal(t, 1, upstream.calls, "retry must use the frozen completion, not poll/reprice")
			got, err := repo.GetJob(ctx, 0, 1, task)
			require.NoError(t, err)
			require.Equal(t, "settled", got.State)
			require.Greater(t, got.Result.Duration, 24*time.Hour)
			if endpoint.IsSeedance() {
				require.Equal(t, 1200, got.Result.Usage.OutputTokens)
				require.InDelta(t, got.UnitCost.ActualCost*1200, got.Cost.ActualCost, 1e-12)
				require.Zero(t, got.Result.VideoCount)
			} else {
				require.Equal(t, 8, got.Result.VideoDurationSeconds)
			}
		})
	}
}

func TestSecurityVideoAmbiguousSubmissionRetainsCapacity(t *testing.T) {
	repo := newSecurityMediaRepo()
	svc := &OpenAIGatewayService{mediaRepo: repo}
	j := &GatewayMediaJob{ID: "unknown", State: "submitting", UserID: 1, Key: &APIKey{ID: 1}}
	require.NoError(t, repo.CreateJob(context.Background(), j))
	ctx := WithGatewayMediaJob(context.Background(), j)
	svc.mediaSubmissionStarting(ctx)
	for _, status := range []int{0, 408, 500, 502} {
		svc.mediaSubmissionRejected(ctx, status)
		require.Equal(t, "submitting", j.State)
	}
	svc.FinishGatewayVideoAttempt(ctx, j, io.EOF)
	require.Equal(t, "submitting", j.State)
	require.ErrorIs(t, repo.CreateJob(ctx, &GatewayMediaJob{ID: "new-key", UserID: 1, Key: &APIKey{ID: 99}}), ErrMediaPendingCapacity)
	jobs, err := repo.ClaimJobs(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, jobs, "unknown creates must not be resubmitted")
	svc.mediaSubmissionRejected(ctx, 400)
	require.Equal(t, "failed", j.State)
	require.NoError(t, repo.CreateJob(ctx, &GatewayMediaJob{ID: "control", UserID: 1}))
}

func TestSecurityContinuationJSONAndTenantPool(t *testing.T) {
	for _, body := range []string{`{"previous_response_id":"","previous_response_id":"resp_victim"}`, `{"previous_response_id":"","previous_response_\u0069d":"resp_victim"}`, `{"Previous_Response_ID":"resp_victim"}`, `{"previous_response_id":123}`, `{"response":{"previous_response_id":"resp_victim"}}`} {
		require.Error(t, ValidateGatewaySecurityJSON([]byte(body)), body)
	}
	require.NoError(t, ValidateGatewaySecurityJSON([]byte(`{"model":"gpt-5.4","previous_response_id":"resp_own","text":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`)))
	a := openAIWSAcquireRequest{TenantScope: "10/user/1"}
	b := openAIWSAcquireRequest{TenantScope: "10/user/2"}
	require.NotEqual(t, normalizeOpenAIWSAcquireCompatibility(a, nil), normalizeOpenAIWSAcquireCompatibility(b, nil))
	svc := &OpenAIGatewayService{}
	ctx := context.Background()
	owner := securityVoiceContext(1, 11)
	SetOpenAIHTTPResponseOwner(owner, 1, 11)
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, 10, "resp_owned", 1, 11))
	otherKey := securityVoiceContext(1, 99)
	SetOpenAIHTTPResponseOwner(otherKey, 1, 99)
	require.NoError(t, svc.validateWSContinuation(ctx, otherKey, []byte(`{"previous_response_id":"resp_owned"}`)))
	require.Equal(t, openAIWSTenantScope(owner), openAIWSTenantScope(otherKey))
	attacker := securityVoiceContext(2, 22)
	SetOpenAIHTTPResponseOwner(attacker, 2, 22)
	require.Error(t, svc.validateWSContinuation(ctx, attacker, []byte(`{"previous_response_id":"resp_owned"}`)))
}

func TestSecurityCustomVoiceCRUDAndPersistence(t *testing.T) {
	repo := newSecurityMediaRepo()
	upstream := &securityMediaHTTP{response: `{"voice_id":"created","name":"mine"}`}
	svc := &OpenAIGatewayService{mediaRepo: repo, httpUpstream: upstream}
	account := &Account{ID: 77, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream"}}
	ctx := context.Background()
	owner := securityVoiceContext(1, 11)
	owner.Request = httptest.NewRequest(http.MethodPost, "/custom-voices", nil)
	_, err := svc.ForwardGrokVoice(ctx, owner, account, "custom-voices", []byte(`{"name":"mine"}`), "application/json")
	require.NoError(t, err)
	stored, err := repo.GetVoice(ctx, 10, 1, "created")
	require.NoError(t, err)
	require.EqualValues(t, 77, stored.AccountID)
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		for _, endpoint := range []string{"custom-voices/created", "custom-voices/created/audio"} {
			attacker := securityVoiceContext(2, 22)
			attacker.Request = httptest.NewRequest(method, "/v1/"+endpoint, nil)
			before := upstream.calls
			_, err := svc.ForwardGrokVoice(ctx, attacker, account, endpoint, []byte(`{"name":"stolen"}`), "application/json")
			require.ErrorIs(t, err, ErrMediaNotOwned)
			require.Equal(t, before, upstream.calls)
		}
	}
	siblingKey := securityVoiceContext(1, 99)
	siblingKey.Request = httptest.NewRequest(http.MethodGet, "/custom-voices/created/audio", nil)
	_, err = svc.ForwardGrokVoice(ctx, siblingKey, account, "custom-voices/created/audio", nil, "")
	require.NoError(t, err)
	wrongAccount := *account
	wrongAccount.ID = 88
	_, err = svc.ForwardGrokVoice(ctx, siblingKey, &wrongAccount, "custom-voices/created/audio", nil, "")
	require.ErrorIs(t, err, ErrMediaNotOwned)
	// Deleting through a different key of the same tenant removes the durable grant.
	siblingKey.Request = httptest.NewRequest(http.MethodDelete, "/custom-voices/created", nil)
	_, err = svc.ForwardGrokVoice(ctx, siblingKey, account, "custom-voices/created", nil, "")
	require.NoError(t, err)
	_, err = repo.GetVoice(ctx, 10, 1, "created")
	require.ErrorIs(t, err, ErrMediaNotOwned)
}

func TestSecurityHTTPTransformFinalContinuationReauthorized(t *testing.T) {
	ctx := context.Background()
	svc := &OpenAIGatewayService{}
	c := securityVoiceContext(1, 11)
	account := &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream"}}
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, 10, "resp_victim", 2, 22))
	// Represents a final JSON/map re-encoding on a non-passthrough API-key path.
	_, err := svc.buildUpstreamRequest(ctx, c, account, []byte(`{"model":"gpt-5.1","previous_response_id":"resp_victim","input":"continue"}`), "upstream", false, "", false)
	require.Error(t, err)
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, 10, "resp_mine", 1, 99))
	req, err := svc.buildUpstreamRequest(ctx, c, account, []byte(`{"model":"gpt-5.1","previous_response_id":"resp_mine","input":"continue"}`), "upstream", false, "", false)
	require.NoError(t, err)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "resp_mine")
}
