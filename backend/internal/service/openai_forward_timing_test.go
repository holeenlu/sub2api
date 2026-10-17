package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOpenAIForwardTimingAttemptAndSemanticFlush(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	first := newOpenAIForwardTiming(c, c.Request, &Account{ID: 3})
	second := newOpenAIForwardTiming(c, c.Request, &Account{ID: 3})
	require.Equal(t, 1, first.attempt)
	require.Equal(t, 2, second.attempt)
	require.Same(t, second, openAIForwardTimingFromContext(c))
	core, logs := observer.New(zap.InfoLevel)
	timing := &openAIForwardTiming{started: time.Now(), log: zap.New(core)}
	timing.MarkFlush()
	require.Equal(t, 0, logs.Len(), "heartbeats must not count as semantic flush")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); timing.MarkSemantic(); timing.MarkFlush() }()
	}
	wg.Wait()
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_semantic")).Len())
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_downstream_flush")).Len())
	trace := httptrace.ContextClientTrace(timing.context(context.Background()))
	trace.GetConn("sensitive-host")
	trace.GotConn(httptrace.GotConnInfo{Reused: true})
	trace.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("secret proxy credential")})
	trace.GotFirstResponseByte()
	timing.markResponse(&http.Response{StatusCode: 502}, errors.New("secret token"))
	for _, entry := range logs.All() {
		require.NotContains(t, entry.ContextMap(), "error")
		require.NotContains(t, entry.Message, "secret")
	}
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_response_byte")).Len())
	var absent *openAIForwardTiming
	absent.MarkSemantic()
	absent.MarkFlush()
}

func TestOpenAIUpstreamJSONOrderPreservesValues(t *testing.T) {
	original := []byte(`{"model":"old","input":[1],"unknown":true}`)
	encoded := []byte(`{"input":[9007199254740993,{"escaped":"<x>中文"}],"stream":true,"model":"new","unknown":true,"client_metadata":{"a":1}}`)
	reordered := restoreOpenAIUpstreamJSONTopLevelOrder(original, encoded)
	require.Equal(t, []string{"model", "input", "unknown", "stream", "client_metadata"}, openAIUpstreamJSONTopLevelKeys(reordered))
	require.Contains(t, string(reordered), "9007199254740993")
	require.Contains(t, string(reordered), "<x>中文")
	require.JSONEq(t, string(encoded), string(reordered))
	require.Equal(t, encoded, restoreOpenAIUpstreamJSONTopLevelOrder([]byte("invalid"), encoded))
	require.Equal(t, []byte("[1]"), restoreOpenAIUpstreamJSONTopLevelOrder(original, []byte("[1]")))
	duplicate := []byte(`{"input":[1],"input":[2],"model":"new"}`)
	require.Equal(t, duplicate, restoreOpenAIUpstreamJSONTopLevelOrder(original, duplicate), "unexpected duplicate keys must not truncate the object")
}
