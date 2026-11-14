package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAICloudSettlementArchiveRepository = (*usageBillingRepository)(nil)

// Preserve the exact payload, including malformed JSON. A changed payload at
// the same root is a separate fact; it never overwrites earlier evidence.
func (r *usageBillingRepository) StoreCloudSettlementArchive(ctx context.Context, root, reason string, payload []byte) error {
	if r == nil || r.db == nil || root == "" {
		return errors.New("Cloud settlement archive is unavailable")
	}
	switch reason {
	case "unknown_usage", "invalid_record", "invalid_settlement", "owner_missing", "owner_invalid", "billing_conflict":
	default:
		return errors.New("invalid Cloud settlement archive reason")
	}
	if payload == nil {
		payload = []byte{}
	}
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	// Check byte equality even on the idempotent path. Neither a fingerprint
	// collision nor accidental external corruption may authorize Redis removal.
	var stored []byte
	err := r.db.QueryRowContext(ctx, `INSERT INTO openai_cloud_settlement_archive(root_id,payload_sha256,reason,payload)
 VALUES($1,$2,$3,$4) ON CONFLICT(root_id,payload_sha256)
 DO UPDATE SET archived_at=openai_cloud_settlement_archive.archived_at RETURNING payload`, root, hash, reason, payload).Scan(&stored)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, payload) {
		return errors.New("Cloud archived settlement payload conflict")
	}
	return nil
}

func (r *usageBillingRepository) HasCloudSettlementArchive(ctx context.Context, root string) (bool, error) {
	if r == nil || r.db == nil || root == "" {
		return false, errors.New("Cloud settlement archive is unavailable")
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM openai_cloud_settlement_archive WHERE root_id=$1)`, root).Scan(&exists)
	return exists, err
}
