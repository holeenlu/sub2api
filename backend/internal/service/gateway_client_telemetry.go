package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ClientTelemetryModeForward        = "forward"
	ClientTelemetryModeDisabled       = "disabled"
	ClaudeTelemetryLegacyPath         = "/api/event_logging/batch"
	ClaudeTelemetryBatchPath          = "/api/event_logging/v2/batch"
	ClientTelemetryMaxBodyBytes int64 = 1 << 20
)

var ErrInvalidClientTelemetry = errors.New("invalid client telemetry batch")

// ClientTelemetryCaptureAllowed reads the shared settings snapshot. Client
// privacy preferences take precedence, without touching account scheduling.
func (s *GatewayService) ClientTelemetryCaptureAllowed(incoming http.Header) bool {
	if strings.TrimSpace(incoming.Get("DNT")) == "1" || strings.TrimSpace(incoming.Get("Sec-GPC")) == "1" {
		return false
	}
	return s == nil || s.clientTelemetryEnabled == nil || s.clientTelemetryEnabled()
}

// ProcessClaudeTelemetry stores the original batch, then replaces its identity
// using the account selected by actual inference and sends it with that credential.
func (s *GatewayService) ProcessClaudeTelemetry(ctx context.Context, key *APIKey, path string, incoming http.Header, body []byte) error {
	if path != ClaudeTelemetryLegacyPath && path != ClaudeTelemetryBatchPath {
		return ErrInvalidClientTelemetry
	}
	if key == nil || key.ID <= 0 || key.UserID <= 0 {
		return ErrInvalidClientTelemetry
	}
	if !s.ClientTelemetryCaptureAllowed(incoming) {
		return nil
	}
	if len(body) == 0 || int64(len(body)) > ClientTelemetryMaxBodyBytes || !utf8.Valid(body) {
		return ErrInvalidClientTelemetry
	}
	var envelope struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Events) == 0 || len(envelope.Events) > 200 {
		return ErrInvalidClientTelemetry
	}
	headers := clientTelemetryHeaders(incoming)
	encodedHeaders, err := json.Marshal(headers)
	if err != nil || len(encodedHeaders) > 16*1024 {
		return ErrInvalidClientTelemetry
	}
	samples := make([]*ClientTelemetryLog, 0, len(envelope.Events))
	for _, raw := range envelope.Events {
		var event struct {
			Type string          `json:"event_type"`
			Data json.RawMessage `json:"event_data"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return ErrInvalidClientTelemetry
		}
		data := bytes.TrimSpace(event.Data)
		if (event.Type != "ClaudeCodeInternalEvent" && event.Type != "GrowthbookExperimentEvent") || len(data) < 2 || data[0] != '{' {
			return ErrInvalidClientTelemetry
		}
		sample, err := clientTelemetrySample(key, path, headers, raw)
		if err != nil {
			return err
		}
		samples = append(samples, sample)
	}
	if s == nil || s.clientTelemetryRepo == nil {
		return ErrClientTelemetryStorage
	}
	storeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.clientTelemetryRepo.CaptureClientTelemetry(storeCtx, samples); err != nil {
		return fmt.Errorf("%w: %w", ErrClientTelemetryStorage, err)
	}
	// The empty acknowledgement confirms durable local receipt. Forwarding is
	// best-effort and must not turn a saved upload into a client retry.
	s.forwardClaudeTelemetry(ctx, key, path, samples)
	return nil
}
