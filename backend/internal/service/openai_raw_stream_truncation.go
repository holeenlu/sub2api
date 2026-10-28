package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// openAIRawStreamTruncatedUpstreamMessage 是 raw CC 直转路径上游截断的 ops 消息。
const openAIRawStreamTruncatedUpstreamMessage = "Upstream Chat Completions stream ended before any terminal chunk"

// openAIRawStreamTerminalState is shared by native Chat and both converters.
// Measured usage is independent of completion: only a usage-only trailer, DONE,
// or the finish reasons of every observed choice can prove success. Keep the
// first explicit error even if a later frame claims completion.
type openAIRawStreamTerminalState struct {
	sawDataLine     bool
	sawDone         bool
	sawUsageTrailer bool
	choices         map[int64]bool
	failure         error
}

func (t *openAIRawStreamTerminalState) ObserveDataLine(payload string) {
	if t == nil {
		return
	}
	t.sawDataLine = true
	if payload == "[DONE]" {
		t.sawDone = true
		return
	}
	if eventError := gjson.Get(payload, "error"); (eventError.Exists() && eventError.Type != gjson.Null) || gjson.Get(payload, "type").String() == "error" {
		t.fail()
	}
	if isOpenAIChatUsageOnlyStreamChunk(payload) && gjson.Get(payload, "usage").IsObject() {
		t.sawUsageTrailer = true
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		if t.choices == nil {
			t.choices = make(map[int64]bool)
		}
		index := choice.Get("index").Int()
		if _, exists := t.choices[index]; !exists && len(t.choices) >= 16*1024 {
			t.failure = errors.New("upstream Chat Completions choice limit exceeded")
			return
		}
		t.choices[index] = t.choices[index] || strings.TrimSpace(choice.Get("finish_reason").String()) != ""
	}
}

func (t *openAIRawStreamTerminalState) fail() {
	if t.failure == nil {
		t.failure = errors.New("upstream Chat Completions error event")
	}
}

func (t *openAIRawStreamTerminalState) Terminated() bool {
	if t == nil || t.failure != nil {
		return false
	}
	if t.sawDone || t.sawUsageTrailer {
		return true
	}
	if len(t.choices) == 0 {
		return false
	}
	for _, finished := range t.choices {
		if !finished {
			return false
		}
	}
	return true
}

// IsTruncated retains the existing non-SSE pass-through only when the caller
// has identified a non-SSE response and actually forwarded bytes. SSE metadata
// is never proof of successful generation.
func (t *openAIRawStreamTerminalState) IsTruncated(allowNonSSEBody bool) bool {
	if t == nil || t.Terminated() {
		return false
	}
	return t.sawDataLine || !allowNonSSEBody
}

// newOpenAIRawStreamTruncatedFailoverError 处理"上游截断且尚未向客户端写出任何
// 字节"的情况：响应头还没提交，可以透明换号重试，客户端不会看到半截流。
func newOpenAIRawStreamTruncatedFailoverError(
	c *gin.Context,
	account *Account,
	upstreamRequestID string,
	cause error,
) *UpstreamFailoverError {
	recordOpenAIRawStreamTruncation(c, account, upstreamRequestID, cause, "failover")

	headers := http.Header{}
	if id := strings.TrimSpace(upstreamRequestID); id != "" {
		headers.Set("x-request-id", id)
	}
	return &UpstreamFailoverError{
		StatusCode:      http.StatusBadGateway,
		ResponseBody:    openAIRawStreamTruncatedErrorBody(cause),
		ResponseHeaders: headers,
	}
}

// recordOpenAIRawStreamTruncation 把上游截断记入 ops 上下文，使其在错误日志与
// 账号健康度中可见——这正是此前"HTTP 200 假成功"丢掉的信息。
func recordOpenAIRawStreamTruncation(
	c *gin.Context,
	account *Account,
	upstreamRequestID string,
	cause error,
	kind string,
) {
	if c == nil {
		return
	}
	message := openAIRawStreamTruncatedMessage(cause)
	platform := PlatformOpenAI
	accountID := int64(0)
	accountName := ""
	if account != nil {
		platform = account.Platform
		accountID = account.ID
		accountName = account.Name
	}

	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           platform,
		AccountID:          accountID,
		AccountName:        accountName,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	})
}

// openAIRawStreamTruncatedMessage 拼出 ops 消息：干净 EOF 没有底层错误可带，
// 传输层错误（connection reset / http2 stream error）则保留原因以便定位。
func openAIRawStreamTruncatedMessage(cause error) string {
	if cause == nil || errors.Is(cause, ErrOpenAIUpstreamStreamTruncated) {
		return openAIRawStreamTruncatedUpstreamMessage
	}
	return openAIRawStreamTruncatedUpstreamMessage + ": " + cause.Error()
}

// openAIRawStreamTruncatedErrorBody 构造 failover 错误体，code/message 与
// 写出后走 openAIUpstreamStreamReadError 的客户端分类保持一致。
func openAIRawStreamTruncatedErrorBody(cause error) []byte {
	code, message := classifyOpenAIUpstreamStreamReadError(cause)
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","code":"` + OpenAIUpstreamStreamTruncatedCode +
			`","message":"Upstream response stream ended before completion"}}`)
	}
	return body
}

// Metadata and empty delta fields do not satisfy the first-output budget.
func chatStreamPayloadHasSemanticOutput(payload string) bool {
	for _, choice := range gjson.Get(payload, "choices").Array() {
		delta := choice.Get("delta")
		for _, field := range []string{"content", "reasoning_content", "reasoning", "refusal"} {
			if delta.Get(field).String() != "" {
				return true
			}
		}
		if len(delta.Get("tool_calls").Array()) > 0 || delta.Get("function_call.name").String() != "" || delta.Get("function_call.arguments").String() != "" {
			return true
		}
	}
	return false
}
