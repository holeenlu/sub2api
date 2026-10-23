package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func siwcWSFixture(t *testing.T, upstream HTTPUpstream) (*OpenAIGatewayService, *OpenAIOAuthService, *Account) {
	t.Helper()
	oauth, _, _, a := siwcFixture(t)
	authorizeSIWC(t, oauth, a)
	a.Status, a.Schedulable, a.Concurrency = StatusActive, true, 2
	a.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
	return &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, openAITokenProvider: NewOpenAITokenProvider(nil, nil, oauth), openaiWSResolver: NewOpenAIWSProtocolResolver(cfg)}, oauth, a
}
func siwcWSResponse(events ...string) *http.Response {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "data: %s\n\n", e)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(b.String()))}
}
func TestOpenAIWSSIWCBridgesFullToolHistory(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		siwcWSResponse(`{"type":"response.created","response":{"id":"resp_siwc_tool","status":"in_progress"}}`,
			`{"type":"response.completed","response":{"id":"resp_siwc_tool","status":"completed","output":[{"type":"reasoning","id":"rs_private","encrypted_content":"private-cipher"},{"type":"function_call","id":"fc_lookup","call_id":"call_lookup","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":4,"output_tokens":2}}}`),
		siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_siwc_answer","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":3}}}`)}}
	svc, _, a := siwcWSFixture(t, upstream)
	turns := make(chan bpsWSTestTurn, 2)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }}
	payload := `{"type":"response.create","model":"gpt-6.1-sol","input":"look up the answer","store":true,"stream":false,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]}`
	client, done := startOpenAIWSMemorySession(t, svc, a, nil, payload, hooks)
	defer client.CloseNow()
	events := readNativeWSTestTurn(t, client)
	first := <-turns
	require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
	require.NoError(t, first.err)
	require.Equal(t, UsageAuthorizationModeSIWC, first.result.AuthorizationMode)
	require.Equal(t, 4, first.result.Usage.InputTokens)
	require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("ChatGPT-Account-ID"))
	require.Empty(t, upstream.lastReq.Header.Get("Originator"))
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, "additional_tools", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "developer", gjson.GetBytes(upstream.lastBody, "input.0.role").String())
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","previous_response_id":"resp_siwc_tool","input":[{"type":"function_call_output","call_id":"call_lookup","output":"42"}]}`)))
	readNativeWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.Len(t, upstream.requests, 2)
	sent := upstream.bodies[1]
	require.False(t, gjson.GetBytes(sent, "previous_response_id").Exists())
	require.Contains(t, string(sent), "look up the answer")
	require.Contains(t, string(sent), "private-cipher")
	require.Contains(t, string(sent), "function_call_output")
	require.Equal(t, "lookup", gjson.GetBytes(sent, "input.0.tools.0.name").String())
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(sent, "model").String())
	requireNativeWSTestServerExit(t, client, done)
}
func TestOpenAIWSSIWCRejectsForeignConnectionAndChangedGrant(t *testing.T) {
	for _, change := range []string{"new connection", "new authorization"} {
		t.Run(change, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_owned","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)}
			svc, oauth, a := siwcWSFixture(t, upstream)
			turns := make(chan bpsWSTestTurn, 1)
			client, done := startOpenAIWSMemorySession(t, svc, a, nil, `{"type":"response.create","model":"gpt-6.1-sol","input":"private"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
			readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			continuation := `{"type":"response.create","model":"gpt-6.1-sol","previous_response_id":"resp_owned","input":"continue"}`
			if change == "new connection" {
				requireNativeWSTestServerExit(t, client, done)
				client, done = startOpenAIWSMemorySession(t, svc, a, nil, continuation, nil)
			} else {
				authorizeSIWC(t, oauth, a)
				require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(continuation)))
			}
			defer client.CloseNow()
			select {
			case err := <-done:
				require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
			case <-time.After(3 * time.Second):
				t.Fatal("unsafe continuation was not rejected")
			}
			require.Len(t, upstream.requests, 1, "unowned history must never reach an upstream")
		})
	}
}
func TestOpenAIWSSIWCReplayOwnerIncludesTenantKeyAccountAndGrant(t *testing.T) {
	svc, oauth, a := siwcWSFixture(t, &httpUpstreamRecorder{})
	makeContext := func(user, key, group int64) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
		c.Set("api_key", &APIKey{ID: key, GroupID: &group})
		SetOpenAIHTTPResponseOwner(c, user, key)
		return c
	}
	base, err := svc.openAIWSSIWCBridgeOwner(t.Context(), makeContext(1, 2, 3), a)
	require.NoError(t, err)
	for _, scope := range [][3]int64{{9, 2, 3}, {1, 8, 3}, {1, 2, 7}} {
		got, e := svc.openAIWSSIWCBridgeOwner(t.Context(), makeContext(scope[0], scope[1], scope[2]), a)
		require.NoError(t, e)
		require.NotEqual(t, base, got)
	}
	other := *a
	other.ID++
	authorizeSIWC(t, oauth, &other)
	otherOwner, err := svc.openAIWSSIWCBridgeOwner(t.Context(), makeContext(1, 2, 3), &other)
	require.NoError(t, err)
	require.NotEqual(t, base, otherOwner)
	authorizeSIWC(t, oauth, a)
	replaced, err := svc.openAIWSSIWCBridgeOwner(t.Context(), makeContext(1, 2, 3), a)
	require.NoError(t, err)
	require.NotEqual(t, base, replaced)
}
func TestOpenAIWSSIWCFailuresNeverReplayAndKeepNextTurnUsable(t *testing.T) {
	for _, kind := range []string{"failed after output", "failed before output", "EOF after output", "EOF before output", "incomplete"} {
		t.Run(kind, func(t *testing.T) {
			var events []string
			if kind != "EOF before output" {
				events = append(events, `{"type":"response.created","response":{"id":"resp_partial","status":"in_progress","usage":{"input_tokens":3,"output_tokens":1}}}`)
			}
			if strings.Contains(kind, "after output") {
				events = append(events, `{"type":"response.output_text.delta","delta":"visible"}`)
			}
			terminal := "response.failed"
			if strings.HasPrefix(kind, "failed") {
				events = append(events, `{"type":"response.failed","response":{"id":"resp_partial","status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","type":"rate_limit_error","message":"usage limit"},"usage":{"input_tokens":3,"output_tokens":1}}}`)
			}
			if kind == "incomplete" {
				terminal = "response.incomplete"
				events = append(events, `{"type":"response.incomplete","response":{"id":"resp_partial","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":3,"output_tokens":1}}}`)
			}
			if kind == "EOF before output" {
				terminal = "error"
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{siwcWSResponse(events...), siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_next","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)}}
			svc, _, a := siwcWSFixture(t, upstream)
			turns := make(chan bpsWSTestTurn, 2)
			client, done := startOpenAIWSMemorySession(t, svc, a, nil, `{"type":"response.create","model":"gpt-6.1-sol","input":"first"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
			defer client.CloseNow()
			received := readNativeWSTestTurn(t, client)
			first := <-turns
			require.Equal(t, terminal, gjson.GetBytes(received[len(received)-1], "type").String())
			require.Len(t, upstream.requests, 1)
			require.NotNil(t, first.result)
			require.True(t, first.result.wsReplayIncomplete)
			if kind != "EOF before output" {
				require.Equal(t, 3, first.result.Usage.InputTokens)
				require.Equal(t, 1, first.result.Usage.OutputTokens)
			}
			require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","input":"independent next turn"}`)))
			received = readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			require.Equal(t, "response.completed", gjson.GetBytes(received[len(received)-1], "type").String())
			require.Len(t, upstream.requests, 2)
			requireNativeWSTestServerExit(t, client, done)
		})
	}
}

