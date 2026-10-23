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
// Independently authorized BPS/SIWC must reuse Forward's grant, budget and metering boundary;
// only the existing apicompat request/response conversion belongs here.
func (s *OpenAIGatewayService) forwardOpenAIGrantCompatibility(ctx context.Context, c *gin.Context, account *Account, body []byte, promptCacheKey, requestModel string, anthropic bool) (*OpenAIForwardResult, error) {
	if gjson.GetBytes(body, "stream").Bool() {
		defer GuardSSEWrites(c)()
	}
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
	var grantHeaders http.Header
	copySIWCHeaders := func() {
		if !account.IsSIWCEnabled() {
			return
		}
		for _, key := range []string{"X-Request-Id", "Retry-After"} {
			if value := grantHeaders.Get(key); value != "" {
				clientWriter.Header().Set(key, value)
			}
		}
	}
	writeSSE := func(payload string) error {
		copySIWCHeaders()
		clientWriter.Header().Set("Content-Type", "text/event-stream")
		clientWriter.Header().Set("Cache-Control", "no-cache")
		clientWriter.Header().Set("X-Accel-Buffering", "no")
		_, err := clientWriter.WriteString(payload)
		if err != nil {
			return err
		}
		return http.NewResponseController(clientWriter).Flush()
	}
	anthropicState := apicompat.NewResponsesEventToAnthropicState()
	anthropicState.Model = originalModel
	chatState := apicompat.NewResponsesEventToChatState()
	chatState.Model = originalModel
	chatState.IncludeUsage = gjson.GetBytes(body, "stream_options.include_usage").Bool()
	writer := &excelBPSResponseWriter{ResponseWriter: clientWriter, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, startDrain: func() {}, context: c, lastWrite: time.Now()}
	grantHeaders = writer.header
	var conversionErr error
	writer.write = func(payload []byte) error {
		if conversionErr != nil {
			return nil
		}
		kind := gjson.GetBytes(payload, "type").String()
		if account.IsSIWCEnabled() && (kind == "response.failed" || kind == "error" || kind == "response.incomplete") {
			envelope := gin.H{"error": siwcCompatibilityErrorDetails(payload)}
			prefix := ""
			if anthropic {
				envelope["type"] = "error"
				prefix = "event: error\n"
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				return err
			}
			return writeSSE(prefix + "data: " + string(raw) + "\n\n")
		}
		if failure := basispoints.ParseUpstreamFailure(payload); !account.IsSIWCEnabled() && failure != nil {
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
			events := apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState)
			if conversionErr = anthropicState.Err(); conversionErr != nil {
				return nil
			}
			for _, out := range events {
				frame, err := apicompat.ResponsesAnthropicEventToSSE(out)
				if err != nil {
					return err
				}
				if err := writeSSE(frame); err != nil {
					return err
				}
			}
		} else {
			chunks := apicompat.ResponsesEventToChatChunks(&event, chatState)
			if conversionErr = chatState.Err(); conversionErr != nil {
				return nil
			}
			for _, out := range chunks {
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
	copySIWCHeaders()
	if result != nil {
		result.Model, result.BillingModel, result.Stream = originalModel, result.UpstreamModel, stream
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
	}
	if conversionErr != nil {
		return result, writeAPICompatConversionError(c, conversionErr, anthropic)
	}
	if writer.err != nil {
		return result, writer.err
	}
	if forwardErr != nil {
		var failover *UpstreamFailoverError
		if errors.As(forwardErr, &failover) || ctx.Err() != nil || writer.terminal {
			return result, forwardErr // no synthetic success or commit before failover
		}
		if account.IsSIWCEnabled() {
			status := writer.status
			if status < 400 {
				status = http.StatusBadGateway
			}
			envelope := gin.H{"error": siwcCompatibilityErrorDetails(writer.pending)}
			if anthropic {
				envelope["type"] = "error"
			}
			MarkResponseCommitted(c)
			c.JSON(status, envelope)
			return result, forwardErr
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
			// Forward already produced the safe client error. Preserve its stable
			// code/param while changing only the Chat response representation.
			detail := gin.H{"type": errType, "message": message}
			for _, field := range []string{"code", "param"} {
				if value := gjson.GetBytes(writer.pending, "error."+field); value.Type == gjson.String && value.String() != "" {
					detail[field] = value.String()
				}
			}
			MarkResponseCommitted(c)
			c.JSON(status, gin.H{"error": detail})
		}
		return result, forwardErr
	}
	if stream {
		if anthropic {
			events := apicompat.FinalizeResponsesAnthropicStream(anthropicState)
			if err := anthropicState.Err(); err != nil {
				return result, writeAPICompatConversionError(c, err, true)
			}
			for _, event := range events {
				frame, err := apicompat.ResponsesAnthropicEventToSSE(event)
				if err != nil {
					return result, err
				}
				if err := writeSSE(frame); err != nil {
					return result, err
				}
			}
		} else {
			chunks := apicompat.FinalizeResponsesChatStream(chatState)
			if err := chatState.Err(); err != nil {
				return result, writeAPICompatConversionError(c, err, false)
			}
			for _, chunk := range chunks {
				frame, err := apicompat.ChatChunkToSSE(chunk)
				if err != nil {
					return result, err
				}
				if err := writeSSE(frame); err != nil {
					return result, err
				}
			}
		}
		if !anthropic {
			err = writeSSE("data: [DONE]\n\n")
		}
		return result, err
	}
	var response apicompat.ResponsesResponse
	if err = json.Unmarshal(writer.pending, &response); err != nil {
		return result, fmt.Errorf("decode OpenAI grant compatibility response: %w", err)
	}
	if err := apicompat.ValidateResponsesToolArguments(&response); err != nil {
		return result, writeAPICompatConversionError(c, err, anthropic)
	}
	if anthropic {
		c.JSON(http.StatusOK, apicompat.ResponsesToAnthropic(&response, originalModel))
	} else {
		c.JSON(http.StatusOK, apicompat.ResponsesToChatCompletions(&response, originalModel))
	}
	return result, nil
}
