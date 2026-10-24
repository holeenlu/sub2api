package apicompat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Non-streaming: ResponsesResponse → ChatCompletionsResponse
// ---------------------------------------------------------------------------

// ResponsesToChatCompletions converts a Responses API response into a Chat
// Completions response. Text output items are concatenated into
// choices[0].message.content, refusal parts into choices[0].message.refusal;
// function_call items become tool_calls.
func ResponsesToChatCompletions(resp *ResponsesResponse, model string) *ChatCompletionsResponse {
	id := resp.ID
	if id == "" {
		id = generateChatCmplID()
	}

	out := &ChatCompletionsResponse{
		ID:          id,
		Object:      "chat.completion",
		Created:     time.Now().Unix(),
		Model:       model,
		ServiceTier: resp.ServiceTier,
	}

	var contentText string
	var refusalText string
	var reasoningText string
	var toolCalls []ChatToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch {
				case part.Type == "output_text" && part.Text != "":
					contentText += part.Text
				case part.Type == "refusal" && part.Refusal != "":
					refusalText += part.Refusal
				}
			}
		case "function_call", "custom_tool_call":
			arguments := item.Arguments
			if item.Type == "custom_tool_call" {
				arguments = item.Input
			}
			toolCalls = append(toolCalls, ChatToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ChatFunctionCall{
					Name:      item.Name,
					Arguments: arguments,
				},
			})
		case "reasoning":
			for _, s := range item.Summary {
				if s.Type == "summary_text" && s.Text != "" {
					reasoningText += s.Text
				}
			}
		case "web_search_call":
			// silently consumed — results already incorporated into text output
		}
	}

	msg := ChatMessage{Role: "assistant", Refusal: refusalText}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if contentText != "" {
		raw, _ := json.Marshal(contentText)
		msg.Content = raw
	}
	if reasoningText != "" {
		msg.ReasoningContent = reasoningText
	}

	finishReason := responsesStatusToChatFinishReason(resp.Status, resp.IncompleteDetails, toolCalls)

	out.Choices = []ChatChoice{{
		Index:        0,
		Message:      msg,
		FinishReason: finishReason,
	}}

	out.Usage = chatUsageFromResponsesUsage(resp.Usage)

	return out
}

