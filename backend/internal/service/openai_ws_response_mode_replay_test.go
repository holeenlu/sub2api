//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Reuse the staged protocol peer with the explicit handshake observation now
// required by the real Native dispatcher. No external services or real
// credentials are used.
type responseModeReplayWSConn struct {
	*nativeCodexStagedConn
	target  string
	headers http.Header
}

func (c *responseModeReplayWSConn) takeNativeHandshakeRequest() (string, http.Header) {
	return c.target, c.headers
}

func (c *responseModeReplayWSConn) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	payload, err := write.Begin(ctx)
	if err != nil {
		return model.SendNotSent, err
	}
	if err := write.RevalidateWrite(ctx); err != nil {
		return model.SendNotSent, err
	}
	if err := c.WriteFrame(ctx, coderws.MessageType(write.MessageType()), payload); err != nil {
		return model.SendNotSent, err
	}
	return model.SendSent, nil
}

type responseModeReplayState struct {
	session.Bindings
	readErr error
}

func (s *responseModeReplayState) GetResponseMode(ctx context.Context, group int64, id string) (session.ResponseMode, error) {
	if id == "resp_producer" && s.readErr != nil {
		return session.ResponseModeUnknown, s.readErr
	}
	return s.Bindings.GetResponseMode(ctx, group, id)
}

func responseModeReplayCreate(stream, event, model, previous, input string) []byte {
	return []byte(fmt.Sprintf(`{"type":"response.create","stream_id":%q,"event_id":%q,"model":%q,"previous_response_id":%q,"input":%q,"stream":true}`, stream, event, model, previous, input))
}

func responseModeReplayCompleted(id, model, text string) []byte {
	return []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","model":%q,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}],"usage":{"input_tokens":2,"output_tokens":1}}}`, id, model, text))
}

type responseModeReplayCase struct {
	producer, consumer session.ResponseMode
	first              bool
	missing            bool
	stateErr           error
}

type responseModeReplayBPSAccounts struct {
	*nativeWSConcurrentAccounts
	service *OpenAIGatewayService
}

func (r *responseModeReplayBPSAccounts) ValidateVendorAuthorizationMode(ctx context.Context, account *Account, group *int64, mode string) error {
	if mode != VendorOpenAIModeBPS {
		return r.nativeWSConcurrentAccounts.ValidateVendorAuthorizationMode(ctx, account, group, mode)
	}
	fresh, err := r.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	if group == nil || !slices.Contains(fresh.GroupIDs, *group) || !fresh.IsSchedulable() || !fresh.IsExcelBPSEnabled() {
		return ErrNativeDispatchScopeChanged
	}
	return r.ValidateVendorSupply(ctx, fresh, group)
}

func (r *responseModeReplayBPSAccounts) ValidateExcelBPSDispatch(ctx context.Context, snapshot *Account, group *int64, upstreamModel string) error {
	if err := r.ValidateVendorAuthorizationMode(ctx, snapshot, group, VendorOpenAIModeBPS); err != nil {
		return err
	}
	fresh, err := r.GetByID(ctx, snapshot.ID)
	if err != nil {
		return err
	}
	ciphertext, _, err := r.service.openAITokenProvider.openAIOAuthService.readExcelGrant(ctx, fresh)
	fingerprint, bound := ExcelBPSDispatchFingerprint(ctx, snapshot.ID)
	if err != nil || !bound || fingerprint != ExcelGrantFingerprint(ciphertext) || !fresh.isExcelBPSUpstreamModelEnabled(upstreamModel) || recoveryOwner(fresh) != recoveryOwner(snapshot) {
		return errors.Join(err, ErrNativeDispatchScopeChanged)
	}
	return nil
}

type responseModeReplayBPSJournal struct {
	*nativeApplicationJournal
	bps responseBPSJournalFixture
}

func (j *responseModeReplayBPSJournal) RecordBPSUsageEvidence(ctx context.Context, in BPSUsageJournalInput) (BPSUsageJournalResult, error) {
	return j.bps.RecordBPSUsageEvidence(ctx, in)
}