type siwcWSCancelUpstream struct {
	httpUpstreamRecorder
	closed chan struct{}
}

func (u *siwcWSCancelUpstream) Do(req *http.Request, p string, a int64, c int) (*http.Response, error) {
	r, e := u.httpUpstreamRecorder.Do(req, p, a, c)
	if e == nil && len(u.requests) == 1 {
		r.Body = &httpBridgeIsolationBody{ctx: req.Context(), prefix: bytes.NewReader([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cancel\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")), suffix: bytes.NewReader(nil), release: make(chan struct{}), closed: u.closed}
	}
	return r, e
}
func (u *siwcWSCancelUpstream) DoWithTLS(req *http.Request, p string, a int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, a, c)
}
func TestOpenAIWSSIWCCancelReleasesUpstreamAndKeepsNextTurn(t *testing.T) {
	upstream := &siwcWSCancelUpstream{closed: make(chan struct{}), httpUpstreamRecorder: httpUpstreamRecorder{responses: []*http.Response{siwcWSResponse(), siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_after_cancel","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)}}}
	svc, _, a := siwcWSFixture(t, upstream)
	turns := make(chan bpsWSTestTurn, 2)
	client, done := startOpenAIWSMemorySession(t, svc, a, nil, `{"type":"response.create","model":"gpt-6.1-sol","input":"first"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
	defer client.CloseNow()
	for i := 0; i < 2; i++ {
		_, e := readPassthroughLifecycleFrame(t, client, 3*time.Second)
		require.NoError(t, e)
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.interrupt","response_id":"resp_cancel","mode":"discard_partial_items"}`)))
	failedInterrupt, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.interrupt.failed", gjson.GetBytes(failedInterrupt, "type").String())
	require.Equal(t, "unsupported_operation", gjson.GetBytes(failedInterrupt, "error.code").String())
	select {
	case <-upstream.closed:
		t.Fatal("unsupported interrupt must not cancel the SIWC stream")
	default:
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.cancel","response_id":"resp_cancel"}`)))
	events := readNativeWSTestTurn(t, client)
	first := <-turns
	require.Equal(t, "response.cancelled", gjson.GetBytes(events[len(events)-1], "type").String())
	require.NoError(t, first.err)
	require.Equal(t, 2, first.result.Usage.InputTokens)
	select {
	case <-upstream.closed:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close upstream response")
	}
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","input":"next"}`)))
	readNativeWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.Len(t, upstream.requests, 2)
	requireNativeWSTestServerExit(t, client, done)
}