func responsesStatusToChatFinishReason(status string, details *ResponsesIncompleteDetails, toolCalls []ChatToolCall) string {
	switch status {
	case "incomplete":
		if details != nil {
			switch details.Reason {
			case "max_output_tokens":
				return "length"
			case "content_filter":
				return "content_filter"
			}
		}
		return "stop"
	case "completed":
		if len(toolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	default:
		return "stop"
	}
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesStreamEvent → []ChatCompletionsChunk (stateful converter)
// ---------------------------------------------------------------------------

// ResponsesEventToChatState tracks state for converting a sequence of Responses
// SSE events into Chat Completions SSE chunks.
type ResponsesEventToChatState struct {
	ID              string
	Model           string
	Created         int64
	ServiceTier     string // upstream tier observed on response events; echoed on chunks
	SentRole        bool
	SawToolCall     bool
	SawText         bool
	Finalized       bool // true after finish chunk has been emitted
	tools           responsesToolCalls
	err             error
	textByPart      map[responsesTextPart]string
	refusalByPart   map[responsesTextPart]string
	reasoningByPart map[responsesTextPart]string
	IncludeUsage    bool
	Usage           *ChatUsage
}

// NewResponsesEventToChatState returns an initialised stream state.
func NewResponsesEventToChatState() *ResponsesEventToChatState {
	return &ResponsesEventToChatState{
		textByPart:      make(map[responsesTextPart]string),
		refusalByPart:   make(map[responsesTextPart]string),
		reasoningByPart: make(map[responsesTextPart]string),
		ID:              generateChatCmplID(),
		Created:         time.Now().Unix(),
	}
}

// ResponsesEventToChatChunks converts a single Responses SSE event into zero
// or more Chat Completions chunks, updating state as it goes.
func ResponsesEventToChatChunks(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt == nil || state == nil || state.Finalized || state.err != nil {
		return nil
	}
	switch evt.Type {
	case "response.created":
		return resToChatHandleCreated(evt, state)
	case "response.output_text.delta":
		return resToChatHandleTextDelta(evt, state)
	case "response.output_text.done", "response.refusal.done", "response.reasoning_summary_text.done", "response.reasoning_text.done":
		return resToChatRecoverPart(evt, state)
	case "response.output_item.done":
		return resToChatHandleItemDone(evt, state)
	case "response.refusal.delta":
		return resToChatHandleRefusalDelta(evt, state)
	case "response.output_item.added":
		return resToChatHandleOutputItemAdded(evt, state)
	case "response.function_call_arguments.delta",
		// custom/freeform 工具（如新版 apply_patch）的输入增量与 function_call 参数增量同形，
		// 均按 OutputIndex 累加到对应工具调用。
		"response.custom_tool_call_input.delta":
		return resToChatHandleFuncArgsDelta(evt, state)
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		return resToChatHandleFuncArgsDone(evt, state)
	case "response.reasoning_summary_text.delta",
		// 原始推理文本增量（真实 Codex 客户端消费的 reasoning_text.delta），
		// 与 reasoning summary 一样映射为 reasoning_content。
		"response.reasoning_text.delta":
		return resToChatHandleReasoningDelta(evt, state)
	// response.done 是 Realtime/WS 与项目透传路径使用的终止别名；
	// 普通 Responses HTTP SSE 的公开终止事件仍以 response.completed 为主。
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return resToChatHandleCompleted(evt, state)
	default:
		return nil
	}
}

// FinalizeResponsesChatStream emits a final chunk with finish_reason if the
// stream ended without a proper completion event (e.g. upstream disconnect).
// It is idempotent: if a completion event already emitted the finish chunk,
// this returns nil.
func FinalizeResponsesChatStream(state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if state == nil || state.Finalized || state.err != nil {
		return nil
	}
	var pending []ChatCompletionsChunk
	for _, call := range state.tools.ordered {
		if state.err = call.finish(); state.err != nil {
			return nil
		}
		pending = append(pending, resToChatEmitTool(call, state)...)
	}
	state.Finalized = true

	finishReason := "stop"
	if state.SawToolCall {
		finishReason = "tool_calls"
	}

	chunks := append(pending, makeChatFinishChunk(state, finishReason))

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:          state.ID,
			Object:      "chat.completion.chunk",
			Created:     state.Created,
			Model:       state.Model,
			ServiceTier: state.ServiceTier,
			Choices:     []ChatChunkChoice{},
			Usage:       state.Usage,
		})
	}

	return chunks
}

// ChatChunkToSSE formats a ChatCompletionsChunk as an SSE data line.
func ChatChunkToSSE(chunk ChatCompletionsChunk) (string, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", data), nil
}

// --- internal handlers ---

func resToChatHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Response != nil {
		if evt.Response.ID != "" {
			state.ID = evt.Response.ID
		}
		if state.Model == "" && evt.Response.Model != "" {
			state.Model = evt.Response.Model
		}
		if evt.Response.ServiceTier != "" {
			state.ServiceTier = evt.Response.ServiceTier
		}
	}
	// Emit the role chunk.
	if state.SentRole {
		return nil
	}
	state.SentRole = true

	role := "assistant"
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Role: role})}
}

func resToChatHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.SawText = true
	state.textByPart[resToAnthTextPartOf(evt)] += evt.Delta
	content := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Content: &content})}
}

func resToChatHandleRefusalDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.refusalByPart[resToAnthTextPartOf(evt)] += evt.Delta
	refusal := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Refusal: &refusal})}
}

// Err reports a conversion failure without exposing tool argument contents.
func (state *ResponsesEventToChatState) Err() error { return state.err }

func resToChatHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Item == nil {
		return nil
	}
	return resToChatHandleTool(evt, state)
}

func resToChatHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	return resToChatHandleTool(evt, state)
}

func resToChatHandleFuncArgsDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	return resToChatHandleTool(evt, state)
}

