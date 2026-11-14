package service

import (
	"context"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// The staging window expired and published response.created (identity only),
// then the upstream closed with 1012 before any content. The turn is retried
// once on a new connection; the client keeps the identity it already saw: no
// second created, the recovered attempt's events are presented under the
// published response.id, and the next turn's previous_response_id is mapped
// back to the upstream identity.
func TestOpenAIWSIngressIdentityOnlyPublicationRetriesOnceUnderPublishedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.StreamDataIntervalTimeout = 3
	first := &openAIWSCaptureConn{
		events:     [][]byte{[]byte(`{"type":"codex.rate_limits"}`), []byte(`{"type":"response.created","response":{"id":"resp_public"}}`)},
		readDelays: []time.Duration{0, 0},
	}
	// After created, block long enough for the 1 s staging window to publish
	// it, then restart (1012) with nothing else delivered.
	firstConn := &openAIWSRestartFailureConn{first}
	second := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"codex.rate_limits"}`),
		[]byte(`{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"second"}}`),
		[]byte(`{"type":"response.created","response":{"id":"resp_upstream"}}`),
		[]byte(`{"type":"response.in_progress","response":{"id":"resp_upstream"}}`),
		[]byte(`{"type":"response.output_text.delta","response_id":"resp_upstream","item_id":"msg_1","delta":"recovered"}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_upstream","usage":{"input_tokens":8,"output_tokens":4}}}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_second_turn","usage":{"input_tokens":2,"output_tokens":1}}}`),
	}}
	first.readDelays = []time.Duration{0, 0}
	dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{&openAIWSDelayedEOFConn{openAIWSClientConn: firstConn, delay: 1600 * time.Millisecond}, second}}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
	account := &Account{ID: 923451, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "test-token"}, Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true}}
	var billed []string
	var stopEvents []*OpsUpstreamErrorEvent
	var finalOps []OpsStreamError
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
		if result != nil {
			billed = append(billed, result.RequestID)
		}
	}}
	client, errs := startOpenAIWSMemorySession(t, svc, account, nil, `{"type":"response.create","model":"gpt-6.1-sol","store":false,"input":[{"role":"user","content":"first"}]}`, hooks, func(c *gin.Context) {
		finalOps = GetOpsStreamErrors(c)
		if raw, ok := c.Get(OpsUpstreamErrorsKey); ok {
			stopEvents, _ = raw.([]*OpsUpstreamErrorEvent)
		}
	})
	defer client.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var types []string
	var created, deltaID, completedID string
	for {
		_, b, err := client.Read(ctx)
		require.NoError(t, err)
		typ := gjson.GetBytes(b, "type").String()
		types = append(types, typ)
		switch typ {
		case "response.created":
			require.Empty(t, created, "the client must see exactly one created: %v", types)
			created = gjson.GetBytes(b, "response.id").String()
		case "response.output_text.delta":
			deltaID = gjson.GetBytes(b, "response_id").String()
		case "response.completed":
			completedID = gjson.GetBytes(b, "response.id").String()
		}
		if typ == "response.completed" {
			break
		}
	}
	require.Equal(t, "resp_public", created, "events: %v", types)
	require.Equal(t, "resp_public", deltaID, "recovered content is presented under the published identity")
	require.Equal(t, "resp_public", completedID)
	require.Equal(t, 1, countStrings(types, "codex.rate_limits"), "preamble already published must not repeat: %v", types)
	require.Equal(t, 1, countStrings(types, "response.created"))
	require.Equal(t, 2, dialer.DialCount(), "one identity-preserving reconnect")

	// Second turn continues from the ID the client knows; upstream receives
	// the identity that actually produced the previous response.
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6.1-sol","store":false,"previous_response_id":"resp_public","input":[{"role":"user","content":"second"}]}`)))
	for {
		_, b, err := client.Read(ctx)
		require.NoError(t, err)
		if gjson.GetBytes(b, "type").String() == "response.completed" {
			require.Equal(t, "resp_second_turn", gjson.GetBytes(b, "response.id").String())
			break
		}
	}
	_ = client.CloseNow()
	select {
	case <-errs:
	case <-time.After(4 * time.Second):
	}
	second.mu.Lock()
	defer second.mu.Unlock()
	require.GreaterOrEqual(t, len(second.writes), 2, "second connection carries the retried turn and the follow-up turn")
	followUp := second.writes[len(second.writes)-1]
	require.Equal(t, "resp_upstream", followUp["previous_response_id"], "client alias is mapped to the upstream identity")
	// Hooks run after each turn settles; read them once the session has ended.
	require.Equal(t, []string{"resp_upstream", "resp_second_turn"}, billed, "billing follows the upstream responses that actually produced the turns")
	require.Empty(t, finalOps)
	for _, ev := range stopEvents {
		require.NotEqual(t, string(OpenAIRetryStopDownstreamCommitted), ev.Reason, "an identity-only publication is not a committed turn")
	}
}

// openAIWSDelayedEOFConn blocks the read that would return EOF/close for the
// given delay, so the ingress staging window can expire first.
type openAIWSDelayedEOFConn struct {
	openAIWSClientConn
	delay time.Duration
}

func (c *openAIWSDelayedEOFConn) ReadMessage(ctx context.Context) ([]byte, error) {
	b, err := c.openAIWSClientConn.ReadMessage(ctx)
	if err == nil {
		return b, nil
	}
	timer := time.NewTimer(c.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return nil, err
}

func (c *openAIWSDelayedEOFConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	b, err := c.ReadMessage(ctx)
	return coderws.MessageText, b, err
}

func countStrings(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}
