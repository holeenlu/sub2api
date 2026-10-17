package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokVoiceURL_UsesAPIDefaultForCLIProxyBase(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": xai.DefaultCLIBaseURL,
		},
	}
	url, err := buildGrokVoiceURL(account, nil, "tts")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/tts", url)

	url, err = buildGrokVoiceURL(account, nil, "realtime")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/realtime", url)
}

func TestBuildGrokVoiceURL_EmptyBaseFallsBackToAPI(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
	}
	url, err := buildGrokVoiceURL(account, nil, "stt")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/stt", url)
}

func TestBuildGrokVoiceURL_RequiresEndpoint(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	_, err := buildGrokVoiceURL(account, nil, "  ")
	require.Error(t, err)
}

func TestBuildGrokVoiceURL_EncodesCustomVoicePathSegments(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	got, err := buildGrokVoiceURL(account, nil, "custom-voices/nlbqfwie/audio")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/custom-voices/nlbqfwie/audio", got)

	_, err = buildGrokVoiceURL(account, nil, "custom-voices/../audio")
	require.Error(t, err)
}

func TestForwardGrokVoice_RejectsNonGrok(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformOpenAI}, "tts", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestAwaitGrokRealtimeAudioObservedReadsFlagAfterRelayExits(t *testing.T) {
	errCh := make(chan error, 1)
	var observed atomic.Bool
	go func() {
		observed.Store(true)
		errCh <- io.EOF
	}()
	got, err := awaitGrokRealtimeAudioObserved(errCh, &observed)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, got, "audioObserved must be read after the relay returns, not before <-errCh")
}

func TestGrokRealtimeEventHasAudio(t *testing.T) {
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"session.created"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hi"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":""}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"abc"}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.output_audio.delta","audio":"abc"}`)))
}

func TestForwardGrokVoice_RejectsUnknownEndpoint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformGrok}, "unknown", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

type securityMediaRepo struct {
	AccountRepository
	mu     sync.Mutex
	voices map[string]GatewayMediaVoice
}

func newSecurityMediaRepo() *securityMediaRepo {
	return &securityMediaRepo{voices: map[string]GatewayMediaVoice{}}
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

func securityVoiceContext(user, key int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/tts", nil)
	g := int64(10)
	c.Set("api_key", &APIKey{ID: key, UserID: user, GroupID: &g})
	return c
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

func TestSecurityCustomVoiceOwnerAllReferences(t *testing.T) {
	repo := newSecurityMediaRepo()
	svc := &OpenAIGatewayService{accountRepo: repo}
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

func TestSecurityCustomVoiceCRUDAndPersistence(t *testing.T) {
	repo := newSecurityMediaRepo()
	upstream := &securityMediaHTTP{response: `{"voice_id":"created","name":"mine"}`}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
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
