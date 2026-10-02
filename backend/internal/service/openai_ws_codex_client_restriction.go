package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// ErrOpenAIWSCodexClientRestricted marks a WebSocket turn denied by the
// account's codex_cli_only policy. It is a client decision, not an account
// failure: the handler neither fails over nor penalizes the account.
var ErrOpenAIWSCodexClientRestricted = errors.New("codex_cli_only restriction: only codex official clients are allowed")

// checkOpenAIWSCodexClientRestriction applies the HTTP codex_cli_only gate to
// one response.create frame. HTTP checks every request, so WS checks every
// turn: the handshake fixes the identity headers, while body fingerprint
// signals and the global policy are read per frame. A denial first sends a 403
// error event, which Codex reports instead of retrying a bare close.
func (s *OpenAIGatewayService) checkOpenAIWSCodexClientRestriction(ctx context.Context, c *gin.Context, account *Account, frame []byte, writeEvent func([]byte) error) error {
	result := s.detectCodexClientRestriction(c, account, frame)
	logCodexCLIOnlyDetection(ctx, c, account, getAPIKeyIDFromContext(c), result, frame)
	if !result.Enabled || result.Matched {
		return nil
	}
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	message := CodexClientRestrictionMessage(result)
	event := buildOpenAIWSCodexClientRestrictedEvent(message)
	if writeEvent != nil && writeEvent(event) == nil {
		markOpenAIWSClientVisibleFailure(c, "error", event)
	}
	return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, message,
		fmt.Errorf("%w: %s", ErrOpenAIWSCodexClientRestricted, result.Reason))
}

// buildOpenAIWSCodexClientRestrictedEvent mirrors the HTTP 403 body as a WS
// error event; the status makes Codex treat it as a final rejection.
func buildOpenAIWSCodexClientRestrictedEvent(message string) []byte {
	payload, err := json.Marshal(map[string]any{
		"type": "error", "sequence_number": 0, "status": http.StatusForbidden,
		"error": map[string]any{"type": "forbidden_error", "message": message},
	})
	if err != nil {
		return []byte(`{"type":"error","sequence_number":0,"status":403,"error":{"type":"forbidden_error","message":"` + CodexOfficialClientsOnlyMessage + `"}}`)
	}
	return payload
}
