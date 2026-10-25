package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Shared storage contains only scoped digests/sketches, never prompt text or
// authentication data. Redis persistence is managed by the existing deployment.
type ContentModerationGuardRecord struct {
	Kind       string   `json:"kind"`
	Key        string   `json:"key"`
	Scope      [32]byte `json:"scope"`
	ImagesHash string   `json:"images_hash,omitempty"`
	Sketch     []uint64 `json:"sketch,omitempty"`
	Runes      int      `json:"runes,omitempty"`
	ExpiresMS  int64    `json:"expires_ms"`
	Generation string   `json:"generation,omitempty"`
}

func moderationGuardGeneration(revision string) string {
	_, generation, found := strings.Cut(revision, ":")
	if !found || generation == "" {
		return "0"
	}
	return generation
}

type ContentModerationGuardStore interface {
	LoadGuardState(context.Context, string) (string, []ContentModerationGuardRecord, bool, error)
	StoreGuardEntries(context.Context, []ContentModerationGuardRecord, int, int) error
	ClearGuardEntries(context.Context) (int64, error)
}

func (s *ContentModerationService) syncGuardMemory(ctx context.Context) error {
	s.guardStoreMu.Lock()
	defer s.guardStoreMu.Unlock()
	store, ok := s.hashCache.(ContentModerationGuardStore)
	if !ok {
		return nil
	}
	s.guardMu.Lock()
	revision := s.guardRevision
	s.guardMu.Unlock()
	readCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	next, records, changed, err := store.LoadGuardState(readCtx, revision)
	if err != nil {
		return fmt.Errorf("shared moderation memory unavailable: %w", err)
	}
	if !changed {
		return nil
	}
	memory := contentModerationGuardMemory{refusals: make(map[[32]byte]contentModerationRefusalEntry), devices: make(map[[32]byte]time.Time)}
	now := time.Now()
	for _, record := range records {
		raw, err := hex.DecodeString(record.Key)
		if err != nil || len(raw) != 32 {
			continue
		}
		var key [32]byte
		copy(key[:], raw)
		expires := time.UnixMilli(record.ExpiresMS)
		if !expires.After(now) {
			continue
		}
		switch record.Kind {
		case "refusal":
			memory.refusals[key] = contentModerationRefusalEntry{scope: record.Scope, imagesHash: record.ImagesHash, sketch: record.Sketch, runes: record.Runes, expires: expires}
		case "device":
			memory.devices[key] = expires
		}
	}
	s.guardMu.Lock()
	// A local observation not yet flushed must not disappear during a concurrent read.
	pending := s.guardPending[:0]
	for _, record := range s.guardPending {
		if record.ExpiresMS > now.UnixMilli() && record.Generation == moderationGuardGeneration(next) {
			applyPendingGuard(&memory, record)
			pending = append(pending, record)
		}
	}
	s.guardPending = pending
	s.guardMemory = memory
	s.guardRevision = next
	s.guardMu.Unlock()
	return nil
}

func applyPendingGuard(memory *contentModerationGuardMemory, record ContentModerationGuardRecord) {
	raw, _ := hex.DecodeString(record.Key)
	var key [32]byte
	copy(key[:], raw)
	expires := time.UnixMilli(record.ExpiresMS)
	if record.Kind == "device" {
		memory.devices[key] = expires
	} else {
		memory.refusals[key] = contentModerationRefusalEntry{scope: record.Scope, imagesHash: record.ImagesHash, sketch: record.Sketch, runes: record.Runes, expires: expires}
	}
}

func (s *ContentModerationService) flushGuardMemory(ctx context.Context, cfg *ContentModerationConfig) error {
	s.guardStoreMu.Lock()
	defer s.guardStoreMu.Unlock()
	s.guardMu.Lock()
	s.pruneGuardMemoryLocked(time.Now(), cfg)
	records := append([]ContentModerationGuardRecord(nil), s.guardPending...)
	s.guardMu.Unlock()
	store, ok := s.hashCache.(ContentModerationGuardStore)
	if len(records) == 0 {
		return nil
	}
	if ok {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 150*time.Millisecond)
		defer cancel()
		if err := store.StoreGuardEntries(writeCtx, records, cfg.RefusalGuard.MaxEntries, cfg.DeviceGuard.MaxEntries); err != nil {
			return err
		}
	}
	written := make(map[string]int64, len(records))
	for _, record := range records {
		written[record.Kind+record.Key] = record.ExpiresMS
	}
	s.guardMu.Lock()
	pending := s.guardPending[:0]
	for _, record := range s.guardPending {
		if written[record.Kind+record.Key] != record.ExpiresMS {
			pending = append(pending, record)
		}
	}
	s.guardPending = pending
	s.guardMu.Unlock()
	return nil
}
