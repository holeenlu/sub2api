package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudCloseWaitsForAdmittedConnectionToExit(t *testing.T) {
	store := newCloudLiveRoutingStore()
	entered, release := make(chan struct{}), make(chan struct{})
	sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		close(entered)
		<-release
		return nil, context.Canceled
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	request := OpenAICloudRequest{Owner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: "shutdown-root", BillingSnapshot: json.RawMessage(`{"frozen":true}`)}
	finished := make(chan error, 1)
	go func() { _, err := sessions.Run(t.Context(), request, nil); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initialization not entered")
	}
	stopped := make(chan struct{})
	go func() { sessions.Close(); close(stopped) }()
	early := false
	select {
	case <-stopped:
		early = true
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("close did not finish after admitted work ended")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request did not return")
	}
	require.False(t, early, "shutdown must not report complete while a live owner can still write terminal facts")
}

func TestCloudShutdownFailureKeepsLocalScopeAndNeverRotatesAccounts(t *testing.T) {
	status, code, _, known := cloudPublicError(ErrOpenAICloudClosing)
	require.True(t, known)
	require.Equal(t, 503, status)
	require.Equal(t, "cloud_gateway_stopping", code)
	require.True(t, (&cloudGatewayError{code: code}).RequestScoped(), "forwarding must exclude this local terminal from account health")
	var scoped interface{ RequestScoped() bool }
	require.ErrorAs(t, ErrOpenAICloudClosing, &scoped)
	require.True(t, scoped.RequestScoped())
	facts, upstreamStatus := cloudTerminalFailureFacts(&OpenAICloudResult{Terminal: true, ErrorStatus: 503, Response: json.RawMessage(`{"status":"failed","error":{"code":"cloud_gateway_stopping","upstream_method":"turn/completed","upstream_turn_status":"interrupted"}}`)})
	require.Empty(t, facts)
	require.Zero(t, upstreamStatus, "shutdown interruption is not an upstream account fault")

	require.Nil(t, cloudPreInferenceFailover(context.Background(), []byte(`{"model":"m","input":"hi"}`), status, code, "stopping", ErrOpenAICloudClosing, true))
	svc, input, store, _ := cloudForwardFixture(t, nil)
	require.NoError(t, svc.cloudSessions.Stop(t.Context()))
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	c.Set("api_key", input.APIKey)
	SetOpenAIHTTPResponseOwner(c, input.User.ID, input.APIKey.ID)
	_, err = svc.forwardOpenAICloud(ctx, c, input.Account, []byte(`{"model":"client-model","input":"hi"}`))
	require.ErrorIs(t, err, ErrOpenAICloudClosing)
	require.Equal(t, 503, rec.Code)
	require.Contains(t, rec.Body.String(), "cloud_gateway_stopping")
	require.Zero(t, store.settlementCount())
}

func TestCloudHistoricalCleanupFailureDoesNotPoisonLaterDrain(t *testing.T) {
	sessions := NewOpenAICloudSessions(nil, nil, nil)
	sessions.mu.Lock()
	sessions.addPendingLocked()
	sessions.finishPendingLocked(errors.New("old request cleanup failed"))
	sessions.mu.Unlock()
	require.NoError(t, sessions.Stop(t.Context()))
}

type cloudShutdownPublishStore struct{ *cloudActorTestStore }

func (s *cloudShutdownPublishStore) PublishCloudEvent(ctx context.Context, id string, event OpenAICloudStreamEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.cloudActorTestStore.PublishCloudEvent(ctx, id, event)
}
func TestCloudShutdownCanPublishPersistedTerminalAfterOwnerCancellation(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.service.store = &cloudShutdownPublishStore{store}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	cancel()
	a.record.Status = "interrupted"
	a.lowerBound = true
	a.setPublicFailure(ErrOpenAICloudClosing)
	require.NoError(t, a.finish("failed", false, true, a.record.Usage, nil))
	require.Len(t, store.settlements, 1)
	require.Len(t, store.results, 2, "terminal event and its durable result still reach another ingress instance")
	require.Contains(t, string(store.results[0].Event), "cloud_gateway_stopping")
	a.requestID = "next"
	require.ErrorIs(t, a.emit("response.output_text.delta", map[string]any{"delta": "must not send"}), context.Canceled, "only a saved terminal may outlive owner cancellation")
}

func TestCloudNilRuntimeStillReturnsNonDispatchError(t *testing.T) {
	var s *OpenAICloudSessions
	_, err := s.Run(t.Context(), OpenAICloudRequest{}, nil)
	require.ErrorIs(t, err, ErrOpenAICloudNotStarted)
	require.ErrorIs(t, err, errCloudStateUnavailable)
}
