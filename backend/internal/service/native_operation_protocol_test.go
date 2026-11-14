package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPReviewMixedProtocolCannotChangePlannedFamily(t *testing.T) {
	response := "{\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1}}}"
	chat := "{\"id\":\"r\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":1}}"
	anthropic := "{\"type\":\"message_start\",\"message\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1}}}"
	for _, sample := range []struct {
		family  nativeUpstreamFamily
		own     string
		foreign []string
	}{
		{nativeUpstreamResponses, response, []string{chat, anthropic, "{\"type\":\"message_stop\"}"}},
		{nativeUpstreamChat, chat, []string{response, anthropic, "{\"type\":\"response.completed\",\"response\":{\"usage\":{\"output_tokens\":900}}}"}},
		{nativeUpstreamAnthropic, anthropic, []string{response, chat, "{\"choices\":[],\"usage\":{\"output_tokens\":900}}"}},
	} {
		t.Run(string(sample.family), func(t *testing.T) {
			d := nativeOperationDecoder{sse: true, kind: sample.family}
			for _, foreign := range sample.foreign {
				require.Empty(t, d.observePayload([]byte(foreign)), "a foreign first frame cannot choose the family")
				require.Equal(t, sample.family, d.kind)
			}
			own := d.observePayload([]byte(sample.own))
			require.NotNil(t, own.Usage)
			require.EqualValues(t, 1, own.Usage.Input.Value)
			for _, foreign := range sample.foreign {
				require.Empty(t, d.observePayload([]byte(foreign)))
				require.Equal(t, sample.family, d.kind)
				require.Equal(t, own.Usage, d.facts().Usage)
				require.Equal(t, model.TerminalUnknown, d.terminal)
			}
		})
	}
	unknown := nativeOperationDecoder{sse: true}
	require.Empty(t, unknown.observePayload([]byte(response)))
	require.Equal(t, nativeUpstreamUnknown, unknown.kind, "missing attempt metadata cannot be supplied by received payload")
}

func TestNativeHTTPReviewSealedWSObservationEntryIsResponsesOnly(t *testing.T) {
	d := nativeOperationDecoder{sse: true}
	require.Empty(t, d.payload([]byte("{\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":900}}")))
	require.Equal(t, nativeUpstreamResponses, d.kind)
	fact := d.payload([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1}}}"))
	require.Equal(t, model.UsageFinal, fact.Usage.Confidence)
	require.EqualValues(t, 1, fact.Usage.Input.Value)
}

func TestNativeHTTPReviewResponsesIngressUsesActualChatEncoder(t *testing.T) {
	body := []byte("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false}")
	f := newNativeOperationFixtureWithInput(t, true, model.ProtocolResponses, body, 3)
	f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
	f.resource.Credentials["api_protocol"] = APIProtocolChatCompletions
	f.resource.Extra = map[string]any{"openai_responses_supported": false}
	f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-4.1"}
	f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		require.Equal(t, nativeUpstreamChat, nativeUpstreamFamilyFromContext(req.Context()))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{\"id\":\"chat-r\",\"object\":\"chat.completion\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}"))}, nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))).WithContext(f.ctx)
	c.Set("api_key", f.key)
	err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		_, err := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) { return f.service.Forward(f.ctx, c, f.resource, body) })
		return NativeOperationStep{Err: err}
	})
	require.NoError(t, err)
	snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Equal(t, model.ProtocolResponses, snapshot.Protocol)
	require.Len(t, snapshot.Attempts, 1)
	require.Equal(t, model.UsageFinal, snapshot.Attempts[0].Usage.Confidence)
	require.EqualValues(t, 2, snapshot.Attempts[0].Usage.Output.Value)
}

func TestNativeHTTPReviewReprepareKeepsActualEncoderFamily(t *testing.T) {
	wire := "{\"id\":\"chat-r\",\"object\":\"chat.completion\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}"
	f, owner, _ := nativeHTTPObservedFixture(t, wire, "application/json", nativeUpstreamChat)
	prepared, target, err := f.op.prepare(f.request(t), f.resource, "", false)
	require.NoError(t, err)
	defer func() { require.NoError(t, prepared.Close()) }()
	state := model.SendNotSent
	port := &nativeHTTPPreparedAttempt{owner: owner, dispatch: f.op, prepared: prepared, target: target, state: &state, family: nativeUpstreamChat}
	changedContext := withNativeUpstreamFamily(f.ctx, nativeUpstreamResponses)
	retry, err := port.Reprepare(changedContext)
	require.NoError(t, err)
	defer func() { require.NoError(t, retry.Close()) }()
	retries := 0
	opened, _, err := owner.core.OpenPrepared(changedContext, retry, &retries, nil)
	require.NoError(t, err)
	actual, ok := opened.(*nativeHTTPPreparedAttempt)
	require.True(t, ok)
	require.Equal(t, nativeUpstreamChat, actual.family)
	body, err := io.ReadAll(actual.response.Body)
	require.NoError(t, err)
	require.Equal(t, wire, string(body))
	require.NoError(t, actual.response.Body.Close())
	require.EqualValues(t, 2, owner.core.Snapshot().Attempts[0].Usage.Output.Value)
	require.Equal(t, model.UsageFinal, owner.core.Snapshot().Attempts[0].Usage.Confidence)
	require.EqualValues(t, 1, f.transport.sends.Load())
	var _ execution.PreparedAttempt = actual
}
