package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"net/http"
)

type excelBPSAttachmentError struct {
	status  int
	message string
}

func (e *excelBPSAttachmentError) Error() string { return e.message }

func (s *OpenAIGatewayService) uploadExcelBPSAttachment(ctx context.Context, headers http.Header, media, payload string, size int64, proxyURL string, account *Account) (string, error) {
	req, err := basispoints.NewAttachmentRequest(ctx, headers, media, payload, size)
	if err != nil {
		return "", &excelBPSAttachmentError{502, "Excel BPS attachment request could not be built"}
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", &excelBPSAttachmentError{502, "Excel BPS attachment upload failed"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests && s.rateLimitService != nil {
			stateCtx, cancel := openAIAccountStateContext(ctx)
			s.rateLimitService.handle429Cooldown(stateCtx, account, resp.Header, nil)
			cancel()
		}
		if resp.StatusCode == http.StatusForbidden {
			s.disableExcelBPSOn403(ctx, account)
		}
		status := resp.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return "", &excelBPSAttachmentError{status, fmt.Sprintf("Excel BPS attachment upload returned HTTP %d; no generation was sent", resp.StatusCode)}
	}
	id, err := basispoints.ReadAttachmentID(resp.Body)
	if err != nil {
		return "", &excelBPSAttachmentError{502, "Excel BPS attachment response has no valid file ID"}
	}
	return id, nil
}
