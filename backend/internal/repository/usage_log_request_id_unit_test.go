package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPrepareUsageLogInsertCanonicalizesLongRequestID(t *testing.T) {
	longID := "client:" + strings.Repeat("long-response-id-", 20)
	log := &service.UsageLog{RequestID: longID, UserID: 1, APIKeyID: 2, AccountID: 3, Model: "gpt-6-astra"}
	prepared := prepareUsageLogInsert(log)
	if len(prepared.requestID) > service.UsageLogRequestIDMaxBytes {
		t.Fatalf("request id exceeds database limit: %d", len(prepared.requestID))
	}
	if prepared.requestID != service.CanonicalUsageLogRequestID(longID) {
		t.Fatalf("request id was not canonicalized consistently: %q", prepared.requestID)
	}
	if prepared.requestID == service.CanonicalUsageLogRequestID(longID+"-different") {
		t.Fatal("different long request ids collapsed to the same billing identity")
	}
}

func TestPrepareUsageLogInsertPreservesShortRequestID(t *testing.T) {
	const id = "resp_short"
	log := &service.UsageLog{RequestID: id, UserID: 1, APIKeyID: 2, AccountID: 3, Model: "gpt-6-astra"}
	prepared := prepareUsageLogInsert(log)
	if prepared.requestID != id || log.RequestID != id {
		t.Fatalf("short request id changed: prepared=%q log=%q", prepared.requestID, log.RequestID)
	}
}
