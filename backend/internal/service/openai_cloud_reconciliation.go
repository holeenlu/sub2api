package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type OpenAICloudReconciliationInput struct {
	RootID    string          `json:"root_id"`
	Sequence  uint64          `json:"sequence"`
	Status    string          `json:"status"`
	Usage     json.RawMessage `json:"usage"`
	Evidence  string          `json:"evidence"`
	Reason    string          `json:"reason"`
	Confirmed bool            `json:"confirmed"`
}

type OpenAICloudUnsettled struct {
	RootID        string            `json:"root_id"`
	Sequence      uint64            `json:"sequence"`
	ThreadID      string            `json:"thread_id"`
	TurnID        string            `json:"turn_id"`
	Model         string            `json:"model"`
	ObservedUsage *OpenAICloudUsage `json:"observed_usage"`
}

type cloudReconciliationRepository interface {
	RecordCloudReconciliation(context.Context, int64, *OpenAICloudSettlement, string, string) error
}
type cloudRuntimeReconciliationRepository interface {
	RecordCloudRuntimeBlock(context.Context, string, int64) error
	PendingCloudReconciliationAcks(context.Context, int64) ([]string, error)
	MarkCloudReconciliationSettled(context.Context, string) error
	CloudRuntimeReconciled(context.Context, int64) (bool, error)
}
type cloudReconciliationStore interface {
	ApplyCloudReconciliation(context.Context, *OpenAICloudSettlement, *OpenAICloudSettlement) error
}

func (s *OpenAICloudService) reconciliationActor(ctx context.Context, accountID int64) (int64, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || actor.Role != authz.SuperAdmin || actor.UserID <= 0 {
		return 0, authz.ErrPermissionDenied
	}
	if err := authz.Revalidate(ctx); err != nil {
		return 0, err
	}
	if _, err := s.account(ctx, accountID); err != nil {
		return 0, err
	}
	if s.gateway == nil || s.gateway.cloudSessions == nil {
		return 0, errors.New("Cloud settlement service is unavailable")
	}
	return actor.UserID, nil
}

func (s *OpenAICloudService) Unsettled(ctx context.Context, accountID int64) ([]OpenAICloudUnsettled, error) {
	if _, err := s.reconciliationActor(ctx, accountID); err != nil {
		return nil, err
	}
	pending, err := s.gateway.cloudSessions.store.ListCloudSettlements(ctx, OpenAICloudMaxSessions*2)
	if err != nil {
		return nil, err
	}
	result := []OpenAICloudUnsettled{}
	for _, item := range pending {
		if item != nil && item.AccountID == accountID && !item.Probe && item.Unknown {
			result = append(result, OpenAICloudUnsettled{item.RootRequestID, item.Sequence, item.ThreadID, item.TurnID, item.Model, item.Usage})
		}
	}
	return result, nil
}

func cloudReconciliationUsage(raw json.RawMessage, observed *OpenAICloudUsage) (*OpenAICloudUsage, error) {
	if len(raw) > 32<<10 || !cloudUniqueJSON(raw) {
		return nil, errors.New("invalid final Cloud usage")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errors.New("invalid final Cloud usage")
	}
	for _, key := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "cache_write_input_tokens", "total_tokens", "web_search_calls", "image_count", "image_sizes", "image_output_sizes"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, errors.New("every final Cloud usage field must be explicit")
		}
	}
	var usage OpenAICloudUsage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&usage) != nil || !cloudBillingUsageValid(&usage) {
		return nil, errors.New("invalid final Cloud usage")
	}
	if observed != nil {
		if usage.InputTokens < observed.InputTokens || usage.OutputTokens < observed.OutputTokens ||
			usage.CachedInputTokens < observed.CachedInputTokens || usage.CacheWriteInputTokens < observed.CacheWriteInputTokens ||
			usage.WebSearchCalls < observed.WebSearchCalls || usage.ImageCount < observed.ImageCount {
			return nil, errors.New("final Cloud usage cannot be below already observed consumption")
		}
		for size, count := range observed.ImageSizes {
			if usage.ImageSizes[size] < count {
				return nil, errors.New("final Cloud images cannot omit observed images")
			}
		}
	}
	return &usage, nil
}

