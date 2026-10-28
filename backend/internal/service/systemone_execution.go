package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	protocol "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	provider "github.com/Wei-Shaw/sub2api/internal/providers/typesafe"
)

// SystemOneExecutionInput is trusted ingress input, not a JSON API object.
// Entity pointers remain at composition; the provider receives sealed values.
type SystemOneExecutionInput struct {
	OperationID                                                          string
	OriginalWireDigest                                                   [32]byte
	Body                                                                 []byte
	RequestedModel                                                       string
	APIKey                                                               *APIKey
	KeyService                                                           *APIKeyService
	Subscription                                                         *UserSubscription
	PricingAt                                                            time.Time
	SecurityClientIP, IPAddress, UserAgent, SessionID                    string
	InboundEndpoint, UpstreamEndpoint, QuotaPlatform, RequestPayloadHash string
	MaxAccountSwitches                                                   int
	ChannelUsageFields
}

// These ports preserve the existing queue and actual response writer. They do
// not perform account selection, forwarding, recovery or usage submission.
type SystemOneIngressPorts struct {
	AcquireAccount func(context.Context, *AccountSelectionResult) (func(), error)
	Deliver        func(context.Context, *provider.Response) (model.DeliveryReceipt, error)
	Selected       func(*Account)
}

type systemOneExecution struct {
	svc        *GatewayService
	in         SystemOneExecutionInput
	ports      SystemOneIngressPorts
	core       *execution.Operation
	recovery   *execution.Recovery
	dispatch   *systemOneDispatch
	pricing    *systemOnePricing
	journal    SystemOneSettlementJournal
	intent     *NativeBillingIntentService
	billingID  billing.Identity
	scopes     map[uint64]NativeUsageIntent
	references map[uint64]billing.NativeIntentReference
}

// ExecuteSystemOne owns scheduling, attempts and finalization for System One.
// A failed operation never hands control to a second execution path.
func (s *GatewayService) ExecuteSystemOne(ctx context.Context, in SystemOneExecutionInput, ports SystemOneIngressPorts) (model.OperationResult, error) {
	var empty model.OperationResult
	if s == nil || nativeDispatchNil(s.usageBillingRepo) {
		return empty, ErrSystemOneJournalUnavailable
	}
	journal, ok := s.usageBillingRepo.(SystemOneSettlementJournal)
	if !ok {
		return empty, ErrSystemOneJournalUnavailable
	}
	if ctx == nil || in.OperationID == "" || in.OriginalWireDigest == ([32]byte{}) || ports.AcquireAccount == nil || ports.Deliver == nil {
		return empty, execution.ErrOperationFacts
	}
	if _, err := protocol.ValidateSystemOneRequest(in.Body); err != nil {
		return empty, err
	}
	in.Body = append([]byte(nil), in.Body...)
	dispatch, err := s.newSystemOneDispatch(ctx, in)
	if err != nil {
		return empty, err
	}
	freshKey, freshSubscription, _, err := dispatch.loadPrincipal(ctx)
	if err != nil {
		return empty, err
	}
	// Queuing may outlive ingress auth/cache projections. The same freshly
	// validated principal supplies hydrated pricing/funding input; never freeze
	// the stale ingress role, group prices, or subscription after checking another.
	in.APIKey, in.Subscription = freshKey, freshSubscription
	pricing, err := s.captureSystemOnePricing(ctx, in)
	if err != nil {
		return empty, err
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return empty, execution.ErrDispatchUnavailable
	}
	budget.mu.Lock()
	control, attempts, output := budget.control, budget.attemptBudget, budget.firstOutput
	budget.mu.Unlock()
	d, err := execution.NewDispatcher(control, in.OperationID, attempts, output, dispatch)
	if err != nil {
		return empty, err
	}
	core, err := execution.NewOperation(d, model.OperationResult{OperationID: in.OperationID, UserID: in.APIKey.UserID, APIKeyID: in.APIKey.ID, GroupID: *in.APIKey.GroupID, GroupBound: true, Protocol: model.ProtocolSystemOne, OriginalWireDigest: in.OriginalWireDigest})
	if err != nil {
		return empty, err
	}
	intent, err := NewNativeBillingIntentService(journal)
	if err != nil {
		return empty, err
	}
	o := &systemOneExecution{svc: s, in: in, ports: ports, core: core, recovery: execution.NewRecovery(in.MaxAccountSwitches, false), dispatch: dispatch, pricing: pricing, journal: journal, intent: intent,
		billingID: billing.Identity{RequestID: nativeIntentCanonicalID(ctx, "", false), APIKeyID: in.APIKey.ID},
		scopes:    make(map[uint64]NativeUsageIntent), references: make(map[uint64]billing.NativeIntentReference)}
	defer PinTurnRetryAccount(ctx, nil)
	err = core.Execute(ctx, func() execution.CycleResult { return o.cycle(ctx) })
	terminal := core.Snapshot()
	allNotSent := len(terminal.Attempts) > 0
	for _, attempt := range terminal.Attempts {
		allNotSent = allNotSent && attempt.Send == model.SendNotSent
	}
	if allNotSent && len(o.references) > 0 {
		for _, ref := range o.references {
			cleanup, cancel := systemOneCleanupContext(ctx)
			receipt, closeErr := intent.CloseNotSent(cleanup, NativeIntentNotSentInput{Key: ref.Key, IntentFingerprint: ref.IntentFingerprint, Operation: terminal})
			cancel()
			if receipt.Durable() {
				core.Billed(model.BillingReceipt{Disposition: model.BillingNotRequired, Durable: true, EvidenceID: receipt.ClosureFingerprint})
			}
			err = errors.Join(err, closeErr)
			break
		}
	}
	return core.Snapshot(), err
}