// Only the completed producer publication is synthetic: it goes through the
// actual multiplex writer/history with the same account, key and credential
// generation. The consumer executes the service's real first/subsequent ingress
// path, including Native permits and finalization through the existing fixture.
// These isolated peers do not claim real-provider or persistent-billing coverage.
func runResponseModeReplay(t *testing.T, ingressMode string, tc responseModeReplayCase) {
	t.Helper()
	modelFor := func(mode session.ResponseMode) string {
		if mode == session.ResponseModeBPS {
			return "gpt-6-astra"
		}
		return "gpt-6-sol"
	}
	consumerModel := modelFor(tc.consumer)
	peer := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(http.StatusOK, "data: "+string(responseModeReplayCompleted("resp_consumer_seed", consumerModel, "seed-answer"))+"\n\n"),
		bpsCompletionResponse(http.StatusOK, "data: "+string(responseModeReplayCompleted("resp_consumer_fork", consumerModel, "fork-answer"))+"\n\n"),
	}}
	svc := excelBPSGatewayTestService(peer)
	svc.cfg = passthroughLifecycleConfig()
	svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	svc.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
	account := excelBPSDualGrantTestAccount(t, svc)
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = ingressMode
	svc.openaiWSResolver = NewOpenAIWSProtocolResolver(svc.cfg)
	svc.toolCorrector = NewCodexToolCorrector()
	planning := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	for _, mode := range []session.ResponseMode{tc.producer, tc.consumer} {
		plan, err := svc.planResponseExecution(planning, account, modelFor(mode))
		require.NoError(t, err)
		require.Equal(t, mode, plan.mode, "mixed-model selection must use this account's actual BPS policy")
	}
	require.False(t, excelBPSExecutionPinned(planning), "planning must not pin a different turn's budget")

	// BPS consumes the same authenticated root, real capacity receipts and
	// synchronous finalizer. Its independent Excel grant and journal remain
	// the BPS owners; a bare Proxy call is no longer a public dispatch fixture.
	bpsProvider := svc.openAITokenProvider
	nativeFixture := newNativeHTTPBridgeFixture(t, svc, account)
	key := nativeFixture.key
	if tc.consumer == session.ResponseModeBPS {
		svc.openAITokenProvider = bpsProvider
		svc.accountRepo = &responseModeReplayBPSAccounts{nativeWSConcurrentAccounts: svc.accountRepo.(*nativeWSConcurrentAccounts), service: svc}
		journal := &responseModeReplayBPSJournal{nativeApplicationJournal: svc.usageBillingRepo.(*nativeApplicationJournal)}
		svc.usageBillingRepo = journal
		nativeFixture.transport.reply = func(req *http.Request, options native.TransportOptions) (*http.Response, error) {
			if options.Profile != native.ProfileExcelBPS || req.URL.String() != basispoints.ResponsesURL {
				return nil, errors.New("BPS replay reached a different mode or endpoint")
			}
			owner := nativeHTTPFromContext(req.Context())
			journal.bps.mu.Lock()
			_, preserved := journal.bps.preserved[owner.dispatch.request.OperationID][owner.active.Load()]
			journal.bps.mu.Unlock()
			if !preserved {
				return nil, errors.New("BPS replay reached the peer without its registered pre-send evidence")
			}
			return peer.Do(req, options.ProxyURL, options.AccountID, options.AccountConcurrency)
		}
	}
	groupID := *key.GroupID
	store := &responseModeReplayState{Bindings: session.NewStateStore(nil), readErr: tc.stateErr}
	svc.openaiWSStateStore = store
	if !tc.missing {
		require.NoError(t, store.BindResponseMode(t.Context(), groupID, "resp_producer", tc.producer, time.Hour))
		require.NoError(t, store.BindHTTPResponseOwner(t.Context(), groupID, "resp_producer", key.UserID, key.ID, time.Hour))
	}
	if tc.missing {
		mode, err := store.GetResponseMode(t.Context(), groupID, "resp_producer")
		require.NoError(t, err)
		require.Equal(t, session.ResponseModeUnknown, mode, "all shared indexes are absent; only the local completed history proves ownership")
	}

	upstream := newStagedPassthroughConn()
	dialer := wsLifecycleDialer(func(_ context.Context, target string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
		return &responseModeReplayWSConn{nativeCodexStagedConn: &nativeCodexStagedConn{upstream}, target: target, headers: headers.Clone()}, http.StatusSwitchingProtocols, http.Header{}, nil
	})
	svc.openaiWSPassthroughDialer = dialer
	pool := newOpenAIWSConnPool(svc.cfg)
	pool.setClientDialerForTest(dialer)
	svc.openaiWSPool = pool
	t.Cleanup(pool.Close)
	t.Cleanup(func() { _ = upstream.Close() })
	usesNativeWS := tc.consumer == session.ResponseModeNative && ingressMode != OpenAIWSIngressModeHTTPBridge

	producerReady := make(chan error, 1)
	consumerDone := make(chan error, 1)
	turns := make(chan bpsWSTestTurn, 4)
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		_, first, err := ds.Read(ctx)
		if err != nil {
			consumerDone <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
		c.Set("api_key", key)
		MarkExcelBPSImageAdmission(c)
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
		if gjson.GetBytes(first, "stream_id").String() == "producer" {
			if err = ds.bindRequestEnvelope(first); err == nil {
				_, err = ds.prepareMultiplexTurn(ctx, c, account, first)
			}
			if err == nil {
				err = ds.Write(ctx, coderws.MessageText, responseModeReplayCompleted("resp_producer", modelFor(tc.producer), "producer-answer"))
			}
			ds.completeMultiplexTurn()
			producerReady <- err
			return
		}
		hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
			turns <- bpsWSTestTurn{result: result, err: err}
		}}
		err = nativeFixture.proxy(ctx, c, ds, account, first, hooks)
		consumerDone <- err
	})
	// Also unblock the peer before the shared fixture joins its workers when
	// an assertion catches an unwanted send without supplying a response.
	t.Cleanup(func() { _ = conn.CloseNow(); _ = upstream.Close() })
	conn.input <- responseModeReplayCreate("producer", "source", modelFor(tc.producer), "", "producer-prompt")
	require.NoError(t, wsLifecycleReceive(t, producerReady))
	require.Equal(t, "resp_producer", gjson.GetBytes(wsLifecycleReceive(t, conn.writes), "response.id").String())

	readCompletedTurn := func(id string) {
		t.Helper()
		turn := wsLifecycleReceive(t, turns)
		require.NoError(t, turn.err)
		require.NotNil(t, turn.result)
		for {
			frame := wsLifecycleReceive(t, conn.writes)
			require.Equal(t, "consumer", gjson.GetBytes(frame, "stream_id").String())
			require.NotEqual(t, "error", gjson.GetBytes(frame, "type").String(), "%s", frame)
			if gjson.GetBytes(frame, "type").String() == "response.completed" {
				require.Equal(t, id, gjson.GetBytes(frame, "response.id").String())
				return
			}
		}
	}
	if !tc.first {
		conn.input <- responseModeReplayCreate("consumer", "seed", consumerModel, "", "consumer-seed")
		if usesNativeWS {
			first := requirePassthroughUpstreamWrite(t, upstream, 2*time.Second)
			require.Equal(t, consumerModel, gjson.GetBytes(first, "model").String())
			upstream.Send(string(responseModeReplayCompleted("resp_consumer_seed", consumerModel, "seed-answer")))
		}
		readCompletedTurn("resp_consumer_seed")
	}
	requestsBeforeFork := len(peer.requests)
	if tc.first || usesNativeWS {
		require.Zero(t, requestsBeforeFork)
	} else {
		require.Equal(t, 1, requestsBeforeFork, "the consumer must finish a real first request before the cross-stream followup")
	}
	conn.input <- responseModeReplayCreate("consumer", "fork", consumerModel, "resp_producer", "fork-prompt")
	allowed := tc.producer == tc.consumer && !tc.missing && tc.stateErr == nil
	if !allowed {
		var err error
		select {
		case err = <-consumerDone:
		case frame := <-upstream.writes:
			t.Fatalf("cross-mode or unavailable producer history reached upstream: %s", frame)
		case <-time.After(2 * time.Second):
			t.Fatal("producer-mode rejection did not return from the actual ingress")
		}
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure, "must reject the retained producer mode, not an unrelated fixture/authorization failure")
		status, reason := http.StatusBadRequest, "continuation_mode_unavailable"
		if tc.stateErr != nil {
			status, reason = http.StatusServiceUnavailable, "continuation_state_unavailable"
			require.ErrorIs(t, err, tc.stateErr)
			require.NotErrorIs(t, err, ErrOpenAIContinuationUnavailable)
			require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
		} else {
			require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
			require.Equal(t, GatewayFailureScopeRequest, failure.Scope)
			if tc.missing {
				require.ErrorIs(t, err, session.ErrResponseModeUnavailable)
			}
		}
		require.Equal(t, status, failure.StatusCode)
		require.Equal(t, status, failure.ClientStatusCode)
		require.EqualValues(t, reason, failure.Reason)
		require.True(t, failure.RequestNotSent)
		require.Equal(t, NextAccountStop, failure.NextAccountAction)
		require.False(t, failure.ShouldRetryNextAccount())
		require.Len(t, peer.requests, requestsBeforeFork, "rejected history must not create another HTTP request or replay")
		require.Empty(t, upstream.writes, "rejected history must not reach the native WS peer")
		return
	}

	var replay []byte
	if usesNativeWS {
		replay = requirePassthroughUpstreamWrite(t, upstream, 2*time.Second)
		upstream.Send(string(responseModeReplayCompleted("resp_consumer_fork", consumerModel, "fork-answer")))
	}
	readCompletedTurn("resp_consumer_fork")
	if !usesNativeWS {
		require.Len(t, peer.requests, requestsBeforeFork+1)
		replay = peer.lastBody
	}
	require.False(t, gjson.GetBytes(replay, "previous_response_id").Exists(), "same-mode fork must reconstruct the completed source history")
	require.Equal(t, consumerModel, gjson.GetBytes(replay, "model").String())
	require.Contains(t, string(replay), "producer-prompt")
	require.Contains(t, string(replay), "producer-answer")
	require.Contains(t, string(replay), "fork-prompt")
	require.NotContains(t, string(replay), "consumer-seed", "fork must use the producer's history, not the consumer's previous turn")
	if tc.consumer == session.ResponseModeBPS {
		for _, request := range peer.requests {
			require.Equal(t, "/basispoints/api/responses", request.URL.Path)
		}
	}
	require.NoError(t, conn.CloseNow())
	require.NoError(t, wsLifecycleReceive(t, consumerDone))
	require.Empty(t, upstream.writes, "completed fork must not trigger another replay")
}