// A deliberate operator reconciliation supplies externally verified final
// facts. It never estimates usage, forgives a balance or bypasses the ledger.
func (s *OpenAICloudService) Reconcile(ctx context.Context, accountID int64, input OpenAICloudReconciliationInput) error {
	actor, err := s.reconciliationActor(ctx, accountID)
	if err != nil {
		return err
	}
	if !input.Confirmed || len(input.RootID) == 0 || len(input.RootID) > 160 ||
		len(strings.TrimSpace(input.Evidence)) < 5 || len(input.Evidence) > 2000 || len(strings.TrimSpace(input.Reason)) < 5 || len(input.Reason) > 2000 ||
		(input.Status != "completed" && input.Status != "failed" && input.Status != "interrupted") {
		return infraerrors.BadRequest("CLOUD_RECONCILIATION_INVALID", "Confirm final consumption and provide its evidence, reason and terminal status")
	}
	store := s.gateway.cloudSessions.store
	old, err := store.GetCloudSettlement(ctx, input.RootID)
	if err != nil {
		return err
	}
	if old == nil || old.AccountID != accountID || old.Probe || !old.Unknown || old.Sequence != input.Sequence {
		return infraerrors.Conflict("CLOUD_RECONCILIATION_CHANGED", "The unresolved settlement changed; refresh before reconciling")
	}
	usage, err := cloudReconciliationUsage(input.Usage, old.Usage)
	if err != nil {
		return infraerrors.BadRequest("CLOUD_RECONCILIATION_USAGE", err.Error())
	}
	repo, ok := s.repo.(cloudReconciliationRepository)
	if !ok {
		return errors.New("Cloud reconciliation audit is unavailable")
	}
	updates, ok := store.(cloudReconciliationStore)
	if !ok {
		return errors.New("Cloud reconciliation storage is unavailable")
	}
	next := *old
	next.Unknown, next.Status, next.Usage, next.Failure = false, input.Status, usage, ""
	next.Sequence++
	if err = repo.RecordCloudReconciliation(ctx, actor, &next, input.Evidence, input.Reason); err != nil {
		return err
	}
	if err = authz.Revalidate(ctx); err != nil {
		return err
	}
	return updates.ApplyCloudReconciliation(ctx, old, &next)
}

// Clearing the runtime block never automatically enables Cloud.
// Both Redis admission and the SQL root check prevent a new unknown bill from
// being cleared by this CAS.
func (s *OpenAICloudService) clearSettledRuntime(ctx context.Context, accountID int64) error {
	proof, ok := s.repo.(cloudRuntimeReconciliationRepository)
	if !ok {
		return nil
	}
	roots, err := proof.PendingCloudReconciliationAcks(ctx, accountID)
	if err != nil {
		return err
	}
	for _, root := range roots {
		acked, err := s.gateway.cloudSessions.store.CloudSettlementAcknowledged(ctx, root)
		if err != nil {
			return err
		}
		if acked {
			if err := proof.MarkCloudReconciliationSettled(ctx, root); err != nil {
				return err
			}
		}
	}
	complete, err := proof.CloudRuntimeReconciled(ctx, accountID)
	if err != nil || !complete {
		return err
	}
	blocked, err := s.gateway.cloudSessions.store.CloudAccountBlocked(ctx, accountID)
	if err != nil || blocked {
		return err
	}
	pending, err := s.gateway.cloudSessions.store.ListCloudSettlements(ctx, OpenAICloudMaxSessions*2)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if item != nil && item.AccountID == accountID {
			return nil
		}
	}
	a, err := s.account(ctx, accountID)
	if err != nil {
		return err
	}
	b, err := s.repo.GetCloudAuthorization(ctx, accountID)
	if err != nil || b == nil || !b.RuntimeBlocked {
		return err
	}
	b.RuntimeBlocked = false
	ok, err = s.repo.StoreCloudAuthorization(ctx, a, b.Generation, b)
	if err != nil {
		return err
	}
	if !ok {
		return errOpenAICloudConflict
	}
	return nil
}

func (w *openAICloudBillingWorker) repairReconciledAccounts(ctx context.Context) {
	if w.service.cloudService == nil {
		return
	}
	repo, ok := w.service.cloudService.repo.(interface {
		ListCloudRuntimeBlockedAccounts(context.Context) ([]int64, error)
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ids, err := repo.ListCloudRuntimeBlockedAccounts(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		_ = w.service.cloudService.clearSettledRuntime(ctx, id)
	}
}

func (s *OpenAICloudService) blockUnsettledRuntime(ctx context.Context, bill *OpenAICloudSettlement, reason string) error {
	if repo, ok := s.repo.(cloudRuntimeReconciliationRepository); ok {
		if err := repo.RecordCloudRuntimeBlock(ctx, bill.RootRequestID, bill.AccountID); err != nil {
			return err
		}
	}
	return s.BlockRuntime(ctx, bill.AccountID, bill.GrantGeneration, reason)
}
