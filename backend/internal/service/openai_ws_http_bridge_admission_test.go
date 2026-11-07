//go:build unit

package service

import (
	"context"
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

type openAIWSAdmissionBody struct {
	io.Reader
	closing chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *openAIWSAdmissionBody) Close() error {
	b.once.Do(func() { close(b.closing) })
	<-b.release
	return nil
}

func TestOpenAIWSHTTPBridgeAdmissionWaitsForBodyCloseAndUsage(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	body := &openAIWSAdmissionBody{Reader: strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"bridge1\",\"model\":\"gpt-5.1\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"), closing: make(chan struct{}), release: make(chan struct{})}
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
	svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}}
	account := passthroughLifecycleAccount()
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
	fixture := newNativeHTTPBridgeFixture(t, svc, account)
	conn := newQueuedWSDownstreamTestConn()
	conn.writes = make(chan []byte, 16)
	settling, settled := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	turnDone := make(chan error, 1)
	joined := make(chan struct{})
	var closeOnce, settleOnce sync.Once
	finishClose := func() { closeOnce.Do(func() { close(body.release) }) }
	finishUsage := func() { settleOnce.Do(func() { close(settled) }) }
	defer conn.CloseNow()
	defer finishUsage()
	defer finishClose()
	t.Cleanup(func() {
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("bridge admission worker did not join")
		}
	})
	go func() {
		defer close(joined)
		done <- RunOpenAIWSMultiplexer(ctx, conn, cfg, func(laneCtx context.Context, ds *OpenAIWSDownstream) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(laneCtx)
			_, first, err := ds.Read(laneCtx)
			if err != nil {
				return
			}
			turnDone <- fixture.proxy(laneCtx, c, ds, account, first, &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) { close(settling); <-settled }})
		})
	}()
	conn.input <- []byte("{\"type\":\"response.create\",\"model\":\"gpt-5.1\",\"input\":\"hello\"}")
	select {
	case <-body.closing:
	case err := <-turnDone:
		t.Fatalf("bridge returned before body cleanup: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not reach body cleanup")
	}
	waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
	finishClose()
	select {
	case <-settling:
	case <-time.After(time.Second):
		t.Fatal("bridge did not reach usage cleanup")
	}
	waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
	finishUsage()
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
	require.NoError(t, conn.CloseNow())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridge lane did not join")
	}
	require.NoError(t, <-turnDone)
	require.EqualValues(t, 1, fixture.transport.sends.Load())
}
