package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// UsageLogRequestIDMaxBytes is the database limit for usage_logs.request_id.
const UsageLogRequestIDMaxBytes = 64

// CanonicalUsageLogRequestID keeps the billing/deduplication identity stable
// while guaranteeing that it fits the historical varchar(64) column. Long
// client and Responses identifiers are reduced to a readable prefix plus a
// deterministic digest; truncation alone would merge unrelated money events.
func CanonicalUsageLogRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= UsageLogRequestIDMaxBytes {
		return raw
	}
	prefix := raw
	if colon := strings.IndexByte(raw, ':'); colon >= 0 && colon < 16 {
		prefix = raw[:colon+1]
	} else if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	maxHex := UsageLogRequestIDMaxBytes - len(prefix) - 1
	if maxHex < 2 {
		prefix = prefix[:UsageLogRequestIDMaxBytes-17]
		maxHex = 16
	}
	maxHex -= maxHex % 2
	digest := sha256.Sum256([]byte(raw))
	return prefix + "~" + hex.EncodeToString(digest[:])[:maxHex]
}