func resToChatHandleTool(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	call, err := state.tools.ingest(evt)
	if err != nil {
		state.err = err
		return nil
	}
	if call == nil {
		return nil
	}
	return resToChatEmitTool(call, state)
}

func resToChatEmitTool(call *responsesToolCall, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if call.item.CallID == "" || call.item.Name == "" {
		return nil
	}
	var chunks []ChatCompletionsChunk
	idx := call.index
	if !call.started {
		call.started = true
		state.SawToolCall = true
		chunks = append(chunks, makeChatDeltaChunk(state, ChatDelta{ToolCalls: []ChatToolCall{{
			Index: &idx, ID: call.item.CallID, Type: "function", Function: ChatFunctionCall{Name: call.item.Name},
		}}}))
	}
	if len(call.arguments) > len(call.emitted) {
		tail := call.arguments[len(call.emitted):]
		call.emitted = call.arguments
		chunks = append(chunks, makeChatDeltaChunk(state, ChatDelta{ToolCalls: []ChatToolCall{{Index: &idx, Function: ChatFunctionCall{Arguments: tail}}}}))
	}
	return chunks
}

func resToChatRecoverPart(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	part := resToAnthTextPartOf(evt)
	delivered := state.textByPart
	send := resToChatHandleTextDelta
	text := evt.Text
	if evt.Type == "response.refusal.done" {
		delivered, send, text = state.refusalByPart, resToChatHandleRefusalDelta, evt.Refusal
	} else if strings.Contains(evt.Type, "reasoning") {
		delivered, send = state.reasoningByPart, resToChatHandleReasoningDelta
		part.ContentIndex = evt.SummaryIndex
	}
	current := delivered[part]
	if !strings.HasPrefix(text, current) || text == current {
		return nil
	}
	copy := *evt
	copy.Delta = text[len(current):]
	return send(&copy, state)
}

func resToChatHandleItemDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Item == nil {
		return nil
	}
	if evt.Item.Type == "function_call" || evt.Item.Type == "custom_tool_call" {
		return resToChatHandleTool(evt, state)
	}
	var chunks []ChatCompletionsChunk
	for i, part := range evt.Item.Content {
		e := ResponsesStreamEvent{Type: "response.output_text.done", OutputIndex: evt.OutputIndex, ContentIndex: i, Text: part.Text, Refusal: part.Refusal}
		if part.Type == "refusal" {
			e.Type = "response.refusal.done"
		} else if part.Type != "output_text" {
			continue
		}
		chunks = append(chunks, resToChatRecoverPart(&e, state)...)
	}
	for i, part := range evt.Item.Summary {
		if part.Type == "summary_text" {
			chunks = append(chunks, resToChatRecoverPart(&ResponsesStreamEvent{Type: "response.reasoning_summary_text.done", OutputIndex: evt.OutputIndex, SummaryIndex: i, Text: part.Text}, state)...)
		}
	}
	return chunks
}

func resToChatHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.reasoningByPart[responsesTextPart{OutputIndex: evt.OutputIndex, ContentIndex: evt.SummaryIndex}] += evt.Delta
	reasoning := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{ReasoningContent: &reasoning})}
}

func resToChatHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Usage != nil {
		state.Usage = chatUsageFromResponsesUsage(evt.Usage)
	}
	if evt.Response != nil {
		if evt.Response.Usage != nil {
			state.Usage = chatUsageFromResponsesUsage(evt.Response.Usage)
		}
		if !state.SentRole && evt.Response.ID != "" {
			state.ID = evt.Response.ID
		}
		if state.Model == "" {
			state.Model = evt.Response.Model
		}
	}
	var recovered []ChatCompletionsChunk
	if evt.Response != nil {
		hadText, hadRefusal, hadReasoning := len(state.textByPart) > 0, len(state.refusalByPart) > 0, len(state.reasoningByPart) > 0
		for i, item := range evt.Response.Output {
			if item.Type == "message" {
				for j, part := range item.Content {
					key := responsesTextPart{i, j}
					_, knownText := state.textByPart[key]
					_, knownRefusal := state.refusalByPart[key]
					// Without a shared part identity, a renumbered snapshot must
					// not replay text that was already delivered.
					if part.Type == "output_text" && (!hadText || knownText) {
						recovered = append(recovered, resToChatRecoverPart(&ResponsesStreamEvent{Type: "response.output_text.done", OutputIndex: i, ContentIndex: j, Text: part.Text}, state)...)
					}
					if part.Type == "refusal" && (!hadRefusal || knownRefusal) {
						recovered = append(recovered, resToChatRecoverPart(&ResponsesStreamEvent{Type: "response.refusal.done", OutputIndex: i, ContentIndex: j, Refusal: part.Refusal}, state)...)
					}
				}
			} else if item.Type == "reasoning" && hadReasoning {
				for j, part := range item.Summary {
					if _, known := state.reasoningByPart[responsesTextPart{i, j}]; known {
						recovered = append(recovered, resToChatRecoverPart(&ResponsesStreamEvent{Type: "response.reasoning_summary_text.done", OutputIndex: i, SummaryIndex: j, Text: part.Text}, state)...)
					}
				}
			} else {
				recovered = append(recovered, resToChatHandleItemDone(&ResponsesStreamEvent{Type: "response.output_item.done", OutputIndex: i, Item: &item}, state)...)
			}
		}
	}
	for _, call := range state.tools.ordered {
		if state.err != nil {
			return nil
		}
		if state.err = call.finish(); state.err != nil {
			return nil
		}
		recovered = append(recovered, resToChatEmitTool(call, state)...)
	}
	if state.err != nil {
		return nil
	}
	state.Finalized = true
	finishReason := "stop"

	if evt.Response != nil {
		if evt.Response.ServiceTier != "" {
			state.ServiceTier = evt.Response.ServiceTier
		}

		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil {
				switch evt.Response.IncompleteDetails.Reason {
				case "max_output_tokens":
					finishReason = "length"
				case "content_filter":
					finishReason = "content_filter"
				}
			}
		case "completed":
			if state.SawToolCall {
				finishReason = "tool_calls"
			}
		}
	} else if state.SawToolCall {
		finishReason = "tool_calls"
	}

	chunks := append(recovered, makeChatFinishChunk(state, finishReason))

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:          state.ID,
			Object:      "chat.completion.chunk",
			Created:     state.Created,
			Model:       state.Model,
			ServiceTier: state.ServiceTier,
			Choices:     []ChatChunkChoice{},
			Usage:       state.Usage,
		})
	}

	return chunks
}

func chatUsageFromResponsesUsage(u *ResponsesUsage) *ChatUsage {
	if u == nil {
		return nil
	}
	usage := &ChatUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
	usage.PromptTokensDetails = promptDetailsFromResponses(u.InputTokensDetails)
	if u.CacheCreationInputTokens > 0 {
		if usage.PromptTokensDetails == nil {
			usage.PromptTokensDetails = &ChatTokenDetails{}
		}
		if usage.PromptTokensDetails.CacheWriteTokens == 0 && usage.PromptTokensDetails.CacheCreationTokens == 0 {
			usage.PromptTokensDetails.CacheCreationTokens = u.CacheCreationInputTokens
		}
	}
	usage.CompletionTokensDetails = completionDetailsFromResponses(u.OutputTokensDetails)
	return usage
}

// promptDetailsFromResponses maps Responses-API input_tokens_details into a
// Chat-Completions prompt_tokens_details. Returns nil when nothing would be
// emitted, so upstreams that do not break down prompt usage stay clean.
func promptDetailsFromResponses(src *ResponsesInputTokensDetails) *ChatTokenDetails {
	if src == nil {
		return nil
	}
	if src.CachedTokens == 0 && src.AudioTokens == 0 && src.CacheCreationTokens == 0 && src.CacheWriteTokens == 0 && src.CacheWriteTokensDetails == nil {
		return nil
	}
	return &ChatTokenDetails{
		CachedTokens:            src.CachedTokens,
		AudioTokens:             src.AudioTokens,
		CacheCreationTokens:     src.CacheCreationTokens,
		CacheWriteTokens:        src.CacheWriteTokens,
		CacheWriteTokensDetails: src.CacheWriteTokensDetails,
	}
}

