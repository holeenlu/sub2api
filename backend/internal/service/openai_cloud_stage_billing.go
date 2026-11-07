package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

// Version 4 changes the debit identity from a turn to its confirmed response
// stages. Older readers must never price the turn aggregate as another bill.
const cloudStageBillingVersion = 4

type OpenAICloudBillingStage struct {
	ResponseID      string                `json:"response_id"`
	ReservationID   string                `json:"reservation_id,omitempty"`
	BillingSnapshot json.RawMessage       `json:"billing_snapshot"`
	SampleFrom      int                   `json:"sample_from"`
	SampleTo        int                   `json:"sample_to"`
	BaseUsage       *OpenAICloudUsage     `json:"base_usage"`
	Usage           *OpenAICloudUsage     `json:"usage"`
	StartedAt       time.Time             `json:"started_at"`
	ClosedAt        time.Time             `json:"closed_at"`
	Status          string                `json:"status"`
	ErrorCode       string                `json:"error_code,omitempty"`
	Telemetry       *OpenAICloudTelemetry `json:"telemetry,omitempty"`
}

func CloudUsesStageBilling(raw json.RawMessage) bool {
	var header struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(raw, &header) == nil && header.Version == cloudStageBillingVersion
}

func (a *cloudSessionActor) beginBillingStage(command OpenAICloudCommand, p *cloudProtocolRequest) error {
	if a.record.Probe || !CloudUsesStageBilling(a.record.BillingSnapshot) {
		return nil
	}
	var snapshot OpenAICloudBillingSnapshot
	raw := command.Request.BillingSnapshot
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.UserID != a.record.UserID || snapshot.APIKeyID != a.record.APIKeyID || snapshot.AccountID != a.record.AccountID || valueOrZero(snapshot.GroupID) != a.record.GroupID {
		return errors.New("Cloud response billing snapshot ownership mismatch")
	}
	if _, ok := snapshot.Prices[a.record.Model]; !ok {
		return errors.New("Cloud response pricing is missing")
	}
	snapshot.Version = cloudStageBillingVersion
	snapshot.UpstreamModel = a.record.Model
	snapshot.ReasoningEffort = p.effort
	raw, err := json.Marshal(&snapshot)
	if err != nil {
		return err
	}
	var base *OpenAICloudUsage
	from := 0
	for _, previous := range a.record.BillingStages {
		base = cloudStageUsageSum(base, previous.Usage)
		from = previous.SampleTo
	}
	a.record.OpenBillingStage = &OpenAICloudBillingStage{ResponseID: a.responseID, ReservationID: command.Request.BillingReservationID, BillingSnapshot: raw, SampleFrom: from, BaseUsage: base, StartedAt: time.Now()}
	if command.Request.Telemetry != nil {
		copy := *command.Request.Telemetry
		a.record.OpenBillingStage.Telemetry = &copy
		a.record.OpenBillingStage.StartedAt = copy.StartedAt
	}
	return nil
}

func cloudStageUsageDifference(total, base *OpenAICloudUsage) (*OpenAICloudUsage, error) {
	if total == nil {
		return nil, nil
	}
	delta, err := cloudUsageDifference(total, base)
	if err != nil {
		return nil, err
	}
	delta = cloudCloneLateUsage(delta)
	if base == nil {
		return delta, nil
	}
	delta.WebSearchCalls = total.WebSearchCalls - base.WebSearchCalls
	delta.ImageCount = total.ImageCount - base.ImageCount
	if delta.WebSearchCalls < 0 || delta.ImageCount < 0 || len(total.ImageOutputSizes) < len(base.ImageOutputSizes) || !reflect.DeepEqual(base.ImageOutputSizes, total.ImageOutputSizes[:len(base.ImageOutputSizes)]) && len(base.ImageOutputSizes) > 0 {
		return nil, errors.New("Cloud hosted usage boundary changed")
	}
	delta.ImageSizes = make(map[string]int)
	for tier, count := range total.ImageSizes {
		n := count - base.ImageSizes[tier]
		if n < 0 {
			return nil, errors.New("Cloud image count moved backwards")
		}
		if n > 0 {
			delta.ImageSizes[tier] = n
		}
	}
	for tier, count := range base.ImageSizes {
		if total.ImageSizes[tier] < count {
			return nil, errors.New("Cloud image count moved backwards")
		}
	}
	delta.ImageOutputSizes = append([]string(nil), total.ImageOutputSizes[len(base.ImageOutputSizes):]...)
	return delta, nil
}

// Publish only the confirmed usage available when a client response closes.
// Tool-wait notifications remain on the root for the next response boundary;
// an already published response bill is immutable.
func (a *cloudSessionActor) closeBillingStage(status string) error {
	stage := a.record.OpenBillingStage
	if stage == nil || !CloudUsesStageBilling(a.record.BillingSnapshot) {
		return nil
	}
	if stage.ClosedAt.IsZero() {
		stage.ClosedAt = time.Now()
		stage.Status = status
		if status != "completed" {
			stage.ErrorCode, _ = a.publicFailure["code"].(string)
		}
	}
	if stage.Usage != nil {
		return nil
	}
	if a.usageTainted || (a.uncertain && status != "completed") {
		return nil
	}
	delta, err := cloudStageUsageDifference(a.record.Usage, stage.BaseUsage)
	if err != nil {
		return err
	}
	if delta == nil {
		return nil
	}
	end := len(a.record.SamplingUsage)
	if stage.SampleFrom > end || !cloudSamplingUsageValid(delta, a.record.SamplingUsage[stage.SampleFrom:end]) {
		return errors.New("Cloud response sampling boundary is inconsistent")
	}
	stage.Usage = delta
	stage.SampleTo = end
	copy := *stage
	a.record.BillingStages = append(a.record.BillingStages, copy)
	return nil
}

