package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A client gone mid-prewarm ends the connection on its next read, as during a
// bridged turn; any other write failure fails the turn.
func TestAnswerOpenAIWSHTTPBridgePrewarmWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failAt  int
		err     error
		wantErr bool
	}{
		{name: "disconnect", failAt: 1, err: io.EOF},
		{name: "write timeout", failAt: 2, err: context.DeadlineExceeded, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			result, err := answerOpenAIWSHTTPBridgePrewarm(1, "gpt-5.1", 1, func([]byte) error {
				writes++
				if writes == tc.failAt {
					return tc.err
				}
				return nil
			})
			require.Equal(t, tc.failAt, writes)
			if !tc.wantErr {
				require.NoError(t, err)
				require.True(t, result.LocalPrewarm)
				return
			}
			require.Nil(t, result)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			var turnErr *openAIWSIngressTurnError
			require.ErrorAs(t, err, &turnErr)
			require.Equal(t, "write_client", turnErr.stage)
			require.True(t, turnErr.wroteDownstream, "response.created already reached the client")
		})
	}
}

// A prewarm sends no upstream request, but a function-tool bridge still has to
// record the turn's lowered tools: the next turn may omit tools and inherit
// them, exactly as after a generated first turn. Explicit tools:[] clears them.
func TestOpenAIWSHTTPBridgePrewarmKeepsToolState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_next\",\"model\":\"gpt-5\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
	firstTurn := `{"type":"response.create","model":"gpt-5","stream":true,"tools":[{"type":"custom","name":"exec","description":"Run a command"}],"input":"run pwd"%s}`
	for _, tc := range []struct {
		name, first, next string
		wantTool          bool
	}{
		{name: "generated first turn", first: strings.Replace(firstTurn, "%s", "", 1), next: `{"type":"response.create","model":"gpt-5","stream":true,"input":"again"}`, wantTool: true},
		{name: "prewarm first turn", first: strings.Replace(firstTurn, "%s", `,"generate":false`, 1), next: `{"type":"response.create","model":"gpt-5","stream":true,"input":"again"}`, wantTool: true},
		{name: "prewarm then explicit clear", first: strings.Replace(firstTurn, "%s", `,"generate":false`, 1), next: `{"type":"response.create","model":"gpt-5","stream":true,"tools":[],"input":"again"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(completed))},
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(completed))},
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}, httpUpstream: upstream}
			account := &Account{ID: 5765, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			write := func([]byte) error { return nil }
			for turn, frame := range []string{tc.first, tc.next} {
				_, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", []byte(frame), len(frame), "gpt-5", false, "", "", "", "", turn+1, write)
				require.NoError(t, err)
			}
			next := upstream.lastBody
			require.Equal(t, "again", gjson.GetBytes(next, "input").String(), "the second turn reached upstream")
			require.Equal(t, tc.wantTool, strings.Contains(gjson.GetBytes(next, "tools").Raw, `"exec"`), "tools sent: %s", gjson.GetBytes(next, "tools").Raw)
		})
	}
}