func TestOpenAIWSResponseModeReplayNativeLaterTurn(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			t.Run("bps_to_native_rejected", func(t *testing.T) {
				runResponseModeReplay(t, mode, responseModeReplayCase{producer: session.ResponseModeBPS, consumer: session.ResponseModeNative})
			})
			t.Run("native_completed_fork", func(t *testing.T) {
				runResponseModeReplay(t, mode, responseModeReplayCase{producer: session.ResponseModeNative, consumer: session.ResponseModeNative})
			})
		})
	}
}

func TestOpenAIWSResponseModeReplayBPSLaterTurn(t *testing.T) {
	t.Run("native_to_bps_rejected", func(t *testing.T) {
		runResponseModeReplay(t, OpenAIWSIngressModeHTTPBridge, responseModeReplayCase{producer: session.ResponseModeNative, consumer: session.ResponseModeBPS})
	})
	t.Run("bps_completed_fork", func(t *testing.T) {
		runResponseModeReplay(t, OpenAIWSIngressModeHTTPBridge, responseModeReplayCase{producer: session.ResponseModeBPS, consumer: session.ResponseModeBPS})
	})
}

func TestOpenAIWSResponseModeReplayMissingProducerState(t *testing.T) {
	// Both parsers rewrite the cross-lane reference before their owner/mode
	// checks. Missing shared indexes must not make that known local output an
	// external unknown response. Passthrough separately checks ownership first.
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			runResponseModeReplay(t, mode, responseModeReplayCase{producer: session.ResponseModeBPS, consumer: session.ResponseModeNative, missing: true})
		})
	}
	t.Run("dependency_is_503", func(t *testing.T) {
		runResponseModeReplay(t, OpenAIWSIngressModeCtxPool, responseModeReplayCase{producer: session.ResponseModeBPS, consumer: session.ResponseModeNative, stateErr: errors.New("producer mode store unavailable")})
	})
}

func TestOpenAIWSResponseModeReplayFirstTurn(t *testing.T) {
	for _, tc := range []struct {
		name               string
		producer, consumer session.ResponseMode
	}{
		{"bps_to_native_rejected", session.ResponseModeBPS, session.ResponseModeNative},
		{"native_to_bps_rejected", session.ResponseModeNative, session.ResponseModeBPS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runResponseModeReplay(t, OpenAIWSIngressModeHTTPBridge, responseModeReplayCase{producer: tc.producer, consumer: tc.consumer, first: true})
		})
	}
}
