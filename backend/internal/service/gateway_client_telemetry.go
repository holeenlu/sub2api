package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"
)

const (
	ClaudeTelemetryLegacyPath         = "/api/event_logging/batch"
	ClaudeTelemetryBatchPath          = "/api/event_logging/v2/batch"
	ClientTelemetryMaxBodyBytes int64 = 1 << 20
)

var ErrInvalidClientTelemetry = errors.New("invalid client telemetry batch")

// SaveClaudeTelemetry only retains real uploaded events locally. It never
// schedules an account, reads supplier credentials, or sends an upstream request.
func (s *GatewayService) SaveClaudeTelemetry(ctx context.Context, key *APIKey, path string, incoming http.Header, body []byte) error {
	if path != ClaudeTelemetryLegacyPath && path != ClaudeTelemetryBatchPath {
		return ErrInvalidClientTelemetry
	}
	if key == nil || key.ID <= 0 || key.UserID <= 0 || len(body) == 0 || int64(len(body)) > ClientTelemetryMaxBodyBytes || !utf8.Valid(body) {
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
	return nil
}
