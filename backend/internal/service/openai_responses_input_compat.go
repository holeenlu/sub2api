package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const openAIResponsesInputTextMaxChars = 10000000

// sanitizeOpenAIResponsesOrphanToolOutputs removes tool-output items that have
// no matching call or item reference anywhere in the current input. Named
// function outputs without a call ID are standalone inputs, not orphan results.
func sanitizeOpenAIResponsesOrphanToolOutputs(reqBody map[string]any, input []any, hasPreviousResponseID bool) (bool, error) {
	if len(input) == 0 || hasPreviousResponseID {
		return false, nil
	}

	toolCallIDs := make(map[string]struct{}, len(input))
	referenceIDs := make(map[string]struct{}, len(input))
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemType := strings.TrimSpace(firstNonEmptyString(item["type"]))
		if itemType == "item_reference" {
			if id := strings.TrimSpace(firstNonEmptyString(item["id"])); id != "" {
				referenceIDs[id] = struct{}{}
			}
			continue
		}
		if !isCodexToolCallContextItemType(itemType) {
			continue
		}
		if id := strings.TrimSpace(firstNonEmptyString(item["call_id"], item["id"])); id != "" {
			toolCallIDs[id] = struct{}{}
		}
	}

	modified := false
	normalized := make([]any, 0, len(input))
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || !isCodexToolCallOutputItemType(strings.TrimSpace(firstNonEmptyString(item["type"]))) {
			normalized = append(normalized, rawItem)
			continue
		}

		callID := strings.TrimSpace(firstNonEmptyString(item["call_id"]))
		// Codex sends externally supplied inputs (such as task delegation) as
		// named function outputs without a preceding function call. Preserve
		// their native type, namespace and output instead of silently dropping
		// the request that started the turn.
		if openai.IsNamedStandaloneToolOutput(item) {
			normalized = append(normalized, rawItem)
			continue
		}
		_, hasToolCall := toolCallIDs[callID]
		_, hasReference := referenceIDs[callID]
		if callID != "" && (hasToolCall || hasReference) {
			normalized = append(normalized, rawItem)
			continue
		}

		modified = true
	}
	if !modified {
		return false, nil
	}
	if len(normalized) == 0 && !hasOpenAIResponsesContextReference(reqBody) {
		return false, &openAIResponsesInputError{
			Code: "missing_tool_context", Param: "input",
			Message: "Tool outputs require their matching calls or a valid previous_response_id; resend the complete conversation input.",
		}
	}
	reqBody["input"] = normalized
	return true, nil
}

func truncateOpenAIResponsesInputText(_ map[string]any) bool {
	// Do not silently rewrite client or tool output. If an upstream enforces a
	// text limit, forwarding the original value preserves its explicit error for
	// the client and the normal Ops error pipeline. This compatibility shim is
	// retained until the two callers can remove the old mutation hook together.
	return false
}

func openAIResponsesInputMayNeedTruncation(_ []byte) bool {
	// See truncateOpenAIResponsesInputText. Returning false also avoids decoding
	// very large bodies solely for a mutation that must not happen.
	return false
}

// openAIResponsesInputError is a local request rejection, never an upstream
// transport failure. It must not consume account health or failover budgets.
type openAIResponsesInputError struct {
	Code    string
	Param   string
	Message string
}

func (e *openAIResponsesInputError) Error() string { return e.Message }

func hasOpenAIResponsesContextReference(body map[string]any) bool {
	for _, field := range []string{"previous_response_id", "prompt", "conversation"} {
		switch value := body[field].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return true
			}
		case map[string]any:
			if field != "previous_response_id" && len(value) > 0 {
				return true
			}
		}
	}
	return false
}

