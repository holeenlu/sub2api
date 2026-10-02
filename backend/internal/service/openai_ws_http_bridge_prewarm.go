package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// answerOpenAIWSHTTPBridgePrewarm acknowledges a generate=false prewarm without
// an upstream request. Codex prewarms each WebSocket connection with its next
// request, then continues from the returned id with only the input added since.
// The bridge has no upstream socket to warm, and forwarding the prewarm would
// bill a real generation whose output breaks that continuation. The ingress
// loop keeps the prewarm input as replay history for the continuation.
func answerOpenAIWSHTTPBridgePrewarm(accountID int64, originalModel string, turn int, writeClientMessage func([]byte) error) (*OpenAIForwardResult, error) {
	start := time.Now()
	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	result := &OpenAIForwardResult{RequestID: responseID, ResponseID: responseID, Model: originalModel, OpenAIWSMode: true, LocalPrewarm: true}
	for sequence, eventType := range []string{"response.created", "response.completed"} {
		response := map[string]any{
			"id": responseID, "object": "response", "created_at": start.Unix(),
			"model": originalModel, "status": "in_progress", "output": []any{},
		}
		if eventType == "response.completed" {
			response["status"] = "completed"
			response["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
		}
		event, err := json.Marshal(map[string]any{"type": eventType, "sequence_number": sequence, "response": response})
		if err != nil {
			return nil, err
		}
		if err := writeClientMessage(event); err != nil {
			if isOpenAIWSClientDisconnectError(err) {
				// Nothing to drain; the next read ends the connection.
				break
			}
			return nil, wrapOpenAIWSIngressTurnError("write_client", fmt.Errorf("write client websocket event: %w", err), sequence > 0)
		}
	}
	result.Duration = time.Since(start)
	logOpenAIWSModeInfo(
		"ingress_ws_http_bridge_prewarm_local account_id=%d turn=%d response_id=%s",
		accountID,
		turn,
		truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
	)
	return result, nil
}
