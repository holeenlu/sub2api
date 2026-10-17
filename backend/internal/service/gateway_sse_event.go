package service

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// SSE has three line endings. Consume a CR immediately, then swallow its
// optional LF on the next call; a live CR-only stream must not wait for EOF.
func newSSEScanLines() bufio.SplitFunc {
	first, afterCR := true, false
	return func(data []byte, atEOF bool) (int, []byte, error) {
		offset := 0
		if afterCR && len(data) > 0 {
			afterCR = false
			if data[0] == '\n' {
				offset, data = 1, data[1:]
			}
		}
		i := bytes.IndexAny(data, "\r\n")
		if i < 0 && !atEOF {
			return offset, nil, nil
		}
		if i < 0 && len(data) == 0 {
			return offset, nil, nil
		}
		advance := len(data)
		token := data
		if i >= 0 {
			advance, token = i+1, data[:i]
			afterCR = data[i] == '\r'
		}
		if first {
			first = false
			token = bytes.TrimPrefix(token, []byte{0xef, 0xbb, 0xbf})
		}
		return offset + advance, token, nil
	}
}

// sseEventBuffer bounds a whole event, including comments and unknown
// fields. Scanner's per-line limit alone cannot bound a stream without delimiters.
// Reuse MaxLineSize as the event limit; reserve the terminating blank line.
type sseEventBuffer struct {
	lines []string
	size  int
}

func (b *sseEventBuffer) addLine(line string, limit int) error {
	// Bound the slice overhead as well as payload bytes. A metadata-only flood
	// must not allocate millions of strings under the large image byte limit.
	if len(b.lines) >= 16*1024 || len(line) >= limit-b.size-1 {
		return fmt.Errorf("upstream SSE event exceeded %d bytes: %w", limit, bufio.ErrTooLong)
	}
	b.size += len(line) + 1
	b.lines = append(b.lines, line)
	return nil
}

func (b *sseEventBuffer) reset() {
	clear(b.lines)
	b.lines = b.lines[:0]
	b.size = 0
}

// sseEvent 是一个完整 SSE 事件（以空行收尾）的解析结果。
//
// 旧实现把事件重建成 "event: X\ndata: Y\n\n" 两行，代价是：
//   - 多条 data 行只取第一条，其余被丢弃（SSE 规范是按 "\n" 拼接成一个值）；
//   - id / retry / 注释行以及重复的 event 行被整体丢弃。
//
// 这里保留原始行，未改写时原样回放，改写时只替换 data 字段，其余字段不动。
type sseEvent struct {
	lines     []string
	eventName string
	data      string
	hasEvent  bool
	hasData   bool
}

// parseSSEEvent 解析一个完整事件的所有行。调用方必须只传入以空行
// 分隔出来的完整事件；未以空行收尾的 EOF 残片不是完整事件。
func parseSSEEvent(lines []string) sseEvent {
	event := sseEvent{lines: lines}
	dataLines := make([]string, 0, 1)
	for _, line := range lines {
		field, value, ok := parseSSELine(line)
		if !ok {
			continue
		}
		switch field {
		case "event":
			event.hasEvent = true
			event.eventName = value
		case "data":
			event.hasData = true
			dataLines = append(dataLines, value)
		}
	}
	event.data = strings.Join(dataLines, "\n")
	return event
}

// parseSSELine 拆出一行的字段名与值。
//
// 字段名前的空白按 SSE 规范是非法的，但旧实现先 TrimSpace 整行再匹配，
// 对缩进过的上游是宽容的；这里保留同样的宽容度，避免收紧解析把既有上游
// 判成无 data 事件。值侧按规范只剥离紧跟冒号的一个 U+0020，其余空白留给
// 调用方按语义处理（JSON 解析本身容忍首尾空白）。
// 以 ":" 开头的注释行返回空字段名，不参与事件语义。
func parseSSELine(line string) (field, value string, ok bool) {
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return "", "", false
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		// 规范：没有冒号的行，整行是字段名，值为空串。
		return line, "", true
	}
	field = line[:colon]
	value = line[colon+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return field, value, true
}

// block 重建待发送的事件块。
//
// eventName 为调用方最终认定的事件名：上游省略 event 字段时，旧实现会按
// data 里的 type 补一行 "event: <type>"，部分客户端依赖它，故保留该补齐。
// data 为 nil 表示不改写正文（原样回放）；非 nil 时只替换首个 data 字段，
// 多条 data 行合并成规范定义的单个值，其余字段与注释保持原位。
func (e sseEvent) block(eventName string, data *string) string {
	// 快路径：绝大多数事件既不改写 data，也自带 event 字段，直接回放原始行，
	// 避免在每条 content_block_delta 上多分配一个切片。
	if data == nil && (e.hasEvent || eventName == "") {
		return strings.Join(e.lines, "\n") + "\n\n"
	}
	lines := make([]string, 0, len(e.lines)+1)
	if !e.hasEvent && eventName != "" {
		lines = append(lines, "event: "+eventName)
	}
	replaced := false
	for _, line := range e.lines {
		if data != nil {
			if field, _, ok := parseSSELine(line); ok && field == "data" {
				if !replaced {
					lines = append(lines, "data: "+*data)
					replaced = true
				}
				continue
			}
		}
		lines = append(lines, line)
	}
	if data != nil && !replaced {
		lines = append(lines, "data: "+*data)
	}
	return strings.Join(lines, "\n") + "\n\n"
}
