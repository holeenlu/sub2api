package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketProbeStreamBody struct {
	io.Reader
	closed bool
}

func (body *codexTicketProbeStreamBody) Close() error {
	body.closed = true
	return nil
}

type codexTicketProbeStreamReadError struct{}

func (codexTicketProbeStreamReadError) Read([]byte) (int, error) {
	return 0, errors.New("test upstream stream interrupted")
}

func TestCodexTicketHarvestRejectsIncompleteOrFailedStream(t *testing.T) {
	// Use a real reference sample that the classifier accepts. Every rejected
	// stream has enough matching output and ticket material to expose the former
	// false success; rejection must preserve an existing verified generation.
	raw, err := os.ReadFile("testdata/modeltrace_gpt_reference.json")
	require.NoError(t, err)
	var samples []struct{ Model, Text string }
	require.NoError(t, json.Unmarshal(raw, &samples))
	var output string
	for _, sample := range samples {
		if sample.Model == "gpt-6-astra" {
			output = sample.Text
			break
		}
	}
	require.NotEmpty(t, output)
	deltaJSON, err := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": output})
	require.NoError(t, err)
	delta := "data: " + string(deltaJSON) + "\n\n"
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	for _, test := range []struct {
		name   string
		tail   string
		error  bool
		noBody bool
	}{
		{name: "EOF after matching deltas"},
		{name: "DONE without completed", tail: "data: [DONE]\n\n"},
		{name: "error after matching deltas", tail: "data: {\"type\":\"error\",\"message\":\"private upstream detail\"}\n\n"},
		{name: "failed after matching deltas", tail: "data: {\"type\":\"response.failed\"}\n\n"},
		{name: "incomplete after matching deltas", tail: "data: {\"type\":\"response.incomplete\"}\n\n"},
		{name: "contradictory terminal status", tail: "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"failed\"}}\n\n"},
		{name: "malformed terminal event", tail: "data: {\"type\":\"response.completed\"\n\n"},
		{name: "read error after matching deltas", error: true},
		{name: "terminal beyond read limit", tail: strings.Repeat(": keepalive\n\n", 180000) + completed},
		{name: "missing response body", noBody: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reader io.Reader = strings.NewReader(delta + test.tail)
			if test.error {
				reader = io.MultiReader(reader, codexTicketProbeStreamReadError{})
			}
			body := &codexTicketProbeStreamBody{Reader: reader}
			upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				response.Body = body
				if test.noBody {
					response.Body = nil
				}
				return response, nil
			}}
			svc, account, history := challengeHarvestService(t, upstream)
			previous := verifiedTicket(account, "gpt-6-astra", "previous-state", "")
			account.Extra = map[string]any{openAICodexTicketExtraKey(previous.Model): previous}
			result, err := svc.runCodexTicketAttemptWithChallengeGenerator(context.Background(), account, previous.Model, "manual", func() (ModelTraceChallenge, error) {
				return ModelTraceChallenge{Prompt: "test challenge", ExpectedCount: 332}, nil
			})
			require.NoError(t, err)
			require.Equal(t, "error", result.Outcome)
			require.Equal(t, "request", result.ReasonCode)
			require.Nil(t, result.TicketGenerationID)
			require.Same(t, previous, account.Extra[openAICodexTicketExtraKey(previous.Model)])
			require.True(t, result.HistoryRecorded)
			require.Len(t, history.attempts, 1)
			require.Equal(t, result.Outcome, history.attempts[0].Outcome)
			if !test.noBody {
				require.True(t, body.closed)
			}
		})
	}
}

func TestCodexTicketProbeCompletionUsesFinalOutputAndClosesWithoutMoreReads(t *testing.T) {
	for _, deltas := range []bool{false, true} {
		name := "completed output only"
		if deltas {
			name = "deltas and completed"
		}
		t.Run(name, func(t *testing.T) {
			stream := ""
			if deltas {
				stream = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"1 2 3\"}\n\n"
			}
			stream += "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"1 2 3\"}]}]}}\n\n"
			// Reading after the terminal event would fail. A completed response
			// must not wait for another network read or for [DONE]/EOF.
			body := &codexTicketProbeStreamBody{Reader: io.MultiReader(strings.NewReader(stream), codexTicketProbeStreamReadError{})}
			upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				response.Body = body
				return response, nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
			output, _, _, status, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "token", "gpt-6-astra", "http://proxy.example.com:8080", ticketProbeChallenge(t), time.Second)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, "1 2 3", output)
			require.True(t, body.closed)
		})
	}
}