func (o *systemOneExecution) cycle(ctx context.Context) execution.CycleResult {
	selection, err := o.svc.SelectAccountWithLoadAwareness(ctx, o.in.APIKey.GroupID, "", protocol.JevLatestModel, o.recovery.FailedAccountIDs, "", o.in.APIKey.UserID)
	if err != nil {
		return execution.CycleResult{Err: err}
	}
	if selection == nil || selection.Account == nil {
		if selection != nil && selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return execution.CycleResult{Err: ErrNoAvailableAccounts}
	}
	release, err := o.ports.AcquireAccount(ctx, selection)
	if err != nil {
		if release != nil {
			release()
		} else if selection.Acquired && selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return execution.CycleResult{Err: err}
	}
	if release != nil {
		defer release()
	}
	account, vetoed, _ := o.svc.GatewayProfitControlVetoLatest(ContextWithSelectionProfitGate(ctx, selection), selection.Account)
	if vetoed {
		if o.recovery.RecordProfitVeto(selection.Account.ID) == execution.RecoveryExhausted {
			return execution.CycleResult{Err: ErrNoAvailableAccounts}
		}
		return execution.CycleResult{Retry: true}
	}
	if account == nil {
		return execution.CycleResult{Err: ErrAccountNotFound}
	}
	identity := model.PreparedIdentity{UserID: o.in.APIKey.UserID, ResourceID: account.ID}
	bound, detach, err := o.dispatch.capacity.BindNativeDispatchLeases(ctx, identity)
	if err != nil {
		return execution.CycleResult{Err: err}
	}
	defer detach()
	prepared, account, err := o.dispatch.prepare(bound, o.in, account.ID)
	if err != nil {
		return execution.CycleResult{Err: err}
	}
	if o.ports.Selected != nil {
		o.ports.Selected(account)
	}
	attempt := &systemOnePrepared{attempt: prepared, owner: o, account: account}
	started := time.Now()
	retries := 0
	opened, sequence, openErr := o.core.OpenPrepared(bound, attempt, &retries, func(waitCtx context.Context) error { return WaitOpenAITurnRetry(waitCtx, 100*time.Millisecond) })
	if opened != nil {
		attempt = opened.(*systemOnePrepared)
	}
	if sequence == 0 {
		return execution.CycleResult{Err: errors.Join(openErr, attempt.Close())}
	}
	var response *provider.Response
	readErr := openErr
	if readErr == nil {
		response, readErr = attempt.attempt.Receive(bound)
	}
	if response != nil {
		o.core.RequestID(sequence, response.Headers.Get("x-request-id"))
		facts := response.Facts(readErr)
		// Authentication/rate-limit responses are explicit rejection evidence;
		// generic 5xx and transport ambiguity never receive this classification.
		if readErr == nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests) {
			facts.Generation = model.GenerationRejected
			facts.RejectionEvidenceID = fmt.Sprintf("systemone-http-%d", response.StatusCode)
		}
		readErr = errors.Join(readErr, o.core.Observe(sequence, facts))
	}
	closeErr := attempt.Close()
	o.core.Closed(sequence, closeErr)
	var failure error = errors.Join(readErr, closeErr)
	if failure == nil && response != nil && response.StatusCode >= 300 {
		failure = o.svc.systemOneHTTPFailure(ctx, account, response)
	}
	if failure != nil && response == nil && ctx.Err() == nil && !systemOneLocalFailure(failure) && classifyUpstreamTransportError(failure).Persistent {
		o.svc.tempUnscheduleTransportError(ctx, account, sanitizeUpstreamErrorMessage(failure.Error()))
	}
	var deliveryErr error
	if failure == nil && response != nil {
		receipt, err := o.ports.Deliver(ctx, response)
		o.core.Delivered(sequence, receipt)
		deliveryErr = err
	}
	cleanup, cancel := systemOneCleanupContext(ctx)
	finalErr := o.finalize(cleanup, sequence, account, response, time.Since(started))
	cancel()
	if finalErr != nil || deliveryErr != nil {
		return execution.CycleResult{Err: errors.Join(failure, deliveryErr, finalErr)}
	}
	if failure == nil {
		return execution.CycleResult{}
	}
	var failover *UpstreamFailoverError
	if !errors.As(failure, &failover) {
		send := model.SendUnknown
		for _, a := range o.core.CycleResult().Attempts {
			if a.Sequence == sequence {
				send = a.Send
			}
		}
		failover = systemOneTransportFailure(ctx, failure, send)
	}
	if failover == nil {
		return execution.CycleResult{Err: failure}
	}
	rejected := response != nil && readErr == nil && (response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 429)
	if !failover.RequestNotSent && !rejected {
		return execution.CycleResult{Err: failure}
	}
	port := &systemOneRecoveryPorts{owner: o, account: account, failure: failover}
	recovered := o.recovery.RecoverFailure(ctx, account.ID, &execution.RecoveryFailure{StatusCode: failover.StatusCode, RequestNotSent: failover.RequestNotSent, RejectedBeforeGeneration: rejected,
		RetryNextAccount: failover.ShouldRetryNextAccount(), RetryableOnSameAccount: failover.RetryableOnSameAccount, SameAccountRetryMax: failover.SameAccountRetryMax, CredentialFailure: failover.IsCredentialFailure()}, account.GetPoolModeRetryCount(), 0, execution.RecoveryPorts{Budget: port, Accounts: port})
	if recovered.Action == execution.RecoveryContinue {
		if recovered.Reason == "same_account_retry" {
			PinTurnRetryAccount(ctx, account)
		} else {
			PinTurnRetryAccount(ctx, nil)
		}
		return execution.CycleResult{Retry: true}
	}
	return execution.CycleResult{Err: errors.Join(failure, recovered.Err)}
}

