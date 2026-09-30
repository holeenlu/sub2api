package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

type gatewayMediaJobKey struct{}
type gatewayMediaPollKey struct{}

// SetGatewayMediaRepository also supports embedding the gateway without Wire.
func (s *OpenAIGatewayService) SetGatewayMediaRepository(repo GatewayMediaRepository) {
	s.mediaRepo = repo
}

func WithGatewayMediaJob(ctx context.Context, j *GatewayMediaJob) context.Context {
	return context.WithValue(ctx, gatewayMediaJobKey{}, j)
}
func gatewayMediaJobFromContext(ctx context.Context) *GatewayMediaJob {
	j, _ := ctx.Value(gatewayMediaJobKey{}).(*GatewayMediaJob)
	return j
}
func IsAsyncVideoCreate(e GrokMediaEndpoint) bool {
	return e == SeedanceEndpointCreate || e == GrokMediaEndpointVideosGenerations || e == GrokMediaEndpointVideosEdits || e == GrokMediaEndpointVideosExtensions
}

func (s *OpenAIGatewayService) PrepareGatewayVideoJob(ctx context.Context, key *APIKey, sub *UserSubscription, account *Account, endpoint GrokMediaEndpoint, info GrokMediaRequestInfo, original, inbound, platform string) (*GatewayMediaJob, error) {
	if s.mediaRepo == nil || key == nil || key.User == nil {
		return nil, fmt.Errorf("durable video settlement unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Persist only billing identity, not API-key tokens, passwords or voice/media
	// input. Group pricing is retained to make the job independent of key edits.
	keyCopy := *key
	keyCopy.Key = ""
	keyCopy.User = &User{ID: key.User.ID, Email: key.User.Email, Username: key.User.Username}
	now := time.Now().UTC()
	j := &GatewayMediaJob{ID: uuid.NewString(), State: "reserving", UserID: key.User.ID, GroupID: derefGroupID(key.GroupID), AccountID: account.ID, Endpoint: endpoint, Key: &keyCopy, Subscription: sub, QuotaPlatform: platform, InboundEndpoint: inbound, OriginalModel: original, CreatedAt: now, AccountRateMultiplier: account.BillingRateMultiplier()}
	if sub != nil {
		copySub := *sub
		copySub.User = nil
		copySub.Group = nil
		copySub.AssignedByUser = nil
		j.Subscription = &copySub
	}
	j.Pending = GrokVideoPendingBilling{Model: info.Model, BillingModel: info.Model, UpstreamModel: account.GetMappedModel(info.Model), VideoResolution: info.Resolution, VideoDurationSeconds: info.DurationSeconds, OriginalModel: original, CreatedAt: now.Format(time.RFC3339Nano)}
	base := 1.0
	if s.cfg != nil {
		base = s.cfg.Default.RateMultiplier
	}
	if key.Group != nil {
		base = s.ResolveUserGroupRateMultiplier(ctx, key.User.ID, j.GroupID, key.Group.RateMultiplier)
	}
	j.Multiplier = base
	mult, img := computePeakAwareMultipliers(key, base, now)
	video := resolveVideoRateMultiplier(key, base)
	sample := &OpenAIForwardResult{Model: info.Model, BillingModel: info.Model, UpstreamModel: j.Pending.UpstreamModel}
	tokens := UsageTokens{OutputTokens: 1}
	sample.Usage.OutputTokens = 1
	if endpoint.IsSeedance() {
		sample.Usage.OutputTokens = 1
		tokens.OutputTokens = 1
	} else {
		sample.VideoCount = 1
		sample.VideoDurationSeconds = 1
		sample.VideoResolution = info.Resolution
	}
	var unit *CostBreakdown
	var pricingUnit GatewayMediaPricingUnit
	var err error
	resolved := s.resolveOpenAIChannelPricing(ctx, info.Model, key)
	if !endpoint.IsSeedance() && (resolved == nil || resolved.Mode != BillingModeToken) {
		unit, pricingUnit = s.calculateOpenAIVideoCostWithUnit(ctx, info.Model, key, sample, video)
	} else {
		unit, err = s.calculateOpenAIRecordUsageCost(ctx, sample, key, usageBillingModelCandidates(info.Model, j.Pending.UpstreamModel), mult, img, video, base, tokens, "", openAILongContextBillingGate(account), now)
		// The token/Seedance path supplies one output token or one request. Even a
		// configured video label here has no duration-based UsageUnits input.
		pricingUnit = GatewayMediaPricingPerOutputToken
		if unit != nil && unit.BillingMode != "" && unit.BillingMode != string(BillingModeToken) {
			pricingUnit = GatewayMediaPricingPerRequest
		}
	}
	if err != nil {
		return nil, err
	}
	if unit == nil {
		return nil, fmt.Errorf("video pricing unavailable")
	}
	if unit.BillingMode == "" {
		unit.BillingMode = string(BillingModeToken)
	}
	j.UnitCost = unit
	j.PricingUnit = pricingUnit
	// Pending capacity, not a guessed token ceiling, bounds unbilled work for all
	// quota dimensions. The ordinary balance hold additionally reserves known
	// requested seconds for Grok, or the smallest billable token for Seedance.
	if (s.cfg == nil || s.cfg.RunMode != config.RunModeSimple) && (sub == nil || key.Group == nil || !key.Group.IsSubscriptionType()) {
		units := 1.0
		if pricingUnit == GatewayMediaPricingPerSecond {
			units = float64(NormalizeVideoBillingDurationSecondsOrDefault(info.DurationSeconds))
		}
		j.HoldAmount = QuantizeUsageBillingAmount(unit.ActualCost * units)
	}
	if err := s.mediaRepo.CreateJob(ctx, j); err != nil {
		return nil, err
	}
	if j.HoldAmount > 0 {
		if s.usageBillingRepo == nil {
			return nil, fmt.Errorf("durable billing unavailable")
		}
		if _, err := s.usageBillingRepo.ReserveBatchImageBalance(ctx, mediaBalanceHold(j, "reserve", 0)); err != nil {
			return nil, err
		}
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateUserBalance(ctx, j.UserID)
		}
	}
	j.State = "submitting"
	if err := s.mediaRepo.SaveJob(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (s *OpenAIGatewayService) mediaSubmissionStarting(ctx context.Context) {
	if j := gatewayMediaJobFromContext(ctx); j != nil {
		j.Submitted = true
	}
}
func (s *OpenAIGatewayService) mediaSubmissionRejected(ctx context.Context, status int) {
	// A transport error, timeout, redirect or server error does not prove that a
	// paid job was not created. Keep capacity occupied and never blind-resubmit.
	if status < 400 || status >= 500 || status == http.StatusRequestTimeout {
		return
	}
	if j := gatewayMediaJobFromContext(ctx); j != nil {
		_ = s.failGatewayMediaJob(ctx, j)
	}
}
func (s *OpenAIGatewayService) FinishGatewayVideoAttempt(ctx context.Context, j *GatewayMediaJob, err error) {
	if j != nil && err != nil && !j.Submitted {
		_ = s.failGatewayMediaJob(ctx, j)
	}
}
func (s *OpenAIGatewayService) saveMediaJobDetached(ctx context.Context, j *GatewayMediaJob) error {
	durable, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return s.mediaRepo.SaveJob(durable, j)
}
func (s *OpenAIGatewayService) acceptGatewayVideo(ctx context.Context, task string) error {
	j := gatewayMediaJobFromContext(ctx)
	if j == nil {
		return nil
	}
	if strings.TrimSpace(task) == "" {
		return fmt.Errorf("video create response missing task ID; submission requires reconciliation")
	}
	j.TaskID = task
	j.State = "pending"
	return s.saveMediaJobDetached(ctx, j)
}
func (s *OpenAIGatewayService) DurableGatewayVideo(ctx context.Context, group, user int64, task string) (*GatewayMediaJob, error) {
	if s.mediaRepo == nil {
		return nil, ErrMediaNotOwned
	}
	return s.mediaRepo.GetJob(ctx, group, user, task)
}

func (s *OpenAIGatewayService) StartGatewayMediaSettlement() {
	if s == nil || s.mediaRepo == nil || s.mediaSettlementCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mediaSettlementCancel = cancel
	s.mediaSettlementDone = make(chan struct{})
	go func() {
		defer close(s.mediaSettlementDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			s.reconcileGatewayMediaJobs(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *OpenAIGatewayService) StopGatewayMediaSettlement() {
	if s != nil && s.mediaSettlementCancel != nil {
		s.mediaSettlementCancel()
		<-s.mediaSettlementDone
	}
}
func (s *OpenAIGatewayService) reconcileGatewayMediaJobs(ctx context.Context) {
	// Claim a single job at a time: a bounded poll cannot outlive its lease while
	// earlier jobs in a batch occupy the worker. Other instances can claim peers.
	for n := 0; n < 32 && ctx.Err() == nil; n++ {
		jobs, err := s.mediaRepo.ClaimJobs(ctx, 1)
		if err != nil {
			logger.LegacyPrintf("gateway.media", "claim failed: %v", err)
			return
		}
		if len(jobs) == 0 {
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err = s.settleGatewayMediaJob(jobCtx, &jobs[0])
		cancel()
		if err != nil {
			logger.LegacyPrintf("gateway.media", "job %s remains pending: %v", jobs[0].ID, err)
		}
	}
}

func (s *OpenAIGatewayService) settleGatewayMediaJob(ctx context.Context, j *GatewayMediaJob) error {
	if j.State == "reserving" || j.State == "releasing" {
		return s.failGatewayMediaJob(ctx, j)
	}
	account, err := s.accountRepo.GetByID(ctx, j.AccountID)
	if err != nil {
		return err
	}
	if j.State == "pending" {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		ctx = context.WithValue(ctx, gatewayMediaPollKey{}, true)
		c.Request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "http://gateway.internal/video-settlement", nil)
		c.Set("api_key", j.Key)
		var result *OpenAIForwardResult
		if j.Endpoint.IsSeedance() {
			result, err = s.ForwardSeedance(ctx, c, account, SeedanceEndpointStatus, j.TaskID, nil)
		} else {
			result, err = s.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoStatus, j.TaskID, nil, "")
		}
		if err != nil {
			return err
		}
		status := gjson.GetBytes(w.Body.Bytes(), "status").String()
		if status == "failed" || status == "cancelled" || status == "canceled" {
			return s.failGatewayMediaJob(ctx, j)
		}
		if j.Endpoint.IsSeedance() {
			if status != "succeeded" {
				return nil
			}
			if result == nil || result.Usage.OutputTokens <= 0 {
				return fmt.Errorf("successful Seedance job missing completion tokens")
			}
		} else {
			if result == nil || result.VideoCount <= 0 {
				return nil
			}
		}
		result.RequestID = StableGrokVideoBillingRequestID(j.TaskID)
		result.ResponseID = j.TaskID
		result.Model = j.Pending.Model
		result.BillingModel = j.Pending.BillingModel
		result.UpstreamModel = j.Pending.UpstreamModel
		result.VideoResolution = NormalizeVideoBillingResolutionOrDefault(j.Pending.VideoResolution)
		if !j.Endpoint.IsSeedance() {
			if result.VideoDurationSeconds <= 0 {
				result.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(j.Pending.VideoDurationSeconds)
			}
			result.VideoCount = 1
			result.ImageCount = 0
		}
		result.Duration = time.Since(j.CreatedAt)
		j.Result = result
		if j.UnitCost == nil {
			return fmt.Errorf("missing durable pricing snapshot")
		}
		cost := *j.UnitCost
		units := 1.0
		switch j.PricingUnit {
		case GatewayMediaPricingPerRequest:
		case GatewayMediaPricingPerOutputToken:
			if result.Usage.OutputTokens <= 0 {
				return fmt.Errorf("token-priced completed video has no usage; retaining pending capacity")
			}
			units = float64(result.Usage.OutputTokens)
		case GatewayMediaPricingPerSecond:
			if result.VideoDurationSeconds <= 0 {
				return fmt.Errorf("second-priced completed video has no duration; retaining pending capacity")
			}
			units = float64(result.VideoDurationSeconds)
		default:
			// A display mode cannot reconstruct a missing historical price unit.
			return fmt.Errorf("missing or unsupported durable video pricing unit")
		}
		applyCostBreakdownMultiplier(&cost, units)
		j.Cost = &cost
		j.State = "billing"
		// Freeze the complete usage event before applying its durable dedup key.
		if err := s.mediaRepo.SaveJob(ctx, j); err != nil {
			return err
		}
	}
	if j.State != "billing" || j.Result == nil || j.Cost == nil {
		return fmt.Errorf("invalid media billing state")
	}
	captured := 0.0
	if j.HoldAmount > 0 {
		captured = math.Min(j.HoldAmount, j.Cost.ActualCost)
		if _, err := s.usageBillingRepo.CaptureBatchImageBalance(ctx, mediaBalanceHold(j, "capture", captured)); err != nil {
			return err
		}
	}
	billedAccount := *account
	billedAccount.RateMultiplier = &j.AccountRateMultiplier
	err = s.RecordUsage(ctx, &OpenAIRecordUsageInput{DeferredBalanceCaptured: captured, Result: j.Result, APIKey: j.Key, User: j.Key.User, Account: &billedAccount, Subscription: j.Subscription, QuotaPlatform: j.QuotaPlatform, InboundEndpoint: j.InboundEndpoint, RequestPayloadHash: HashUsageRequestPayload([]byte(j.TaskID)), APIKeyService: s.mediaAPIKeyService, PricingAt: j.CreatedAt, DeferredMediaCost: j.Cost, DeferredMediaMultiplier: &j.Multiplier, ChannelUsageFields: ChannelUsageFields{OriginalModel: j.OriginalModel, ChannelMappedModel: j.Pending.Model}})
	if err != nil {
		return err
	}
	if s.mediaAPIKeyService != nil {
		s.mediaAPIKeyService.InvalidateAuthCacheByUserID(ctx, j.UserID)
	}
	j.State = "settled"
	return s.mediaRepo.SaveJob(ctx, j)
}

func mediaBalanceHold(j *GatewayMediaJob, phase string, actual float64) *BatchImageBalanceHoldCommand {
	id := BatchImageHoldRequestID(j.ID)
	if phase != "reserve" {
		id = "gateway-media-" + phase + ":" + j.ID
	}
	return &BatchImageBalanceHoldCommand{RequestID: id, APIKeyID: j.Key.ID, UserID: j.UserID, BatchID: j.ID, HoldAmount: j.HoldAmount, ActualAmount: actual}
}
func (s *OpenAIGatewayService) failGatewayMediaJob(ctx context.Context, j *GatewayMediaJob) error {
	// Save release intent first, so restart retries the same idempotent primitive.
	j.State = "releasing"
	if err := s.saveMediaJobDetached(ctx, j); err != nil {
		return err
	}
	durable, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if j.HoldAmount > 0 {
		if s.usageBillingRepo == nil {
			return fmt.Errorf("billing unavailable")
		}
		if _, err := s.usageBillingRepo.ReleaseBatchImageBalance(durable, mediaBalanceHold(j, "release", 0)); err != nil {
			return err
		}
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateUserBalance(durable, j.UserID)
		}
	}
	j.State = "failed"
	return s.mediaRepo.SaveJob(durable, j)
}
