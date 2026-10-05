package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type CodexDiagnosticItem struct {
	FingerprintCommit string              `json:"fingerprint_commit,omitempty"`
	ExpectedCount     int                 `json:"expected_count,omitempty"`
	DurationMS        int64               `json:"duration_ms"`
	Model             string              `json:"model"`
	Status            string              `json:"status"`
	Reason            string              `json:"reason,omitempty"`
	PredictedModel    string              `json:"predicted_model,omitempty"`
	Probability       float64             `json:"probability,omitempty"`
	ParsedCount       int                 `json:"parsed_number_count,omitempty"`
	HTTPStatus        int                 `json:"http_status,omitempty"`
	GatewayErrorCode  string              `json:"gateway_error_code,omitempty"`
	Harvest           *CodexTicketAttempt `json:"harvest,omitempty"`
}

func CodexDiagnosticGatewayError(raw []byte) string {
	if len(raw) > 16<<10 {
		raw = raw[:16<<10]
	}
	for _, path := range []string{"reason", "error.code"} {
		code := gjson.GetBytes(raw, path).String()
		if code == "" || len(code) > 64 {
			continue
		}
		for _, character := range code {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' {
				continue
			}
			code = ""
			break
		}
		if code != "" {
			return code
		}
	}
	return ""
}

func CodexDiagnosticOutput(raw []byte) (string, bool) {
	completed := false
	failed := false
	streamed := false
	var output strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		streamed = true
		switch gjson.Get(payload, "type").String() {
		case "response.failed", "response.incomplete", "error":
			failed = true
		case "response.output_text.delta":
			_, _ = output.WriteString(gjson.Get(payload, "delta").String())
		case "response.completed":
			completed = true
			if output.Len() == 0 {
				gjson.Get(payload, "response.output").ForEach(func(_, message gjson.Result) bool {
					message.Get("content").ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "output_text" {
							_, _ = output.WriteString(part.Get("text").String())
						}
						return true
					})
					return true
				})
			}
		}
	}
	if streamed {
		return output.String(), completed && !failed && scanner.Err() == nil
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return "", false
	}
	if gjson.GetBytes(raw, "status").String() != "completed" {
		return "", false
	}
	if text := gjson.GetBytes(raw, "output_text").String(); text != "" {
		return text, true
	}
	return gjson.GetBytes(raw, "output.0.content.0.text").String(), true
}

// RunCodexDiagnosticProbe shares the same pinned-account, billed gateway path for
// interactive and scheduled runs. It returns metadata, never prompts or keys.
func RunCodexDiagnosticProbe(ctx context.Context, gateway *OpenAIGatewayService, router http.Handler, key *APIKey, accountID int64, model, remoteAddr, host string, generateChallenge func() (ModelTraceChallenge, error)) (item CodexDiagnosticItem) {
	item = CodexDiagnosticItem{Model: model, Status: "failed"}
	start := time.Now()
	defer func() { item.DurationMS = time.Since(start).Milliseconds() }()
	probeCtx, templateErr := gateway.PrepareCodexProbeContext(ctx)
	if templateErr != nil {
		item.Reason = "template_unavailable"
		if errors.Is(templateErr, ErrCodexProbeTemplateInvalid) {
			item.Reason = "template_invalid"
		}
		return item
	}
	challenge, challengeErr := generateChallenge()
	if challengeErr != nil {
		item.Reason = "challenge_generation_failed"
		return item
	}
	item.ExpectedCount = challenge.ExpectedCount
	body, probeHeaders, buildErr := gateway.BuildCodexDiagnosticRequest(probeCtx, accountID, model, challenge)
	if buildErr != nil {
		item.Reason = "request_build_failed"
		if errors.Is(buildErr, ErrCodexProbeIdentity) {
			item.Reason = "identity_resolution_failed"
		}
		return item
	}
	// The normal gateway owns ticket policy, scheduling, injection and invalidation.
	// Diagnostics only pin the target account and analyze the model response.
	modelCtx, cancel := context.WithTimeout(WithCodexTicketDiagnostic(probeCtx, accountID), 120*time.Second)
	request, requestErr := http.NewRequestWithContext(modelCtx, http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if requestErr != nil {
		cancel()
		item.Reason = "request_build_failed"
		return item
	}
	for name, values := range probeHeaders {
		request.Header[name] = values
	}
	request.Header.Set("Authorization", "Bearer "+key.Key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.RemoteAddr = remoteAddr
	request.Host = host
	writer := &diagnosticRecorder{ResponseRecorder: httptest.NewRecorder()}
	router.ServeHTTP(writer, request)
	cancel()
	item.HTTPStatus = writer.Code
	if writer.Code < 200 || writer.Code >= 300 {
		item.Reason = "gateway_request_failed"
		item.GatewayErrorCode = CodexDiagnosticGatewayError(writer.Body.Bytes())
		return item
	}
	output, readErr := io.ReadAll(io.LimitReader(writer.Body, 2<<20))
	if readErr != nil {
		item.Reason = "response_read_failed"
		return item
	}
	if writer.overflow {
		item.Reason = "response_too_large"
		return item
	}
	diagnosticText, complete := CodexDiagnosticOutput(output)
	if !complete {
		item.Reason = "response_incomplete"
		return item
	}
	prediction, commit, predictErr := ModelTracePredictCommitted(diagnosticText, challenge.ExpectedCount)
	item.FingerprintCommit = commit
	item.ParsedCount = prediction.ParsedCount
	if predictErr != nil {
		item.Reason = "insufficient_numbers"
		return item
	}
	item.PredictedModel, item.Probability = prediction.Model, prediction.Probability
	if prediction.Model == model {
		item.Status = "normal"
	} else {
		item.Status = "degraded"
		item.Reason = "fingerprint_mismatch"
	}
	return item
}

// Keep retained output bounded even if an upstream sends a large event stream.
type diagnosticRecorder struct {
	*httptest.ResponseRecorder
	overflow bool
}

func (w *diagnosticRecorder) Write(b []byte) (int, error) {
	if w.Code == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.Body.Len()+len(b) > 2<<20 {
		w.overflow = true
		return len(b), nil
	}
	return w.ResponseRecorder.Write(b)
}
func (w *diagnosticRecorder) WriteString(v string) (int, error) { return w.Write([]byte(v)) }
