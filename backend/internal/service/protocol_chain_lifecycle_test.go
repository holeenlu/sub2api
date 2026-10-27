//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBoundedDetachedCompletionPreservesTaskDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	upstream, finish := detachUpstreamContext(parent, time.Hour)
	defer finish()
	parentDeadline, _ := parent.Deadline()
	upstreamDeadline, ok := upstream.Deadline()
	require.True(t, ok)
	require.Equal(t, parentDeadline, upstreamDeadline)
	cancel()
	require.NoError(t, upstream.Err(), "client cancellation allows bounded billing completion")
	<-upstream.Done()
	require.ErrorIs(t, upstream.Err(), context.DeadlineExceeded)
}

func TestOAuthImageCollectorReturnsAtTerminalWithoutEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"aGVsbG8="}],"usage":{"input_tokens":3,"output_tokens":4}}}`+"\n\n")
	}()
	finished := make(chan error, 1)
	go func() {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
		_, count, _, err := (&OpenAIGatewayService{}).handleOpenAIImagesOAuthNonStreamingResponse(&http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, c, "b64_json", "gpt-image-2")
		if err == nil && count != 1 {
			err = io.ErrUnexpectedEOF
		}
		finished <- err
	}()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("collector waited for EOF after complete protocol terminal")
	}
}

func TestEmbeddingsDetachedBodyExpiresWithTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	body := newContextBoundBlockingReadCloser(nil)
	defer body.forceUnblock()
	svc := &OpenAIGatewayService{httpUpstream: &contextBoundHTTPUpstream{body: body}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil).WithContext(ctx)
	result, err := svc.ForwardEmbeddings(ctx, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}, []byte(`{"model":"text-embedding-3-small","input":"hello"}`), "")
	require.Nil(t, result)
	require.Error(t, err)
	require.ErrorIs(t, body.ctx.Err(), context.DeadlineExceeded)
}

func TestBedrockForwardRetainsMeteredUsageOnError(t *testing.T) {
	event := `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`
	wire := buildBedrockTestFrame("chunk", []byte(`{"bytes":"`+base64.StdEncoding.EncodeToString([]byte(event))+`"}`))
	upstream := &httpUpstreamStub{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(wire))}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-5-5","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)), "anthropic")
	require.NoError(t, err)
	result, err := svc.forwardBedrock(context.Background(), c, &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "fixture", "aws_region": "us-east-1"}}, parsed, time.Now())
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
}

func TestGeminiNativeHeartbeatDoesNotExtendUpstreamIdle(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10}}\n\n")
	}()
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 2
	cfg.Gateway.StreamKeepaliveInterval = 1
	svc := &GeminiMessagesCompatService{cfg: cfg}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini:streamGenerateContent", nil)
	result, err := svc.handleNativeStreamingResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, time.Now(), false, &Account{ID: 1}, "")
	require.ErrorIs(t, err, errOpenAISSEIdle)
	require.NotNil(t, result)
	require.Equal(t, 10, result.usage.InputTokens)
	require.Contains(t, rec.Body.String(), ": keepalive")
}

func TestGrokRealtimeSlowDownstreamWriteIsBounded(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 1
	stub := &grokRealtimeFrameUpstreamStub{toClient: make(chan grokRealtimeFrame, 1)}
	stub.toClient <- grokRealtimeFrame{kind: coderws.MessageBinary, data: bytes.Repeat([]byte{1}, 16<<20)}
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := coderws.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer client.CloseNow()
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		_, err = (&OpenAIGatewayService{cfg: cfg}).ProxyGrokRealtimeConn(r.Context(), c, client, &GrokRealtimeUpstream{conn: stub, accountID: 1})
		finished <- err
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	select {
	case err = <-finished:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal("slow client retained the relay beyond the write budget")
	}
}

func TestGrokTTSAudioFlushesBeforeUpstreamEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	upstream := &httpUpstreamStub{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/mpeg"}}, Body: reader}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	done := make(chan *OpenAIForwardResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		result, _ := svc.ForwardGrokVoice(r.Context(), c, &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}, "tts", []byte(`{"text":"hello"}`), "application/json")
		done <- result
	}))
	defer server.Close()
	go func() { _, _ = writer.Write([]byte("first")) }()
	response, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	first := make([]byte, 5)
	_, err = io.ReadFull(response.Body, first)
	require.NoError(t, err)
	require.Equal(t, "first", string(first))
	select {
	case <-done:
		t.Fatal("upstream EOF was required before first audio")
	default:
	}
	require.NoError(t, writer.Close())
	_, _ = io.ReadAll(response.Body)
	result := <-done
	require.NotNil(t, result)
	require.Equal(t, 5.0/1_000_000, result.AudioUsage.DurationOrUnits)
}
