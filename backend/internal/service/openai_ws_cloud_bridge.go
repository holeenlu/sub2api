package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Use the existing serialized SSE-to-WS writer and keepalive. The Cloud owner
// retains server RPCs and history; the ingress must not expand/replay them.
func (s *OpenAIGatewayService) proxyOpenAIWSCloudTurn(ctx context.Context, c *gin.Context, account *Account, body []byte, originalModel string, write func([]byte) error) (*OpenAIForwardResult, error) {
	writer := &excelBPSResponseWriter{ResponseWriter: c.Writer, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, write: write, startDrain: func() {}, context: c, lastWrite: time.Now()}
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	stop := writer.startKeepalive(excelBPSWSKeepaliveInterval)
	ctx = context.WithValue(ctx, cloudWebsocketModelKey{}, originalModel)
	result, err := s.forwardOpenAICloud(ctx, c, account, body)
	stop()
	if result != nil {
		result.OpenAIWSMode = true
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
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
	if !writer.terminal {
		status, code, message, ok := cloudPublicError(err)
		if !ok {
			status, code, message = http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed."
		}
		event, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": map[string]string{"code": code, "message": message}})
		if writeErr := writer.emit(event); writeErr != nil {
			return result, writeErr
		}
	}
	return result, &openAIWSTurnRejectedError{cause: errors.New("Cloud turn was rejected")}
}
