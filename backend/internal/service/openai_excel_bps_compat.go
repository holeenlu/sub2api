package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The native compatibility forwarders own their transport and retry policy.
// BPS must instead reuse Forward's grant, budget, repair and metering boundary;
// only the existing apicompat request/response conversion belongs here.
func (s *OpenAIGatewayService) forwardExcelBPSCompatibility(ctx context.Context, c *gin.Context, account *Account, body []byte, promptCacheKey, requestModel string, anthropic bool) (*OpenAIForwardResult, error) {
	originalModel := gjson.GetBytes(body, "model").String()
	stream := gjson.GetBytes(body, "stream").Bool()
	var converted []byte
	var request *apicompat.ResponsesRequest
	var err error
	if anthropic {
		var inbound apicompat.AnthropicRequest
		if err = json.Unmarshal(body, &inbound); err != nil {
			return nil, err
		}
		request, err = apicompat.AnthropicToResponses(&inbound)
		if err == nil {
			if request.Reasoning != nil {
				request.Reasoning.Effort = openAICompatAnthropicReasoningEffort(&inbound, account.GetMappedModel(requestModel), request.Reasoning.Effort)
			}
			if containsBetaToken(getRequestTokenListHeader(c.Request, "anthropic-beta"), claude.BetaFastMode) {
				request.ServiceTier = "priority"
			}
		}
	} else if gjson.GetBytes(body, "input").Exists() && !gjson.GetBytes(body, "messages").Exists() {
		converted = body // preserve Responses-shaped inputs, including context validation
	} else {
		var inbound apicompat.ChatCompletionsRequest
		if err = json.Unmarshal(body, &inbound); err != nil {
			return nil, err
		}
		request, err = apicompat.ChatCompletionsToResponses(&inbound)
	}
	if err != nil {
		return nil, err
	}
	if request != nil {
		converted, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	// Native compatibility paths apply group effort policy after conversion.
	// Responses-shaped bodies have already passed the Responses body policy.
	if request != nil {
		converted, err = sjson.SetBytes(converted, "model", account.GetMappedModel(requestModel))
		if err != nil {
			return nil, err
		}
		converted, _, err = ApplyOpenAIReasoningEffortPolicyFromContext(ctx, converted)
		if err != nil {
			if IsReasoningEffortPolicyDenied(err) {
				MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
				if anthropic {
					writeAnthropicError(c, http.StatusForbidden, "permission_error", err.Error())
				} else {
					writeChatCompletionsError(c, http.StatusForbidden, "permission_error", err.Error())
				}
			}
			return nil, err
		}
	}
	converted, err = sjson.SetBytes(converted, "model", requestModel)
	if err != nil {
		return nil, err
	}
	converted, err = sjson.SetBytes(converted, "stream", stream)
	if err != nil {
		return nil, err
	}
	if promptCacheKey != "" {
		converted, err = sjson.SetBytes(converted, "prompt_cache_key", promptCacheKey)
		if err != nil {
			return nil, err
		}
	}
	clientWriter := c.Writer
	writeSSE := func(payload string) error {
		clientWriter.Header().Set("Content-Type", "text/event-stream")
		clientWriter.Header().Set("Cache-Control", "no-cache")
		clientWriter.Header().Set("X-Accel-Buffering", "no")
		_, err := clientWriter.WriteString(payload)
		clientWriter.Flush()
		return err
	}
	anthropicState := apicompat.NewResponsesEventToAnthropicState()
	anthropicState.Model = originalModel
	chatState := apicompat.NewResponsesEventToChatState()
	chatState.Model = originalModel
	chatState.IncludeUsage = gjson.GetBytes(body, "stream_options.include_usage").Bool()
	writer := &excelBPSResponseWriter{ResponseWriter: clientWriter, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, startDrain: func() {}, context: c, lastWrite: time.Now()}
	writer.write = func(payload []byte) error {
		if failure := basispoints.ParseUpstreamFailure(payload); failure != nil {
			if anthropic {
				return writeSSE(buildAnthropicStreamErrorSSE(failure.Type, failure.Message))
			}
			raw, err := json.Marshal(gin.H{"error": failure.Details()})
			if err != nil {
				return err
			}
			return writeSSE("data: " + string(raw) + "\n\n")
		}
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		if anthropic {
			for _, out := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
				frame, err := apicompat.ResponsesAnthropicEventToSSE(out)
				if err != nil {
					return err
				}
				if err := writeSSE(frame); err != nil {
					return err
				}
			}
		} else {
			for _, out := range apicompat.ResponsesEventToChatChunks(&event, chatState) {
				frame, err := apicompat.ChatChunkToSSE(out)
				if err != nil {
					return err
				}
				if err := writeSSE(frame); err != nil {
					return err
				}
			}
		}
		return nil
	}
	c.Writer = writer
	defer func() { c.Writer = clientWriter }()
	result, forwardErr := s.Forward(ctx, c, account, converted)
	c.Writer = clientWriter
	if result != nil {
		result.Model, result.BillingModel, result.Stream = originalModel, result.UpstreamModel, stream
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
	}
	if writer.err != nil {
		return result, writer.err
	}
	if forwardErr != nil {
		var failover *UpstreamFailoverError
		if errors.As(forwardErr, &failover) || ctx.Err() != nil || writer.terminal {
			return result, forwardErr // no synthetic success or commit before failover
		}
		message := gjson.GetBytes(writer.pending, "error.message").String()
		errType := gjson.GetBytes(writer.pending, "error.type").String()
		if message == "" {
			message, errType = "Excel BPS request could not be completed", "api_error"
		}
		status := writer.status
		if status < 400 {
			status = http.StatusBadGateway
		}
		if anthropic {
			writeAnthropicError(c, status, errType, message)
		} else {
			writeChatCompletionsError(c, status, errType, message)
		}
		return result, forwardErr
	}
	if stream {
		if !anthropic {
			err = writeSSE("data: [DONE]\n\n")
		}
		return result, err
	}
	var response apicompat.ResponsesResponse
	if err = json.Unmarshal(writer.pending, &response); err != nil {
		return result, fmt.Errorf("decode BPS compatibility response: %w", err)
	}
	if anthropic {
		c.JSON(http.StatusOK, apicompat.ResponsesToAnthropic(&response, originalModel))
	} else {
		c.JSON(http.StatusOK, apicompat.ResponsesToChatCompletions(&response, originalModel))
	}
	return result, nil
}