// The root's before-send journal boundary runs after actual attempt numbering
// and before the adapter consumes its permit. No pricing/billing is in provider.
type systemOnePrepared struct {
	attempt  *provider.Attempt
	owner    *systemOneExecution
	account  *Account
	sequence uint64
}

func (p *systemOnePrepared) Identity() model.PreparedIdentity { return p.attempt.Identity() }
func (p *systemOnePrepared) Target() model.AttemptTarget      { return p.attempt.Target() }
func (p *systemOnePrepared) Close() error                     { return p.attempt.Close() }
func (p *systemOnePrepared) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	p.sequence = permit.AttemptSequence()
	if err := p.owner.preserve(ctx, p.account, p.Identity(), permit.AttemptSequence()); err != nil {
		return model.SendNotSent, execution.PreRequestFailure{}, errors.Join(ErrSystemOnePreparation, err)
	}
	return p.attempt.Open(ctx, permit)
}
func (p *systemOnePrepared) Reprepare(ctx context.Context) (execution.PreparedAttempt, error) {
	closeErr := p.attempt.Close()
	p.owner.core.Closed(p.sequence, closeErr)
	if closeErr != nil {
		return nil, closeErr
	}
	next, err := p.attempt.Reprepare(ctx)
	if err != nil {
		return nil, err
	}
	return &systemOnePrepared{attempt: next.(*provider.Attempt), owner: p.owner, account: p.account}, nil
}

func (o *systemOneExecution) preserve(ctx context.Context, account *Account, identity model.PreparedIdentity, sequence uint64) error {
	scope, err := o.pricing.capture(ctx, o.svc, o.in, account, identity, sequence)
	if err != nil {
		return err
	}
	o.scopes[sequence] = scope
	receipt, err := o.intent.PreserveBeforeSend(ctx, scope)
	if receipt.Durable() {
		o.references[sequence] = receipt.NativeIntentReference
		return err
	}
	// No send follows an uncertain commit. Confirm the original technical row
	// solely to allow bounded NotSent closure, never to invent a durable receipt.
	cleanup, cancel := systemOneCleanupContext(ctx)
	defer cancel()
	stored, loadErr := o.intent.Load(cleanup, scope.Key)
	if loadErr == nil && stored != nil {
		for _, a := range stored.Attempts {
			if a.Sequence == sequence {
				copy := NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: a}
				got, e := copy.NativeIntentReference()
				want, w := scope.NativeIntentReference()
				if e == nil && w == nil && got == want {
					o.references[sequence] = got
				}
			}
		}
	}
	return errors.Join(ErrUsageBillingUnavailable, err, loadErr)
}

