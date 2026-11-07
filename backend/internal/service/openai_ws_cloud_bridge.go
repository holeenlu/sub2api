package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// cloudContinuationErrorKey carries a public continuation rejection (for
// example previous_response_not_found) to the bridge turn, which reports it as
// a per-turn error event instead of closing the client connection.
type cloudContinuationErrorKey struct{}

// Use the shared serialized Responses-event sink and keepalive. Cloud emits
// events directly, without SSE encoding/parsing. Its owner retains live server
// RPCs and continuation; the sink observes history only for validated transitions.
func (s *OpenAIGatewayService) proxyOpenAIWSCloudTurn(ctx context.Context, c *gin.Context, account *Account, body []byte, originalModel string, write func([]byte) error) (*OpenAIForwardResult, error) {
	writer := &responsesEventWriter{ResponseWriter: c.Writer, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, collectReplay: true, maxFrameBytes: OpenAICloudMaxEnvelopeBytes, write: write, startDrain: func() {}, context: c, lastWrite: time.Now(), failureOrigin: openAIWSFailureGateway}
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	keepaliveInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}
	stop := writer.startKeepalive(keepaliveInterval)
	defer stop()
	ctx = context.WithValue(ctx, cloudWebsocketModelKey{}, originalModel)
	var result *OpenAIForwardResult
	err, _ := ctx.Value(cloudContinuationErrorKey{}).(error)
	if err == nil {
		result, err = s.forwardOpenAICloud(ctx, c, account, body)
	} else {
		recordCloudAdaptation(c, cloudIngressAdaptations(ctx), err)
	}
	stop()
	if result != nil {
		result.OpenAIWSMode = true
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
		result.wsReplayInput = writer.replay.AllItems()
		result.wsReplayInputExists = len(result.wsReplayInput) > 0
		result.wsAccountFailoverReplayInput = writer.replay.AllItems()
		result.wsReplayIncomplete = len(writer.replay.pending) > 0 || writer.replay.unidentifiedOutput
	}
	if writer.err != nil {
		return result, writer.err
	}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return result, context.Cause(ctx)
	}
	if IsTurnBudgetError(err) {
		return result, err
	}
	var failure *UpstreamFailoverError
	if errors.As(err, &failure) && failure.Reason == CloudPreInferenceFailureReason && !writer.terminal {
		return result, failure
	}
	if !writer.terminal {
		status, code, message, ok := cloudPublicError(err)
		if !ok {
			status, code, message = http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed."
		}
		if status >= 400 && status < 500 && (&cloudGatewayError{code: code}).RequestScoped() {
			writer.failureOrigin = openAIWSFailureRequest
		}
		event, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": cloudPublicErrorFields(err, code, message)})
		if writeErr := writer.emit(event); writeErr != nil {
			return result, writeErr
		}
	}
	return result, &openAIWSTurnRejectedError{cause: err}
}
