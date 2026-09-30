package service

import (
	"context"
	"strings"
	"time"
)

type CatalogMediaObservation struct {
	Model       string    `json:"model"`
	Operation   string    `json:"operation"`
	DriverModel string    `json:"driver_model,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
}

// Only completed normal business traffic records evidence. Discovery never
// generates a billable image. Generation does not imply edit or mask support.
func (s *ModelCatalogService) RecordMediaSuccess(ctx context.Context, a *Account, result *OpenAIForwardResult, endpoint string) error {
	if s == nil || a == nil || result == nil || result.ImageCount <= 0 {
		return nil
	}
	model := strings.TrimSpace(result.BillingModel)
	if model == "" {
		return nil
	}
	operation := "responses/image_generation"
	if strings.Contains(endpoint, "images/generations") {
		operation = "images/generations"
	}
	if strings.Contains(endpoint, "edits") {
		operation = "images/edits"
	}
	source, err := resolveCredentialAccount(ctx, s.accounts, a)
	if err != nil {
		return err
	}
	return s.repo.ObserveMedia(ctx, a.ID, modelCatalogScope(a, source), CatalogMediaObservation{Model: model, Operation: operation, DriverModel: result.UpstreamModel, ObservedAt: time.Now().UTC()})
}
