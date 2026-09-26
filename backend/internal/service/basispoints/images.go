package basispoints

import (
	"fmt"
	"net/url"
	"strings"
)

// HTTPS references and opaque provider file IDs pass through unchanged.
func validateImage(part object) error {
	if err := validateImageDetail(part); err != nil {
		return err
	}
	if id, exists := part["file_id"]; exists {
		if _, hasURL := part["image_url"]; hasURL || !validFileID(text(id)) {
			return fmt.Errorf("basispoints image requires exactly one valid file_id or HTTPS image_url")
		}
		return nil
	}
	raw, ok := part["image_url"].(string)
	if !ok || raw == "" {
		return fmt.Errorf("basispoints input_image requires an HTTPS image_url or file_id")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
		return fmt.Errorf("basispoints inline image must be uploaded before request preparation")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.TrimSpace(raw) != raw {
		return fmt.Errorf("basispoints input_image requires an absolute HTTPS image URL without embedded credentials")
	}
	return nil
}

func validFileID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func validateImageDetail(part object) error {
	if detail, exists := part["detail"]; exists && detail != nil {
		switch text(detail) {
		case "auto", "low", "high", "original":
		default:
			return fmt.Errorf("basispoints image detail must be auto, low, high or original")
		}
	}
	return nil
}
