package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type siwcTransportConn struct {
	*openAIWSReaderLoopFakeConn
	mu       sync.Mutex
	writes   [][]byte
	event    string
	writeErr error
}

func (c *siwcTransportConn) WriteJSON(ctx context.Context, value any) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.writes = append(c.writes, body)
	event := c.event
	c.mu.Unlock()
	if event != "" {
		c.messages <- []byte(event)
	}
	return nil
}

type siwcTransportDialer struct {
	mu       sync.Mutex
	conns    []*siwcTransportConn
	event    string
	urls     []string
	headers  []http.Header
	writeErr error
}

func (d *siwcTransportDialer) Dial(_ context.Context, url string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	conn := &siwcTransportConn{openAIWSReaderLoopFakeConn: newOpenAIWSReaderLoopFakeConn(), event: d.event, writeErr: d.writeErr}
	d.conns = append(d.conns, conn)
	d.urls = append(d.urls, url)
	d.headers = append(d.headers, headers.Clone())
	return conn, 101, http.Header{"X-Request-Id": []string{"req_ws_test"}}, nil
}
func siwcTransportFixture(t *testing.T, event string) (*OpenAIGatewayService, *OpenAIOAuthService, *Account, *openAIWSSIWCSession, *siwcTransportDialer) {
	svc, oauth, account := siwcWSFixture(t, &httpUpstreamRecorder{})
	dialer := &siwcTransportDialer{event: event}
	svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
	t.Cleanup(func() { svc.getOpenAIWSConnPool().Close() })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	session := newOpenAIWSSIWCSession(c, account)
	t.Cleanup(session.close)
	return svc, oauth, account, session, dialer
}
func siwcTransportRequest(t *testing.T, oauth *OpenAIOAuthService, account *Account, body string) *http.Request {
	t.Helper()
	token, owner, err := oauth.SIWCAccessTokenWithIdentity(t.Context(), account, nil)
	require.NoError(t, err)
	ctx := WithOpenAITurnBudget(t.Context(), NewOpenAITurnBudget(nil))
	ctx = withSIWCRequestGrant(ctx, account, token, owner)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
func TestSIWCWSTransportCommitAndRotation(t *testing.T) {
	event := "{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_complete\",\"status\":\"completed\",\"output\":[]}}"
	svc, oauth, a, session, dialer := siwcTransportFixture(t, event)
	input := "{\"model\":\"gpt-6.1-sol\",\"input\":[{\"role\":\"user\",\"content\":\"one\"}]}"
	resp, err := session.do(svc, siwcTransportRequest(t, oauth, a, input), a, "")
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Nil(t, session.anchor, "the reader cannot commit an unaccepted result")
	result := &OpenAIForwardResult{RequestID: "resp_complete", UpstreamTerminalEvent: "response.completed"}
	session.finish(result, nil)
	require.NotNil(t, session.anchor)
	dialer.mu.Lock()
	require.Len(t, dialer.conns, 1)
	conn := dialer.conns[0]
	require.Equal(t, siwcWebSocketURL, dialer.urls[0])
	require.Len(t, dialer.headers[0], 2)
	dialer.mu.Unlock()
	select {
	case <-conn.closed:
		t.Fatal("successful turn closed retained socket")
	default:
	}
	// An accepted prefix on the held socket sends only the new input. Capture
	// the emitted summary rather than treating a candidate estimate as savings.
	c, logs := newWSLatencyTestContext()
	timing := newOpenAITransportTiming(c, a, "websocket", 0)
	next := `{"model":"gpt-6.1-sol","input":[{"role":"user","content":"one"},{"role":"user","content":"next <中文&>"}]}`
	req := siwcTransportRequest(t, oauth, a, next)
	req = req.WithContext(context.WithValue(req.Context(), openAITransportTimingKey{}, timing))
	resp, err = session.do(svc, req, a, "")
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	session.finish(result, nil)
	timing.Finish(nil)
	normalized, err := normalizeSIWCBody([]byte(next))
	require.NoError(t, err)
	fullWire, _, _, err := siwcWebSocketPayload(normalized, nil)
	require.NoError(t, err)
	conn.mu.Lock()
	require.Len(t, conn.writes, 2)
	sentBytes := len(conn.writes[1])
	require.True(t, gjson.GetBytes(conn.writes[1], "previous_response_id").Exists())
	conn.mu.Unlock()
	summaries := wsLatencySummaries(logs)
	require.Len(t, summaries, 1)
	payload := summaries[0]["incremental_payload"].(map[string]int64)
	require.Equal(t, int64(len(fullWire)), payload["full_json_bytes"])
	require.Equal(t, int64(sentBytes), payload["sent_json_bytes"])
	require.Equal(t, false, summaries[0]["body_wire_bytes_known"])
	require.NotContains(t, fmt.Sprint(summaries), "next")

	// Rotating the bearer preserves grant identity, but a new authenticated
	// socket must start with full input rather than an old response ID.
	pr, pg, r, g, err := oauth.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	g.AccessToken = "rotated-siwc-token"
	_, _, err = oauth.saveSIWCRecord(t.Context(), a, pr, pg, r, g)
	require.NoError(t, err)
	resp, err = session.do(svc, siwcTransportRequest(t, oauth, a, input), a, "")
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	session.finish(result, nil)
	dialer.mu.Lock()
	require.Len(t, dialer.conns, 2)
	second := dialer.conns[1]
	dialer.mu.Unlock()
	select {
	case <-conn.closed:
	default:
		t.Fatal("rotated socket was not evicted")
	}
	second.mu.Lock()
	require.Len(t, second.writes, 1)
	require.False(t, gjson.GetBytes(second.writes[0], "previous_response_id").Exists())
	second.mu.Unlock()
}
func TestSIWCWSTransportCancellationAndPartialClose(t *testing.T) {
	for _, scenario := range []string{"blocked read", "partial frame", "cancel after terminal", "failed turn", "malformed completed", "write fails"} {
		t.Run(scenario, func(t *testing.T) {
			event := "{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_complete\",\"status\":\"completed\",\"output\":[]}}"
			if scenario == "blocked read" {
				event = ""
			}
			if scenario == "malformed completed" {
				event = "{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_complete\",\"status\":\"completed\"}}"
			}
			svc, oauth, a, session, dialer := siwcTransportFixture(t, event)
			req := siwcTransportRequest(t, oauth, a, "{\"model\":\"gpt-6.1-sol\",\"input\":[]}")
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			timing := &openAIForwardTiming{}
			req = req.WithContext(context.WithValue(ctx, openAITransportTimingKey{}, timing))
			if scenario == "write fails" {
				dialer.writeErr = errors.New("failed write")
			}
			resp, err := session.do(svc, req, a, "")
			if scenario == "write fails" {
				require.Error(t, err)
				require.Nil(t, resp)
				require.False(t, timing.payloadKnown, "failed writes cannot claim sent JSON or savings")
				return
			}
			require.NoError(t, err)
			require.True(t, timing.payloadKnown)
			if scenario == "blocked read" {
				done := make(chan error, 1)
				go func() { _, err := io.ReadAll(resp.Body); done <- err }()
				reader := resp.Body.(*openAIWSSIWCSSEBody)
				require.Eventually(t, func() bool {
					if reader.mu.TryLock() {
						reader.mu.Unlock()
						return false
					}
					return true
				}, time.Second, time.Millisecond)
				var closers sync.WaitGroup
				for i := 0; i < 4; i++ {
					closers.Add(1)
					go func() { defer closers.Done(); _ = resp.Body.Close() }()
				}
				closers.Wait()
				select {
				case err := <-done:
					require.Error(t, err)
				case <-time.After(time.Second):
					t.Fatal("Close did not cancel blocked Read")
				}
			} else if scenario == "partial frame" {
				_, err := resp.Body.Read(make([]byte, 1))
				require.NoError(t, err)
			} else {
				_, err = io.ReadAll(resp.Body)
				require.NoError(t, err)
			}
			if scenario == "cancel after terminal" {
				cancel()
			}
			require.NoError(t, resp.Body.Close())
			var turnErr error
			if scenario == "failed turn" {
				turnErr = errors.New("downstream write failed")
			}
			session.finish(&OpenAIForwardResult{RequestID: "resp_complete", UpstreamTerminalEvent: "response.completed"}, turnErr)
			require.Nil(t, session.anchor)
			dialer.mu.Lock()
			conn := dialer.conns[0]
			dialer.mu.Unlock()
			select {
			case <-conn.closed:
			default:
				t.Fatal("uncertain or rejected turn retained socket")
			}
		})
	}
}
func TestSIWCWSPrefixProofPreservesPrecisionAndSettings(t *testing.T) {
	first := []byte("{\"model\":\"same\",\"instructions\":\"first\",\"input\":[{\"type\":\"message\",\"content\":9007199254740993}]}")
	_, anchor, _, err := siwcWebSocketPayload(first, nil)
	require.NoError(t, err)
	output := []byte("{\"type\":\"message\",\"id\":\"msg_one\",\"content\":[]}")
	anchor.history, err = siwcAppendJSONHash(anchor.history, output)
	require.NoError(t, err)
	anchor.items++
	anchor.responseID = "resp_<中文&>_one"
	for _, tc := range []struct {
		name, number, instructions string
		wantDelta                  bool
	}{{"reordered equivalent", "9007199254740993", "first", true}, {"distinct precise integer", "9007199254740992", "first", false}, {"settings changed", "9007199254740993", "different", false}} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf("{\"model\":\"same\",\"instructions\":%q,\"input\":[{\"content\":%s,\"type\":\"message\"},%s,{\"type\":\"message\",\"content\":\"next\"}]}", tc.instructions, tc.number, output)
			wire, pending, fullBytes, err := siwcWebSocketPayload([]byte(body), anchor)
			require.NoError(t, err)
			require.Equal(t, tc.wantDelta, gjson.GetBytes(wire, "previous_response_id").Exists())
			if tc.wantDelta {
				require.Len(t, gjson.GetBytes(wire, "input").Array(), 1)
			} else {
				require.Len(t, gjson.GetBytes(wire, "input").Array(), 3)
			}
			require.Equal(t, 3, pending.items)
			fullWire, _, _, err := siwcWebSocketPayload([]byte(body), nil)
			require.NoError(t, err)
			require.Equal(t, len(fullWire), fullBytes, "counterfactual must use the same WS envelope")
			if !tc.wantDelta {
				require.Equal(t, len(wire), fullBytes)
			}
		})
	}
}