func (o *systemOneExecution) finalize(ctx context.Context, sequence uint64, account *Account, response *provider.Response, duration time.Duration) error {
	ref, ok := o.references[sequence]
	if !ok {
		return ErrUsageBillingUnavailable
	}
	prep := SystemOneUsagePreparation{Version: 1, Intent: ref, Scope: o.scopes[sequence], Operation: o.core.Snapshot(), ObservedAt: time.Now(), Duration: duration, UserAgent: o.in.UserAgent, IPAddress: o.in.IPAddress, SessionID: o.in.SessionID, InboundEndpoint: o.in.InboundEndpoint, UpstreamEndpoint: o.in.UpstreamEndpoint, ChannelUsageFields: o.in.ChannelUsageFields}
	for _, a := range prep.Operation.Attempts {
		if a.Sequence == sequence && a.Send == model.SendNotSent {
			return nil
		}
	}
	// Keep the existing in-flight hold until the journal confirms the facts.
	// An unconfirmed write must retain this reference until the cache TTL,
	// just like the Native finalizer; no second reservation is created.
	var releaseReservation func()
	if reservation := InflightReservationFromContext(ctx); reservation != nil {
		releaseReservation = reservation.Acquire()
	}
	upstreamID := ""
	if response != nil {
		prep.StatusCode = response.StatusCode
		prep.InputTokens = copyPricingValue(response.Usage.InputTokens)
		prep.OutputTokens = copyPricingValue(response.Usage.OutputTokens)
		prep.ReturnedModel = response.Model
		upstreamID = response.Headers.Get("x-request-id")
		prep.UpstreamRequestID = usageUpstreamRequestIDPtr(account, response.Headers, false)
	}
	// A missing upstream identity is recoverable evidence, not permission to
	// mint a new money root. Keep only the original client/local identity when
	// one exists; otherwise the journal records pending usage without a
	// settlement command and recovery waits for an authoritative identity.
	if o.billingID.RequestID == "" && response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		o.billingID.RequestID = nativeIntentCanonicalID(ctx, upstreamID, false)
	}
	prep.Identity = o.billingID
	if err := ValidateSystemOneUsagePreparation(prep); err != nil {
		return err
	}
	priceErr := o.pricing.resolveFinalPrice(ctx, o.svc, &prep)
	if priceErr == nil && prep.InputTokens != nil && prep.OutputTokens != nil && prep.StatusCode >= 200 && prep.StatusCode < 300 {
		prep.Settlement, prep.SettlementUsage, priceErr = o.svc.PrepareSystemOneUsage(ctx, prep)
	}
	// Missing preparation remains durable PendingPricing, and unknown quantities
	// remain PendingUsage. Neither path submits a zero monetary command.
	receipt, err := o.journal.RecordSystemOneUsage(ctx, &prep)
	if receipt.Intent != ref || !receipt.Billing.Durable {
		return errors.Join(ErrUsageBillingUnavailable, err, priceErr)
	}
	if releaseReservation != nil {
		releaseReservation()
	}
	// The repository owns the atomic money and usage-log transaction. The
	// service still owns the post-commit effects (quota cache invalidation,
	// deferred last-used updates and notifications), exactly as the Native and
	// BPS finalizers do. Keeping this here prevents System One from silently
	// acknowledging a charge without running the shared effects boundary.
	if receipt.ActualApplyResult != nil && prep.Settlement != nil && prep.Settlement.UsageLog != nil {
		cmd := prep.Settlement
		user := o.in.APIKey.User
		if user == nil {
			user = &User{ID: o.in.APIKey.UserID}
		}
		cost := &CostBreakdown{TotalCost: cmd.UsageLog.TotalCost, ActualCost: cmd.UsageLog.ActualCost}
		params := &postUsageBillingParams{
			Cost: cost, User: user, APIKey: o.in.APIKey, Account: account,
			Subscription: o.in.Subscription, APIKeyService: o.in.KeyService,
			RequestPayloadHash:    prep.Scope.Request.Billing.RequestPayloadHash,
			IsSubscriptionBill:    prep.Scope.Request.Billing.IsSubscriptionBill,
			AccountRateMultiplier: prep.Scope.Attempt.FrozenPricing.Policy.AccountRateMultiplier,
			Platform:              prep.Scope.Request.Billing.QuotaPlatform,
			UsageLogOnly:          prep.Scope.Request.Mode == UsageBillingModeLogOnly,
		}
		_, effectsErr := finishUsageBillingEffects(ctx, params, o.svc.billingDeps(), receipt.ActualApplyResult)
		err = errors.Join(err, effectsErr)
	}
	o.core.Billed(receipt.Billing)
	return errors.Join(err, priceErr)
}
