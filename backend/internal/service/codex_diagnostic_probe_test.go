package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiagnosticRecorderBoundsRetainedOutput(t *testing.T) {
	w := &diagnosticRecorder{ResponseRecorder: httptest.NewRecorder()}
	_, err := w.Write([]byte(strings.Repeat("1 ", 1<<20)))
	require.NoError(t, err)
	_, err = w.Write([]byte("extra"))
	require.NoError(t, err)
	require.True(t, w.overflow)
	require.Equal(t, 2<<20, w.Body.Len())
}
func TestDiagnosticFailedTerminalNeverBecomesNormal(t *testing.T) {
	_, complete := CodexDiagnosticOutput([]byte("data: {\"type\":\"response.completed\"}\n\ndata: {\"type\":\"response.failed\"}\n\n"))
	require.False(t, complete)
	_, complete = CodexDiagnosticOutput([]byte("data: [DONE]\n\n"))
	require.False(t, complete)
}