// completionDetailsFromResponses maps Responses-API output_tokens_details
// into a Chat-Completions completion_tokens_details. Mirrors the OpenAI
// official CompletionUsage schema: reasoning_tokens, audio_tokens, and
// the predicted-outputs accepted/rejected counts. Returns nil when nothing
// would be emitted so non-reasoning, non-audio responses stay clean.
func completionDetailsFromResponses(src *ResponsesOutputTokensDetails) *ChatTokenDetails {
	if src == nil {
		return nil
	}
	if src.ReasoningTokens == 0 && src.AudioTokens == 0 &&
		src.AcceptedPredictionTokens == 0 && src.RejectedPredictionTokens == 0 {
		return nil
	}
	return &ChatTokenDetails{
		ReasoningTokens:          src.ReasoningTokens,
		AudioTokens:              src.AudioTokens,
		AcceptedPredictionTokens: src.AcceptedPredictionTokens,
		RejectedPredictionTokens: src.RejectedPredictionTokens,
	}
}

func makeChatDeltaChunk(state *ResponsesEventToChatState, delta ChatDelta) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:          state.ID,
		Object:      "chat.completion.chunk",
		Created:     state.Created,
		Model:       state.Model,
		ServiceTier: state.ServiceTier,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}
}

func makeChatFinishChunk(state *ResponsesEventToChatState, finishReason string) ChatCompletionsChunk {
	empty := ""
	return ChatCompletionsChunk{
		ID:          state.ID,
		Object:      "chat.completion.chunk",
		Created:     state.Created,
		Model:       state.Model,
		ServiceTier: state.ServiceTier,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{Content: &empty},
			FinishReason: &finishReason,
		}},
	}
}

// generateChatCmplID returns a "chatcmpl-" prefixed random hex ID.
func generateChatCmplID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// BufferedResponseAccumulator: accumulates SSE delta events for non-streaming
// paths where the terminal event may have empty output.
// ---------------------------------------------------------------------------

// BufferedResponseAccumulator reuses the streaming converter's reconciliation
// rules, then collects the deltas for a buffered response. This keeps done-only
// recovery, stable tool identity and argument validation identical on both paths.
type BufferedResponseAccumulator struct {
	text      strings.Builder
	refusal   strings.Builder
	reasoning strings.Builder
	state     *ResponsesEventToChatState
}

func NewBufferedResponseAccumulator() *BufferedResponseAccumulator {
	return &BufferedResponseAccumulator{state: NewResponsesEventToChatState()}
}

func (a *BufferedResponseAccumulator) Err() error { return a.state.Err() }

func (a *BufferedResponseAccumulator) ProcessEvent(event *ResponsesStreamEvent) {
	for _, chunk := range ResponsesEventToChatChunks(event, a.state) {
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != nil {
				a.text.WriteString(*choice.Delta.Content)
			}
			if choice.Delta.Refusal != nil {
				a.refusal.WriteString(*choice.Delta.Refusal)
			}
			if choice.Delta.ReasoningContent != nil {
				a.reasoning.WriteString(*choice.Delta.ReasoningContent)
			}
		}
	}
}

// HasContent reports whether any content has been accumulated.
func (a *BufferedResponseAccumulator) HasContent() bool {
	return a.text.Len() > 0 || a.refusal.Len() > 0 || len(a.state.tools.ordered) > 0 || a.reasoning.Len() > 0
}