// Validate the complete envelope before issuing any debit. Persisted stages
// contain no client tool text: only scope, pricing, time and measured counters.
func cloudValidateBillingStages(root *OpenAICloudSettlement) error {
	seen := map[string]bool{}
	end := 0
	var cumulative *OpenAICloudUsage
	for _, stage := range root.BillingStages {
		if (stage.Status != "completed" && stage.Status != "failed" && stage.Status != "interrupted") || (stage.ReservationID != "" && (!strings.HasPrefix(stage.ReservationID, "cloud_hold_") || len(stage.ReservationID) > 160)) || !strings.HasPrefix(stage.ResponseID, "resp_cloud_") || len(stage.ResponseID) > 100 || seen[stage.ResponseID] || stage.SampleFrom != end || stage.SampleTo < end || stage.SampleTo > len(root.SamplingUsage) || stage.ClosedAt.IsZero() || stage.StartedAt.IsZero() || stage.ClosedAt.Before(stage.StartedAt) {
			return cloudPermanentSettlementError("invalid_settlement")
		}
		seen[stage.ResponseID] = true
		if !cloudRecoveryUsageEqual(stage.BaseUsage, cumulative) && !(cumulative == nil && stage.BaseUsage != nil && stage.BaseUsage.TotalTokens == 0 && stage.BaseUsage.ImageCount == 0 && stage.BaseUsage.WebSearchCalls == 0) {
			return cloudPermanentSettlementError("invalid_settlement")
		}
		if !cloudBillingUsageValid(stage.Usage) || !cloudSamplingUsageValid(stage.Usage, root.SamplingUsage[stage.SampleFrom:stage.SampleTo]) {
			return cloudPermanentSettlementError("invalid_sampling_usage")
		}
		var snapshot OpenAICloudBillingSnapshot
		if json.Unmarshal(stage.BillingSnapshot, &snapshot) != nil || snapshot.Version != cloudStageBillingVersion || snapshot.UserID != root.UserID || snapshot.APIKeyID != root.APIKeyID || snapshot.AccountID != root.AccountID || valueOrZero(snapshot.GroupID) != root.GroupID || snapshot.UpstreamModel != root.Model {
			return cloudPermanentSettlementError("owner_invalid")
		}
		cumulative = cloudStageUsageSum(cumulative, stage.Usage)
		end = stage.SampleTo
	}
	if cumulative != nil {
		if _, err := cloudStageUsageDifference(root.Usage, cumulative); err != nil || root.Usage == nil {
			return cloudPermanentSettlementError("invalid_sampling_usage")
		}
	}
	return nil
}
func cloudStageUsageSum(a, b *OpenAICloudUsage) *OpenAICloudUsage {
	if a == nil {
		return cloudCloneLateUsage(b)
	}
	v := cloudSumTokenUsage(a, b)
	v.WebSearchCalls = a.WebSearchCalls + b.WebSearchCalls
	v.ImageCount = a.ImageCount + b.ImageCount
	v.ImageSizes = map[string]int{}
	for k, n := range a.ImageSizes {
		v.ImageSizes[k] = n
	}
	for k, n := range b.ImageSizes {
		v.ImageSizes[k] += n
	}
	v.ImageOutputSizes = append(append([]string(nil), a.ImageOutputSizes...), b.ImageOutputSizes...)
	return v
}

func (w *openAICloudBillingWorker) settleStages(ctx context.Context, root *OpenAICloudSettlement) error {
	if err := cloudValidateBillingStages(root); err != nil {
		return err
	}
	var cumulative *OpenAICloudUsage
	for _, stage := range root.BillingStages {
		child := *root
		child.RootRequestID = root.RootRequestID + "/" + stage.ResponseID
		child.BillingSnapshot = stage.BillingSnapshot
		child.Usage = stage.Usage
		child.SamplingUsage = root.SamplingUsage[stage.SampleFrom:stage.SampleTo]
		child.ObservedAt = stage.ClosedAt
		child.Status = stage.Status
		child.ErrorCode = stage.ErrorCode
		child.Unknown = false
		child.LowerBound = stage.Status != "completed"
		child.BillingStages = nil
		child.OpenBillingStage = nil
		child.ReconciliationOnly = false
		child.LateUsage = nil
		child.ArchivePayload = nil
		child.ArchiveReason = ""
		child.Telemetry = stage.Telemetry
		// A response delivering another parallel tool without new model/hosted
		// consumption is not an extra per-request inference charge.
		acknowledged := false
		if w.store != nil {
			var err error
			acknowledged, err = w.store.CloudSettlementAcknowledged(ctx, child.RootRequestID)
			if err != nil {
				return err
			}
		}
		if !acknowledged && (stage.Usage.TotalTokens > 0 || stage.Usage.WebSearchCalls > 0 || stage.Usage.ImageCount > 0) {
			if err := w.settleOne(ctx, &child); err != nil {
				return err
			}
		}
		child.RootRequestID = stage.ReservationID
		w.releaseReservation(ctx, &child)
		cumulative = cloudStageUsageSum(cumulative, stage.Usage)
	}
	if root.ReconciliationOnly || len(root.LateUsage) > 0 {
		return cloudPermanentSettlementError("billing_conflict")
	}
	if root.Unknown {
		return cloudPermanentSettlementError("unknown_usage")
	}
	if cloudTerminalStatus(root.Status) && !cloudRecoveryUsageEqual(cumulative, root.Usage) {
		if !(cumulative == nil && root.Usage != nil && root.Usage.TotalTokens == 0 && root.Usage.ImageCount == 0 && root.Usage.WebSearchCalls == 0) {
			return cloudPermanentSettlementError("invalid_sampling_usage")
		}
	}
	return nil
}