// Validate only the context carrier, not a closed list of input item types.
// Unknown multimodal/tool/compaction items must survive protocol evolution.
func validateOpenAIResponsesInput(body []byte) error {
	for _, field := range []string{"input", "previous_response_id", "prompt", "conversation"} {
		value := gjson.GetBytes(body, field)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return nil
		}
		if field != "previous_response_id" && value.IsObject() && len(value.Map()) > 0 {
			return nil
		}
		if field == "input" && value.IsArray() && len(value.Array()) > 0 {
			return nil
		}
	}
	return &openAIResponsesInputError{
		Code: "missing_required_parameter", Param: "input",
		Message: "One of input, previous_response_id, prompt or conversation must provide context; resend the complete conversation input.",
	}
}

// A gateway transform losing valid context is our defect, not a client or
// account error. The ordinary local rejection remains a non-retryable 400.
func validateOpenAIResponsesInputTransition(original, outgoing []byte) error {
	err := validateOpenAIResponsesInput(outgoing)
	if err != nil && validateOpenAIResponsesInput(original) == nil {
		return &openAIResponsesInputError{Code: "gateway_input_context_lost", Param: "input", Message: "Request preparation lost the original context; the request was not sent upstream."}
	}
	return err
}

func (e *openAIResponsesInputError) statusAndType() (int, string) {
	if e.Code == "gateway_input_context_lost" {
		return http.StatusInternalServerError, "server_error"
	}
	return http.StatusBadRequest, "invalid_request_error"
}

func writeOpenAIResponsesInputError(c *gin.Context, err error) {
	var inputErr *openAIResponsesInputError
	if c != nil && errors.As(err, &inputErr) {
		status, errorType := inputErr.statusAndType()
		envelope := gin.H{"error": gin.H{
			"type": errorType, "code": inputErr.Code,
			"param": inputErr.Param, "message": inputErr.Message,
		}}
		// Queue keepalives may already have committed a Responses SSE stream.
		// Reuse its terminal-event encoder instead of appending a JSON response.
		if c.Writer.Written() && strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") && c.Request != nil && strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/responses") {
			MarkOpsStreamErrorValue(c, OpsStreamError{ErrType: errorType, Code: inputErr.Code, Message: inputErr.Message, IntendedStatus: status, CountTowardsSLA: status >= 500, RequestScoped: true})
			source, _ := json.Marshal(envelope)
			if _, writeErr := fmt.Fprint(c.Writer, buildOpenAIResponseFailedSSE("", "", 0, source, inputErr.Message)); writeErr != nil {
				_ = c.Error(writeErr)
			}
			c.Writer.Flush()
			return
		}
		c.JSON(status, envelope)
	}
}

// WriteOpenAIContinuationError shares the Responses JSON/SSE rejection writer
// with ingress validation so queue keepalives cannot corrupt a recovery signal.
func WriteOpenAIContinuationError(c *gin.Context) {
	if c == nil {
		return
	}
	StopOpenAICompactSSEKeepaliveCommitted(c)
	MarkOpsStreamErrorValue(c, OpsStreamError{
		ErrType: "invalid_request_error", Code: "previous_response_not_found",
		Message: ErrOpenAIContinuationUnavailable.Error(), IntendedStatus: http.StatusBadRequest,
		RequestScoped: true, CountTowardsSLA: false,
	})
	writeOpenAIResponsesInputError(c, &openAIResponsesInputError{
		Code: "previous_response_not_found", Param: "previous_response_id",
		Message: ErrOpenAIContinuationUnavailable.Error(),
	})
	MarkResponseCommitted(c)
}

func (e *openAIResponsesInputError) websocketEvent(request []byte) ([]byte, error) {
	_, errorType := e.statusAndType()
	event, err := json.Marshal(map[string]any{"type": "error", "error": map[string]string{
		"type": errorType, "code": e.Code, "param": e.Param, "message": e.Message,
	}})
	if err != nil {
		return nil, err
	}
	lane, eventID, err := openAIWSRequestEnvelope(request)
	if err != nil {
		return nil, err
	}
	return openAIWSEventWithEnvelope(event, lane, eventID)
}
