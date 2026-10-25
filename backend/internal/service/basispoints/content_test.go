package basispoints

import (
	"errors"
	"strings"
	"testing"
)

func TestEncryptedContentValidationExposesSafePath(t *testing.T) {
	b := &Bridge{}
	err := b.validateHistoryContent([]any{object{
		"type":    "encrypted_content",
		"content": "opaque-secret-payload",
	}}, 103, "content")
	var validationErr *ContentValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ContentValidationError, got %T: %v", err, err)
	}
	if validationErr.Path != "input[103].content[0]" || validationErr.ContentType != "encrypted_content" {
		t.Fatalf("unexpected location: %+v", validationErr)
	}
	if strings.Contains(err.Error(), "opaque-secret-payload") {
		t.Fatal("validation error leaked encrypted payload")
	}
	for _, advice := range []string{"for old history", "Ignore encrypted content in history", "this account's Excel/BPS settings", "omitted content will be unavailable", "original plaintext for the current task", "without a multi-agent v2 override"} {
		if !strings.Contains(err.Error(), advice) {
			t.Fatalf("missing recovery advice %q: %s", advice, err)
		}
	}
}