func TestOpenAIWSSIWCSendOwnershipRequiresMatchingAtomicSnapshot(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_owned","status":"completed","output":[]}}`)}
	svc, oauth, a := siwcWSFixture(t, upstream)
	token, identity, err := oauth.SIWCAccessTokenWithIdentity(t.Context(), a, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	for _, scenario := range []string{"absent", "wrong token", "wrong account", "valid"} {
		ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(svc.cfg))
		selected := a
		if scenario != "absent" {
			boundToken := token
			if scenario == "wrong token" {
				boundToken = "different"
			}
			ctx = withSIWCRequestGrant(ctx, a, boundToken, identity)
		}
		if scenario == "wrong account" {
			changed := *a
			changed.ID++
			selected = &changed
		}
		req, e := svc.buildSIWCRequest(ctx, c, []byte(`{"model":"gpt-6.1-sol","input":"hi"}`), token)
		require.NoError(t, e)
		response, e := svc.doOpenAIUpstream(req, "", selected)
		if scenario != "valid" {
			require.Error(t, e)
			require.Nil(t, response)
			require.Empty(t, upstream.requests)
			continue
		}
		require.NoError(t, e)
		require.Len(t, upstream.requests, 1)
		target, ok := upstream.lastReq.Context().Value(turnRecoveryTargetKey{}).(turnRecoveryTarget)
		require.True(t, ok)
		require.Equal(t, "siwc", target.mode)
		require.Equal(t, a.ID, target.owner.account)
		require.Equal(t, sha256.Sum256([]byte("Bearer "+token+"\x00"+identity)), target.grant)
		require.NoError(t, response.Body.Close())
	}
}
func TestOpenAIWSSIWCRejectedRequestLeavesSocketUsable(t *testing.T) {
	for _, scenario := range []string{"unsupported tool", "HTTP quota rejection"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_next","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)}}
			input := `{"type":"response.create","model":"gpt-6.1-sol","input":"first","tools":[{"type":"tool_search"}]}`
			sent := 0
			code := "OPENAI_SIWC_UNSUPPORTED_TOOL"
			if scenario == "HTTP quota rejection" {
				input = `{"type":"response.create","model":"gpt-6.1-sol","input":"first"}`
				sent = 1
				code = "subscription_sharing_usage_limit_exceeded"
				upstream.responses = append([]*http.Response{{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"subscription_sharing_usage_limit_exceeded","type":"rate_limit_error","message":"usage unavailable"}}`))}}, upstream.responses...)
			}
			svc, _, a := siwcWSFixture(t, upstream)
			turns := make(chan bpsWSTestTurn, 2)
			client, done := startOpenAIWSMemorySession(t, svc, a, nil, input, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
			defer client.CloseNow()
			received := readNativeWSTestTurn(t, client)
			require.Error(t, (<-turns).err)
			require.Equal(t, "error", gjson.GetBytes(received[len(received)-1], "type").String())
			require.Equal(t, code, gjson.GetBytes(received[len(received)-1], "error.code").String())
			require.Len(t, upstream.requests, sent)
			require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","input":"valid next","tools":[]}`)))
			readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			require.Len(t, upstream.requests, sent+1)
			requireNativeWSTestServerExit(t, client, done)
		})
	}
}