func TestSIWCWSTransportBareErrorDrainsAuthoritativeUsage(t *testing.T) {
	svc, oauth, a, session, dialer := siwcTransportFixture(t, `{"type":"error","error":{"code":"rate_limit_exceeded","message":"retry later"}}`)
	resp, err := session.do(svc, siwcTransportRequest(t, oauth, a, `{"model":"gpt-6.1-sol","input":[]}`), a, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	dialer.mu.Lock()
	conn := dialer.conns[0]
	dialer.mu.Unlock()
	conn.messages <- []byte(`{"type":"response.failed","response":{"id":"resp_failed","status":"failed","usage":{"input_tokens":19,"output_tokens":3}}}`)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), `"input_tokens":19`)
	require.Equal(t, 2, strings.Count(string(data), "data: "))
	require.NoError(t, resp.Body.Close())
	session.finish(&OpenAIForwardResult{RequestID: "resp_failed", UpstreamTerminalEvent: "response.failed"}, nil)
	require.Nil(t, session.anchor)
	select {
	case <-conn.closed:
	default:
		t.Fatal("failed turn socket remained reusable")
	}
}

func TestSIWCWSTransportFrameLimitAndStaleClose(t *testing.T) {
	event := "{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_complete\",\"status\":\"completed\",\"output\":[]}}"
	svc, oauth, a, session, dialer := siwcTransportFixture(t, event)
	svc.cfg.Gateway.MaxLineSize = 32
	req := func() *http.Request {
		return siwcTransportRequest(t, oauth, a, "{\"model\":\"gpt-6.1-sol\",\"input\":[]}")
	}
	first, err := session.do(svc, req(), a, "")
	require.NoError(t, err)
	_, err = io.ReadAll(first.Body)
	require.ErrorContains(t, err, "size limit")
	require.NoError(t, first.Body.Close())
	session.finish(nil, err)
	require.Nil(t, session.anchor)
	svc.cfg.Gateway.MaxLineSize = defaultMaxLineSize
	second, err := session.do(svc, req(), a, "")
	require.NoError(t, err)
	// An old cancellation callback must not evict the next turn's fresh lease.
	require.NoError(t, first.Body.Close())
	_, err = io.ReadAll(second.Body)
	require.NoError(t, err)
	require.NoError(t, second.Body.Close())
	session.finish(&OpenAIForwardResult{RequestID: "resp_complete", UpstreamTerminalEvent: "response.completed"}, nil)
	require.NotNil(t, session.anchor)
	dialer.mu.Lock()
	require.Len(t, dialer.conns, 2)
	current := dialer.conns[1]
	dialer.mu.Unlock()
	select {
	case <-current.closed:
		t.Fatal("stale callback evicted current lease")
	default:
	}
}
