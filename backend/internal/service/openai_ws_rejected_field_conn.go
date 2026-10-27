package service

import (
	"context"
	"sync"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// The passthrough relay owns turn settlement and delivery. This adapter holds
// only uncommitted metadata and replaces an explicitly rejected request on a
// fresh same-account connection, before the relay can observe a failed turn.
// A control write, parent reference, usage, unknown frame or any released frame
// closes that opportunity. It never retries a transport failure.
type openAIWSRejectedFieldConn struct {
	ctx            context.Context
	mu             sync.Mutex // serializes client writes with connection replacement
	inner          openaiwsv2.FrameConn
	redial         func(context.Context, []byte) (openaiwsv2.FrameConn, []byte, error)
	webSearchAdded func() bool
	writeTimeout   time.Duration
	request        []byte
	state          *openAIResponsesRejectedFieldRetryState
	generation     uint64
	blocked        bool
	added          bool
	closed         bool
	// Only the upstream reader owns these fields. A timed metadata flush keeps
	// the pending read alive, so it neither loses a frame nor poisons the socket.
	readGeneration uint64
	pending        chan openAIWSRepairFrame
	held           []openAIWSRepairFrame
	heldBytes      int
	heldUntil      time.Time
	ready          []openAIWSRepairFrame
}
type openAIWSRepairFrame struct {
	kind coderws.MessageType
	body []byte
	err  error
}

func (c *openAIWSRejectedFieldConn) WriteFrame(ctx context.Context, kind coderws.MessageType, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errOpenAIWSConnClosed
	}
	if kind == coderws.MessageText && gjson.GetBytes(body, "type").String() == "response.create" {
		c.generation++
		c.request = append([]byte(nil), body...)
		c.state = newOpenAIResponsesRejectedFieldRetryState(body)
		c.added = c.webSearchAdded != nil && c.webSearchAdded()
		c.blocked = gjson.GetBytes(body, "previous_response_id").String() != "" || gjson.GetBytes(body, "generate").Type == gjson.False
	} else {
		c.blocked = true
	}
	return c.inner.WriteFrame(ctx, kind, body)
}
func (c *openAIWSRejectedFieldConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.inner.Close()
}
func (c *openAIWSRejectedFieldConn) release(frame *openAIWSRepairFrame) {
	c.mu.Lock()
	c.blocked = true
	c.request = nil
	c.state = nil
	c.mu.Unlock()
	c.ready = append(c.ready, c.held...)
	if frame != nil {
		c.ready = append(c.ready, *frame)
	}
	c.held = nil
	c.heldBytes = 0
	c.heldUntil = time.Time{}
}
func (c *openAIWSRejectedFieldConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	for {
		if len(c.ready) > 0 {
			f := c.ready[0]
			c.ready[0] = openAIWSRepairFrame{}
			c.ready = c.ready[1:]
			if len(c.ready) == 0 {
				c.ready = nil
			}
			return f.kind, f.body, f.err
		}
		c.mu.Lock()
		generation, blocked, conn := c.generation, c.blocked, c.inner
		c.mu.Unlock()
		if generation != c.readGeneration {
			c.readGeneration = generation
			c.held = nil
			c.heldBytes = 0
			c.heldUntil = time.Time{}
		}
		if c.pending == nil {
			ch := make(chan openAIWSRepairFrame, 1)
			c.pending = ch
			go func() { kind, body, err := conn.ReadFrame(c.ctx); ch <- openAIWSRepairFrame{kind, body, err} }()
		}
		var timer *time.Timer
		var deadline <-chan time.Time
		if !c.heldUntil.IsZero() {
			timer = time.NewTimer(time.Until(c.heldUntil))
			deadline = timer.C
		}
		var f openAIWSRepairFrame
		select {
		case f = <-c.pending:
			c.pending = nil
		case <-deadline:
			c.release(nil)
			continue
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			_ = c.Close()
			<-c.pending
			c.pending = nil
			return coderws.MessageText, nil, ctx.Err()
		}
		if timer != nil {
			timer.Stop()
		}
		if f.err != nil {
			c.release(&f)
			continue
		}
		// The next turn can be written while this reader is idle between
		// turns. Associate its first event with the current write generation,
		// not the generation captured before the blocking socket read.
		c.mu.Lock()
		generation, blocked = c.generation, c.blocked
		c.mu.Unlock()
		if generation != c.readGeneration {
			c.readGeneration = generation
			c.held = nil
			c.heldBytes = 0
			c.heldUntil = time.Time{}
		}
		// Concatenated/invalid documents are opaque. Never infer rejection proof
		// from just their leading error or hide a trailing usage event.
		typ := gjson.GetBytes(f.body, "type").String()
		var usage OpenAIUsage
		parseOpenAIWSResponseUsageFromCompletedEvent(f.body, &usage)
		images := newOpenAIImageOutputCounter()
		images.AddSSEData(f.body)
		safe := !blocked && f.kind == coderws.MessageText && gjson.ValidBytes(f.body) && !openAIUsageHasTokens(&usage) && images.Count() == 0
		if safe && (typ == "error" || typ == "response.failed") {
			c.mu.Lock()
			if !c.blocked && c.generation == generation && !c.closed && ctx.Err() == nil {
				next, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(openAIWSRejectedFieldRetryHTTPStatus(f.body), c.request, f.body, c.added)
				if err == nil && changed && c.state.Allow(next) {
					_ = c.inner.Close() // late terminals must never enter the replacement turn
					replacement, repaired, retryErr := c.redial(ctx, next)
					if retryErr == nil {
						c.inner = replacement
						next = repaired
						writeCtx, cancel := context.WithTimeout(ctx, c.writeTimeout)
						retryErr = c.inner.WriteFrame(writeCtx, coderws.MessageText, next)
						cancel()
					}
					if retryErr == nil {
						c.request = next
						c.held = nil
						c.heldBytes = 0
						c.heldUntil = time.Time{}
						c.mu.Unlock()
						continue
					}
					c.mu.Unlock()
					return coderws.MessageText, nil, retryErr
				}
			}
			c.mu.Unlock()
		}
		hold := safe && (isOpenAIWSTurnMetadataEvent(typ) || typ == "response.created" || typ == "response.in_progress")
		if hold && c.heldBytes+len(f.body) <= openAIFirstOutputStageMaxBytes {
			if c.heldUntil.IsZero() {
				c.heldUntil = time.Now().Add(time.Second)
			}
			c.held = append(c.held, f)
			c.heldBytes += len(f.body)
			continue
		}
		c.release(&f)
	}
}
