package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ErrOpenAIWSModelSwitchRequiresReconnect marks a follow-up turn that the open
// connection cannot carry. It is a routing decision, not an account failure.
var ErrOpenAIWSModelSwitchRequiresReconnect = errors.New("websocket model switch requires reconnect")

func newOpenAIWSNativeModelSwitchError(model string) error {
	return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "model switch requires reconnect",
		fmt.Errorf("%w: native model %q requires upstream WebSocket", ErrOpenAIWSModelSwitchRequiresReconnect, model))
}

// newOpenAIWSBPSModelSwitchError closes a native upstream WS connection whose
// follow-up turn switches to a BPS model. Codex switches models on an open
// socket; closing before admission makes it retry on a new connection, which
// is routed and bridged for the BPS model from its first frame.
func newOpenAIWSBPSModelSwitchError(model string) error {
	return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "model switch requires reconnect",
		fmt.Errorf("%w: Excel BPS model %q needs the HTTP/SSE bridge", ErrOpenAIWSModelSwitchRequiresReconnect, model))
}

// excelBPSWSKeepaliveInterval bounds client-visible silence on a bridged turn.
// Codex abandons a WebSocket turn after five minutes without an event, while
// BPS can hold its first byte longer than that (queueing, uploads, image
// compaction). The forwarder's own SSE comments start only after headers and
// are not WS events. A var so tests can shorten it.
var excelBPSWSKeepaliveInterval = 15 * time.Second

// proxyOpenAIWSExcelBPSTurn runs one bridged client WS turn through the BPS
// HTTP/SSE forwarder. generate=false prewarms never reach here: the bridge
// answers them locally before dispatching a BPS generation.
func (s *OpenAIGatewayService) proxyOpenAIWSExcelBPSTurn(ctx context.Context, c *gin.Context, account *Account, body []byte, originalModel string, write func([]byte) error) (*OpenAIForwardResult, error) {
	start := time.Now()
	upstreamCtx := ctx
	startDrain := func() {}
	writer := &responsesEventWriter{ResponseWriter: c.Writer, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, collectReplay: true, maxFrameBytes: basispoints.MaxSSEEventBytes, write: write, startDrain: startDrain, context: c, lastWrite: start}
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	stopKeepalive := writer.startKeepalive(excelBPSWSKeepaliveInterval)
	defer stopKeepalive()
	result, err := s.forwardExcelBPS(upstreamCtx, c, account, body, start, gjson.GetBytes(body, "model").String())
	stopKeepalive()
	if result != nil {
		result.Model = originalModel
		result.OpenAIWSMode = true
		result.LatencyMetrics = SnapshotOpenAIUsageLatency(c, result.Stream || result.OpenAIWSMode)
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
		// The WS continuation cache uses response.id, not the upstream HTTP
		// request ID. Replay every output item, as the shared bridge does, so
		// incremental turns rebuild the full history on either channel.
		if result.ResponseID != "" {
			result.RequestID = result.ResponseID
		}
		result.wsReplayInput = writer.replay.AllItems()
		result.wsReplayInputExists = len(result.wsReplayInput) > 0
		result.wsAccountFailoverReplayInput = writer.replay.AllItems()
		result.wsReplayIncomplete = len(writer.replay.pending) > 0 || writer.replay.unidentifiedOutput
	}
	if writer.err != nil {
		return result, writer.err
	}
	if err == nil {
		// The drain window may close right after the final event; that must
		// not turn a completed turn into a timeout.
		return result, nil
	}
	if upstreamCtx.Err() != nil {
		return result, context.Cause(upstreamCtx)
	}
	if errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
		return result, err
	}
	// Explicit rejections and replay-safe transient failures before output
	// reach the existing account-switch loop without closing the client socket.
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) {
		return result, err
	}
	if err != nil && !writer.terminal {
		status := writer.status
		var errorBody map[string]any
		_ = json.Unmarshal(writer.pending, &errorBody)
		if status < 400 || errorBody["error"] == nil {
			status = http.StatusBadGateway
			errorBody = map[string]any{"error": gin.H{"type": "server_error", "code": "basispoints_bridge_error", "message": "Excel BPS response could not be forwarded"}}
		}
		errorBody["type"], errorBody["status"], errorBody["sequence_number"] = "error", status, 0
		event, marshalErr := json.Marshal(errorBody)
		if marshalErr != nil {
			return result, fmt.Errorf("encode websocket BPS error: %w", marshalErr)
		}
		if writeErr := writer.emit(event); writeErr != nil {
			return result, writeErr
		}
	}
	return result, &openAIWSTurnRejectedError{cause: err}
}
