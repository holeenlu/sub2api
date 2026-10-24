package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// An expired owner lost its socket and pending server RPCs. Recovery publishes
// accounting facts under a new fenced lease; it never reconnects or replays.
func (s *OpenAIGatewayService) recoverCloudSessions(ctx context.Context, store OpenAICloudSessionStore, workerID string) error {
	if s == nil || store == nil || strings.TrimSpace(workerID) == "" {
		return errors.New("Cloud recovery dependencies are unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	expired, err := store.ListExpiredCloudSessions(ctx, OpenAICloudMaxSessions)
	if err != nil || len(expired) == 0 {
		return err
	}
	var failures []error
	for _, candidate := range expired {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		if candidate == nil || candidate.SessionID == "" || candidate.Status == "closed" {
			continue
		}
		// Every claim needs a fresh token even when this worker reclaimed the
		// same session previously; a late earlier attempt cannot become owner.
		owner := workerID + ":recovery:" + uuid.NewString()
		record, claimed, err := store.ClaimCloudSession(ctx, candidate.SessionID, owner, OpenAICloudOwnerLease)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !claimed {
			continue
		}
		if record == nil || record.SessionID != candidate.SessionID || record.OwnerToken != owner || record.RootRequestID == "" {
			failures = append(failures, errors.New("Cloud recovery claim did not establish ownership"))
			continue
		}
		if err := s.recoverCloudSession(ctx, store, record); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *OpenAIGatewayService) recoverCloudSession(ctx context.Context, store OpenAICloudSessionStore, record *OpenAICloudSessionRecord) error {
	if record.Status == "closed" {
		return nil
	}
	if record.Status == "completed" {
		acknowledged, err := store.CloudSettlementAcknowledged(ctx, record.RootRequestID)
		if err != nil {
			return err
		}
		if acknowledged {
			return store.ReleaseCloudSession(ctx, record.SessionID, record.OwnerToken)
		}
		pending, err := store.GetCloudSettlement(ctx, record.RootRequestID)
		if err != nil {
			return err
		}
		if pending != nil {
			// Save published the terminal record and this bill atomically. A
			// consumer may currently own the bill, so preserve it byte-for-byte.
			if pending.Unknown || pending.Status != "completed" || pending.RootRequestID != record.RootRequestID || pending.SessionID != record.SessionID ||
				pending.OpenAICloudOwner != record.OpenAICloudOwner || !cloudBillingUsageValid(pending.Usage) ||
				!cloudRecoveryUsageEqual(pending.Usage, record.Usage) || !cloudRecoveryJSONEqual(pending.BillingSnapshot, record.BillingSnapshot) {
				return errors.New("Cloud completed session and its settlement disagree")
			}
			return store.ReleaseCloudSession(ctx, record.SessionID, record.OwnerToken)
		}
		if observedAt, valid := cloudRecoveryCompletedProof(record); valid {
			record.Sequence++
			settlement := &OpenAICloudSettlement{OpenAICloudBinding: record.OpenAICloudBinding, BillingSnapshot: record.BillingSnapshot,
				Status: "completed", Usage: record.Usage, Sequence: record.Sequence, ObservedAt: observedAt}
			if err := store.SaveCloudSession(ctx, record, nil, settlement); err != nil {
				return err
			}
			return store.ReleaseCloudSession(ctx, record.SessionID, record.OwnerToken)
		}
	}
	// The last usage watermark is retained but cannot prove final consumption.
	// In particular, a created record is not proof turn/start was never sent.
	record.Status = "unknown"
	record.Failure = "Cloud owner expired before final consumption was confirmed; execution was not replayed"
	record.Result = nil
	record.Sequence++
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: record.OpenAICloudBinding, BillingSnapshot: record.BillingSnapshot,
		Status: "unknown", Usage: record.Usage, Sequence: record.Sequence, ObservedAt: time.Now().UTC(), Failure: record.Failure, Unknown: true}
	if err := store.SaveCloudSession(ctx, record, nil, settlement); err != nil {
		return err
	}
	return nil
}

func cloudRecoveryCompletedProof(record *OpenAICloudSessionRecord) (time.Time, bool) {
	result := record.Result
	if result == nil || !result.Terminal || result.AwaitingTools || !result.UsageCredible || !cloudBillingUsageValid(record.Usage) ||
		result.SessionID != record.SessionID || result.RootRequestID != record.RootRequestID ||
		result.ThreadID != record.ThreadID || result.TurnID != record.TurnID || !cloudRecoveryUsageEqual(result.Usage, record.Usage) {
		return time.Time{}, false
	}
	var response struct {
		Status    string          `json:"status"`
		CreatedAt int64           `json:"created_at"`
		Usage     json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(result.Response, &response) != nil || response.Status != "completed" || response.CreatedAt <= 0 || !cloudRecoveryPublicUsageMatches(response.Usage, record.Usage) {
		return time.Time{}, false
	}
	observedAt := time.Unix(response.CreatedAt, 0).UTC()
	return observedAt, !observedAt.After(time.Now().Add(time.Minute))
}

func cloudRecoveryUsageEqual(a, b *OpenAICloudUsage) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens && a.CachedInputTokens == b.CachedInputTokens && a.CacheWriteInputTokens == b.CacheWriteInputTokens && a.TotalTokens == b.TotalTokens &&
		a.WebSearchCalls == b.WebSearchCalls && a.ImageCount == b.ImageCount && maps.Equal(a.ImageSizes, b.ImageSizes) && slices.Equal(a.ImageOutputSizes, b.ImageOutputSizes)
}

// The public terminal response carries token totals. Hosted usage belongs to
// the whole Cloud turn and may include items emitted by earlier HTTP responses;
// compare those facts in the typed Result.Usage instead of recounting output.
func cloudRecoveryPublicUsageMatches(raw json.RawMessage, expected *OpenAICloudUsage) bool {
	var usage struct {
		Input  json.RawMessage `json:"input_tokens"`
		Output json.RawMessage `json:"output_tokens"`
		Total  json.RawMessage `json:"total_tokens"`
		Detail struct {
			Cached json.RawMessage `json:"cached_tokens"`
			Write  json.RawMessage `json:"cache_creation_tokens"`
		} `json:"input_tokens_details"`
	}
	if expected == nil || json.Unmarshal(raw, &usage) != nil {
		return false
	}
	encoded, err := json.Marshal(map[string]json.RawMessage{"inputTokens": usage.Input, "outputTokens": usage.Output, "totalTokens": usage.Total,
		"cachedInputTokens": usage.Detail.Cached, "cacheWriteInputTokens": usage.Detail.Write})
	if err != nil {
		return false
	}
	observed, err := cloudParseUsage(encoded)
	return err == nil && observed.InputTokens == expected.InputTokens && observed.OutputTokens == expected.OutputTokens && observed.TotalTokens == expected.TotalTokens &&
		observed.CachedInputTokens == expected.CachedInputTokens && observed.CacheWriteInputTokens == expected.CacheWriteInputTokens
}

func cloudRecoveryJSONEqual(a, b json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
		return true
	}
	// Redis's JSON codec may reorder keys; ordering is not a billing fact.
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
