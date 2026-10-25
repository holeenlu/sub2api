package sse

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fragmentReader struct {
	reader    io.Reader
	chunkSize int
}

func (r *fragmentReader) Read(dst []byte) (int, error) {
	return r.reader.Read(dst[:min(len(dst), r.chunkSize)])
}

func scanAll(t *testing.T, input string, chunkSize int) []string {
	t.Helper()
	scanner := bufio.NewScanner(&fragmentReader{reader: strings.NewReader(input), chunkSize: chunkSize})
	scanner.Split(ScanLines())
	scanner.Buffer(make([]byte, 16), len(input)+1)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	return lines
}

func TestScanLinesEndingsAndBOM(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string
	}{
		{"lf", "event: a\ndata: 1\n\n", []string{"event: a", "data: 1", ""}},
		{"cr", "event: a\rdata: 1\r\r", []string{"event: a", "data: 1", ""}},
		{"crlf", "event: a\r\ndata: 1\r\n\r\n", []string{"event: a", "data: 1", ""}},
		{"bare_cr_then_lf_is_one_ending", "a\r\nb\r\r\nc", []string{"a", "b", "", "c"}},
		{"bom_only_first_line", "\ufeffdata: 1\n\ufeffx\n", []string{"data: 1", "\ufeffx"}},
		{"eof_tail_without_ending", "data: 1\ntail", []string{"data: 1", "tail"}},
	} {
		for _, chunkSize := range []int{1, 2, 4096} {
			t.Run(fmt.Sprintf("%s_chunk_%d", tc.name, chunkSize), func(t *testing.T) {
				require.Equal(t, tc.want, scanAll(t, tc.input, chunkSize))
			})
		}
	}
}

func TestScanLinesIncrementalCursor(t *testing.T) {
	for _, ending := range []string{"\n", "\r", "\r\n", "\r\n\n"} {
		for _, chunkSize := range []int{1, 3, 127, 4096} {
			t.Run(fmt.Sprintf("ending_%q_chunk_%d", ending, chunkSize), func(t *testing.T) {
				long := strings.Repeat("x", 128*1024)
				lines := scanAll(t, "\ufeff"+long+ending+"tail"+ending+ending, chunkSize)
				require.Equal(t, long, lines[0])
				require.Contains(t, lines, "tail")
				require.Equal(t, "", lines[len(lines)-1])
			})
		}
	}
	// Returning the optional LF advance without a token changes Scanner's
	// buffer origin. The cursor must be relative to the remaining token.
	split := ScanLines()
	advance, token, err := split([]byte("first\r"), false)
	require.NoError(t, err)
	require.Equal(t, 6, advance)
	require.Equal(t, "first", string(token))
	advance, token, err = split([]byte("\npar"), false)
	require.NoError(t, err)
	require.Equal(t, 1, advance)
	require.Nil(t, token)
	advance, token, err = split([]byte("partial"), true)
	require.NoError(t, err)
	require.Equal(t, 7, advance)
	require.Equal(t, "partial", string(token))
}

// A CR-terminated line must be delivered before the next byte arrives; a live
// CR-only stream cannot wait for a following LF or EOF.
func TestScanLinesCRDoesNotWaitForNextByte(t *testing.T) {
	split := ScanLines()
	advance, token, err := split([]byte("data: 1\r"), false)
	require.NoError(t, err)
	require.Equal(t, 8, advance)
	require.Equal(t, "data: 1", string(token))
}