// BuildOutput constructs a []ResponsesOutput from the accumulated delta
// content. The order matches what ResponsesToChatCompletions expects:
// reasoning → message → function_calls.
func (a *BufferedResponseAccumulator) BuildOutput() []ResponsesOutput {
	var out []ResponsesOutput

	if a.reasoning.Len() > 0 {
		out = append(out, ResponsesOutput{
			Type: "reasoning",
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: a.reasoning.String(),
			}},
		})
	}

	if a.text.Len() > 0 || a.refusal.Len() > 0 {
		var content []ResponsesContentPart
		if a.text.Len() > 0 {
			content = append(content, ResponsesContentPart{Type: "output_text", Text: a.text.String()})
		}
		if a.refusal.Len() > 0 {
			content = append(content, ResponsesContentPart{Type: "refusal", Refusal: a.refusal.String()})
		}
		out = append(out, ResponsesOutput{
			Type:    "message",
			Role:    "assistant",
			Content: content,
		})
	}

	for _, call := range a.state.tools.ordered {
		out = append(out, call.output())
	}

	return out
}

// SupplementResponseOutput fills resp.Output from accumulated stream content
// when the terminal event delivered an empty output array. It also fills empty
// function-call arguments from authoritative argument-done events.
func (a *BufferedResponseAccumulator) SupplementResponseOutput(resp *ResponsesResponse) {
	if resp == nil {
		return
	}
	if len(resp.Output) == 0 {
		if a.HasContent() {
			resp.Output = a.BuildOutput()
		}
		return
	}

	// The terminal event can carry a non-empty output array whose message has
	// no usable text. Trusting it as-is silently drops the text that already
	// streamed: the client gets an empty reply while usage still bills the
	// terminal output_tokens. Refill it from the accumulated deltas; non-empty
	// terminal text stays authoritative.
	if a.text.Len() > 0 && !responsesOutputHasText(resp.Output) {
		if !fillResponsesOutputText(resp.Output, a.text.String()) {
			resp.Output = append(resp.Output, ResponsesOutput{
				Type: "message",
				Role: "assistant",
				Content: []ResponsesContentPart{{
					Type: "output_text",
					Text: a.text.String(),
				}},
			})
		}
	}

	// A nonempty terminal output may still omit a streamed tool or refusal.
	// Match calls by identity before considering their original array position.
	for _, call := range a.state.tools.ordered {
		found := false
		for i := range resp.Output {
			item := &resp.Output[i]
			if item.Type != call.item.Type {
				continue
			}
			if item.CallID != "" && item.CallID != call.item.CallID {
				continue
			}
			if item.CallID == "" && item.ID != "" && item.ID != call.item.ID {
				continue
			}
			if item.CallID == "" && item.ID == "" && i != call.outputIndex {
				continue
			}
			if item.Type == "custom_tool_call" && item.Input == "" {
				item.Input = call.arguments
			}
			if item.Type == "function_call" && item.Arguments == "" {
				item.Arguments = call.arguments
			}
			found = true
			break
		}
		if !found {
			resp.Output = append(resp.Output, call.output())
		}
	}
	if a.refusal.Len() > 0 {
		for _, item := range resp.Output {
			for _, part := range item.Content {
				if part.Type == "refusal" && part.Refusal != "" {
					return
				}
			}
		}
		resp.Output = append(resp.Output, ResponsesOutput{Type: "message", Role: "assistant", Content: []ResponsesContentPart{{Type: "refusal", Refusal: a.refusal.String()}}})
	}

}

// responsesOutputHasText reports whether the terminal output already carries
// usable message text. Whitespace-only text does not count.
func responsesOutputHasText(output []ResponsesOutput) bool {
	for i := range output {
		if output[i].Type != "message" {
			continue
		}
		for _, part := range output[i].Content {
			if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
				return true
			}
		}
	}
	return false
}

// fillResponsesOutputText writes text into the first empty output_text part of
// the first message item, adding an output_text part when that message has
// none. It returns false when the output holds no message item, leaving the
// caller to append one.
func fillResponsesOutputText(output []ResponsesOutput, text string) bool {
	for i := range output {
		if output[i].Type != "message" {
			continue
		}
		for j := range output[i].Content {
			if output[i].Content[j].Type != "output_text" {
				continue
			}
			if strings.TrimSpace(output[i].Content[j].Text) == "" {
				output[i].Content[j].Text = text
				return true
			}
		}
		output[i].Content = append(output[i].Content, ResponsesContentPart{
			Type: "output_text",
			Text: text,
		})
		return true
	}
	return false
}
