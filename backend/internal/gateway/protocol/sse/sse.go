// Package sse holds the server-sent events line splitter shared by the gateway
// service and the basispoints translator. basispoints cannot import service,
// so the splitter lives here.
package sse

import (
	"bufio"
	"bytes"
)

// ScanLines returns a bufio.SplitFunc for SSE lines. SSE has three line
// endings: CR, LF and CRLF. A CR is consumed immediately and its optional LF
// is swallowed on the next call, so a live CR-only stream does not wait for
// EOF. A UTF-8 BOM before the first line is dropped. The returned function is
// stateful; use one per scanner.
func ScanLines() bufio.SplitFunc {
	first, afterCR := true, false
	scanned := 0
	return func(data []byte, atEOF bool) (int, []byte, error) {
		offset := 0
		if afterCR && len(data) > 0 {
			afterCR = false
			if data[0] == '\n' {
				offset, data = 1, data[1:]
			}
		}
		// Scanner retains an unfinished token between calls. Search only bytes
		// received since the previous call, including after buffer compaction.
		i := bytes.IndexAny(data[scanned:], "\r\n")
		if i >= 0 {
			i += scanned
		}
		if i < 0 && !atEOF {
			scanned = len(data)
			return offset, nil, nil
		}
		if i < 0 && len(data) == 0 {
			return offset, nil, nil
		}
		scanned = 0
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