func TestOpenAIWSSIWCErrorDiagnosticsPreserveHTTPAndSemanticStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, message  string
		httpStatus, semanticStatus int
		stream                     bool
	}{
		{"HTTP detail", `{"detail":"Workspace administrator denied subscription sharing"}`, "error", "Workspace administrator denied subscription sharing", 403, 403, false},
		{"HTTP status wins", `{"error":{"type":"rate_limit_error","code":"subscription_sharing_usage_limit_exceeded","message":"Provider unavailable","param":"model"}}`, "subscription_sharing_usage_limit_exceeded", "Provider unavailable", 503, 503, false},
		{"stream quota", `{"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"type":"rate_limit_error","code":"subscription_sharing_usage_limit_exceeded","message":"Plan budget exhausted","param":"model"},"usage":{"input_tokens":3,"output_tokens":1}}}`, "subscription_sharing_usage_limit_exceeded", "Plan budget exhausted", 200, 429, true},
		{"stream auth", `{"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"type":"authentication_error","code":"subscription_sharing_invalid_user","message":"Authorization revoked","param":"model"},"usage":{"input_tokens":3,"output_tokens":1}}}`, "subscription_sharing_invalid_user", "Authorization revoked", 200, 401, true},
		{"bare stream quota", `{"type":"error","error":{"type":"rate_limit_error","code":"subscription_sharing_usage_limit_exceeded","message":"Plan budget exhausted","param":"model"}}`, "subscription_sharing_usage_limit_exceeded", "Plan budget exhausted", 200, 429, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{StatusCode: tc.httpStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}
			if tc.stream {
				response = siwcWSResponse(tc.body)
			}
			response.Header.Set("x-request-id", "req_siwc_diagnostic")
			response.Header.Set("Retry-After", "17")
			upstream := &httpUpstreamRecorder{responses: []*http.Response{response, siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_next","status":"completed","output":[]}}`)}}
			svc, _, account := siwcWSFixture(t, upstream)
			turns := make(chan bpsWSTestTurn, 2)
			var diagnostics []OpsStreamError
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, `{"type":"response.create","model":"gpt-6.1-sol","input":"first"}`,
				&OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }},
				func(c *gin.Context) { diagnostics = GetOpsStreamErrors(c) })
			defer client.CloseNow()
			events := readNativeWSTestTurn(t, client)
			first := <-turns
			last := events[len(events)-1]
			code, _, message := parseOpenAIWSErrorEventFields(last)
			if tc.code != "error" {
				require.Equal(t, tc.code, code)
			}
			require.Equal(t, tc.message, message)
			prefix := "error"
			if gjson.GetBytes(last, "response.error").Exists() {
				prefix = "response.error"
			}
			if tc.code != "error" {
				require.Equal(t, "model", gjson.GetBytes(last, prefix+".param").String())
			}
			if !tc.stream {
				require.Equal(t, int64(tc.httpStatus), gjson.GetBytes(last, "status").Int())
				require.Error(t, first.err)
			}
			if strings.Contains(tc.name, "stream ") && strings.HasPrefix(tc.body, `{"type":"response.failed"`) {
				require.Equal(t, 3, first.result.Usage.InputTokens)
			}
			require.Len(t, upstream.requests, 1, "accepted failures cannot replay")
			require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","input":"next full request"}`)))
			readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			require.Len(t, upstream.requests, 2)
			requireNativeWSTestServerExit(t, client, done)
			require.Len(t, diagnostics, 1)
			failure := diagnostics[0]
			require.Equal(t, tc.httpStatus, failure.UpstreamStatus)
			require.Equal(t, tc.semanticStatus, failure.IntendedStatus)
			require.Equal(t, tc.code, failure.Code)
			require.Equal(t, tc.message, failure.Message)
			attempt := LastOpsUpstreamAttempt(failure.UpstreamErrors)
			require.NotNil(t, attempt)
			require.Equal(t, tc.httpStatus, attempt.UpstreamStatusCode)
			require.Equal(t, "req_siwc_diagnostic", attempt.UpstreamRequestID)
			require.Equal(t, "17", gjson.Get(attempt.Detail, "retry_after").String())
			if tc.code != "error" {
				require.Equal(t, "model", gjson.Get(attempt.Detail, "param").String())
			}
		})
	}
}

