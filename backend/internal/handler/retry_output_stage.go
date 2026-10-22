package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Stage only pre-content SSE metadata. Both a byte ceiling and a timer commit
// it; after commitment the existing writer guards forbid regeneration. This
// preserves message/response identity instead of removing the failover guard.
type retryOutputStage struct {
	gin.ResponseWriter
	mu                sync.Mutex
	buffer            bytes.Buffer
	committed, closed bool
	timer             *time.Timer
	err               error
	ctx               context.Context
}

const retryMetadataLimit = 32 << 10
const retryMetadataHold = time.Second

func beginRetryOutputStage(c *gin.Context) func(error) error {
	original := c.Writer
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	w := &retryOutputStage{ResponseWriter: original, ctx: ctx}
	c.Writer = w
	return func(err error) error {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return err
		}
		w.closed = true
		if w.timer != nil {
			w.timer.Stop()
		}
		var failover *service.UpstreamFailoverError
		if err == nil || (!errors.As(err, &failover) && !service.IsTurnBudgetError(err)) {
			w.commitLocked()
		}
		w.buffer.Reset()
		writeErr := w.err
		w.mu.Unlock()
		c.Writer = original
		if writeErr != nil {
			return writeErr
		}
		return err
	}
}

func (w *retryOutputStage) commitLocked() {
	if w.committed {
		return
	}
	w.committed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.buffer.Len() > 0 {
		_, w.err = w.ResponseWriter.Write(w.buffer.Bytes())
		w.buffer.Reset()
		w.ResponseWriter.Flush()
	}
}

func (w *retryOutputStage) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, http.ErrHandlerTimeout
	}
	if w.committed {
		if retryStageHasContent(p) || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
			service.MarkTurnOutputCommitted(w.ctx)
		}
		return w.ResponseWriter.Write(p)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") || (gjson.ValidBytes(p) && gjson.ParseBytes(p).IsObject()) || w.buffer.Len()+len(p) > retryMetadataLimit {
		if retryStageHasContent(p) || gjson.ValidBytes(p) || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
			service.MarkTurnOutputCommitted(w.ctx)
		}
		w.commitLocked()
		return w.ResponseWriter.Write(p)
	}
	_, _ = w.buffer.Write(p)
	if retryStageHasContent(w.buffer.Bytes()) {
		service.MarkTurnOutputCommitted(w.ctx)
		w.commitLocked()
	} else if w.timer == nil {
		w.timer = time.AfterFunc(retryMetadataHold, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			if !w.closed {
				w.commitLocked()
			}
		})
	}
	return len(p), w.err
}

func (w *retryOutputStage) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *retryOutputStage) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		w.ResponseWriter.Flush()
	}
}
func (w *retryOutputStage) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *retryOutputStage) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}
func (w *retryOutputStage) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Size()
}
func (w *retryOutputStage) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Written()
}
func (w *retryOutputStage) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Status()
}

// retryStageEventPayloads 按 SSE 规范还原每个事件的 data 值：事件以空行分隔，
// 事件内多条 data 行用 "\n" 拼成一个值。
//
// 逐行独立判断是不够的：多行 data 事件的单独一行通常不是合法 JSON，会被整条
// 跳过，于是暂存层认为"没有内容"，而服务层（gateway_upstream_response.go 的
// parseAnthropicSSEEvent）已经按规范拼接并取到了 usage——两层对同一个事件的
// 理解必须一致，否则提交判断会和实际写出的内容脱节。
func retryStageEventPayloads(p []byte) [][]byte {
	payloads := make([][]byte, 0, 4)
	var current [][]byte
	flush := func() {
		if len(current) > 0 {
			payloads = append(payloads, bytes.Join(current, []byte("\n")))
			current = nil
		}
	}
	for _, line := range bytes.Split(p, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			flush()
			continue
		}
		line = bytes.TrimLeft(line, " \t")
		field, value, _ := bytes.Cut(line, []byte(":"))
		if !bytes.Equal(field, []byte("data")) {
			continue
		}
		// 规范只剥离紧跟冒号的一个 U+0020，且是逐行剥离；与服务层
		// parseAnthropicSSELine 保持同一套语义。
		value = bytes.TrimPrefix(value, []byte(" "))
		current = append(current, value)
	}
	// 结尾残片同样参与判断：这里只决定"是否已经写出过内容"，
	// 与服务层是否把它当成完整事件派发是两回事。
	flush()
	return payloads
}

func retryStageHasContent(p []byte) bool {
	for _, payload := range retryStageEventPayloads(p) {
		data := bytes.TrimSpace(payload)
		if bytes.Equal(data, []byte("[DONE]")) {
			return true
		}
		if !gjson.ValidBytes(data) {
			continue
		} // a partial frame stays bounded
		event := gjson.ParseBytes(data)
		switch event.Get("type").String() {
		case "message_start":
			usage := event.Get("message.usage")
			if usage.Get("input_tokens").Int() > 0 || usage.Get("output_tokens").Int() > 0 || usage.Get("cache_creation_input_tokens").Int() > 0 || usage.Get("cache_read_input_tokens").Int() > 0 ||
				usage.Get("cache_creation.ephemeral_5m_input_tokens").Int() > 0 || usage.Get("cache_creation.ephemeral_1h_input_tokens").Int() > 0 {
				return true // Preserve observed cost and the provider's partial-usage result.
			}
			continue
		case "ping", "response.created", "response.in_progress", "error":
			continue
		case "content_block_start":
			block := event.Get("content_block")
			if block.Get("type").String() == "text" && block.Get("text").String() == "" {
				continue
			}
			return true
		case "":
			if event.Get("error").Exists() {
				continue
			}
			for _, choice := range event.Get("choices").Array() {
				if choice.Get("finish_reason").Type == gjson.String {
					return true
				}
				delta := choice.Get("delta")
				if delta.Get("content").String() != "" || delta.Get("reasoning_content").String() != "" || delta.Get("tool_calls").Exists() || delta.Get("function_call").Exists() {
					return true
				}
			}
			if event.Get("candidates").Exists() || event.Get("usageMetadata").Exists() || event.Get("usage").Exists() {
				return true
			}
		default:
			return true
		}
	}
	return false
}