type siwcWSBudgetRepo struct {
	*siwcTestRepo
	exhaust bool
	cause   error
}

func (r *siwcWSBudgetRepo) GetSIWCRecord(ctx context.Context, a *Account) (string, string, error) {
	if r.exhaust {
		for i := 0; i <= selectionbudget.TurnLimit; i++ {
			if err := selectionbudget.Take(ctx, "postgres", 1); err != nil {
				r.cause = err
				return "", "", err
			}
		}
		return "", "", errors.New("credential preparation escaped aggregate budget")
	}
	select {
	case <-ctx.Done():
		r.cause = context.Cause(ctx)
		return "", "", r.cause
	case <-time.After(150 * time.Millisecond):
		return "", "", errors.New("credential preparation escaped turn deadline")
	}
}
func TestOpenAIWSSIWCPreparationUsesExistingTurnBudgets(t *testing.T) {
	for _, phase := range []string{"history owner", "atomic credential"} {
		for _, exhaust := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/aggregate=%t", phase, exhaust), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{}
				svc, oauth, a := siwcWSFixture(t, upstream)
				repo := &siwcWSBudgetRepo{siwcTestRepo: oauth.excelAccountRepo.(*siwcTestRepo), exhaust: exhaust}
				oauth.excelAccountRepo = repo
				budget := NewOpenAITurnBudget(svc.cfg)
				budget.deadline = time.Now().Add(20 * time.Millisecond)
				ctx := WithOpenAITurnBudget(t.Context(), budget)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
				var err error
				if phase == "history owner" {
					_, err = svc.openAIWSSIWCBridgeOwner(ctx, c, a)
				} else {
					payload := []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"test"}`)
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, a, "unused", payload, len(payload), "gpt-6.1-sol", "", "", "", "", 1, func([]byte) error { return nil })
				}
				if exhaust {
					require.ErrorIs(t, repo.cause, selectionbudget.ErrExhausted)
				} else {
					require.ErrorIs(t, repo.cause, ErrOpenAIWSFirstOutputBudgetExhausted)
				}
				require.Error(t, err) // Public credential errors intentionally hide storage causes.
				require.Empty(t, upstream.requests)
			})
		}
	}
}
