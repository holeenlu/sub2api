package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type responseBPSRoleKey struct{}

func withResponseBPSRole(ctx context.Context, role model.AttemptRole) context.Context {
	return context.WithValue(ctx, responseBPSRoleKey{}, role)
}

// The authenticated root owns this binding across selection/request contexts.
// It adds neither a budget nor a customer/capacity identity.
type responseBPSExecution struct {
	service    *OpenAIGatewayService
	owner      *nativeHTTPOperation
	controller *ResponseModeExecution
	mu         sync.Mutex // registrations only, never held during authority I/O
	admitMu    sync.Mutex // orders registered pre-send rows; released before native.Open performs I/O
	seals      map[model.PreparedIdentity]responseBPSSeal
	responses  map[*http.Response]*responseBPSObservation
}

type responseBPSSeal struct {
	account      *Account
	token, model string
	accountID    string
	endpoint     string
	grant        excelBPSDispatchGrant
	wirePolicy   [32]byte
}

func responseBPSRoleEndpoint(role model.AttemptRole, endpoint string) bool {
	if endpoint == basispoints.AttachmentsURL {
		return role == model.AttemptControl || role == model.AttemptPreparationRetry
	}
	return endpoint == basispoints.ResponsesURL && (role == model.AttemptGenerate || role == model.AttemptCompact || role == model.AttemptRepair)
}

func responseBPSFromContext(ctx context.Context) *responseBPSExecution {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil {
		return nil
	}
	owner.modeMu.Lock()
	defer owner.modeMu.Unlock()
	return owner.bps
}

func (s *OpenAIGatewayService) bindResponseBPS(ctx context.Context) (*responseBPSExecution, error) {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil || owner.dispatch == nil || owner.dispatch.authority == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	owner.modeMu.Lock()
	defer owner.modeMu.Unlock()
	if owner.bps != nil {
		if owner.bps.service != s {
			return nil, execution.ErrDispatchChanged
		}
		return owner.bps, nil
	}
	if authority, ok := s.accountRepo.(AccountExcelBPSDispatchRepository); !ok || nativeDispatchNil(authority) {
		return nil, execution.ErrDispatchUnavailable
	}
	b := &responseBPSExecution{service: s, owner: owner, seals: make(map[model.PreparedIdentity]responseBPSSeal), responses: make(map[*http.Response]*responseBPSObservation)}
	_, controller, err := BindResponseModeExecution(ctx, VendorOpenAIModeBPS, b)
	if err != nil {
		return nil, err
	}
	b.controller, owner.bps = controller, b
	owner.delegated = false
	return b, nil
}

// RevalidateDispatch is required by Admit, Consume and the connection handoff.
// It reuses the real root's key and acquired-capacity authorities, but never
// calls Native-only grant/mode validation to authorize an Excel request.
func (b *responseBPSExecution) RevalidateDispatch(ctx context.Context, identity model.PreparedIdentity) (execution.DispatchValidation, error) {
	var denied execution.DispatchValidation
	b.mu.Lock()
	seal, registered := b.seals[identity]
	b.mu.Unlock()
	a := b.owner.dispatch.authority
	r := a.request
	if !registered || !a.ready() || !identity.Valid() || identity.Mode != VendorOpenAIModeBPS || identity.Transport != "http" || !responseBPSRoleEndpoint(identity.Role, seal.endpoint) ||
		identity.OperationID != r.OperationID || identity.APIKeyID != r.APIKeyID || identity.UserID != r.UserID || identity.GroupID != r.GroupID || identity.GroupBound != r.GroupBound ||
		identity.Binding != (model.ModeBinding{BillingScope: r.OperationID}) {
		return denied, nativeDispatchError("BPS sealed registration", execution.ErrInvalidPreparedIdentity)
	}
	key, sub, deadline, err := a.loadKey(ctx)
	if err != nil {
		return denied, err
	}
	fresh, err := a.accounts.GetByID(ctx, identity.ResourceID)
	if err != nil {
		return denied, nativeDispatchError("BPS account", err)
	}
	if fresh == nil || fresh.ID != seal.account.ID || !fresh.IsSchedulable() || !fresh.IsExcelBPSEligible() || fresh.IsOpenAICloudConfigured() ||
		!fresh.isExcelBPSUpstreamModelEnabled(seal.model) || recoveryOwner(fresh) != identity.Owner || nativeDispatchWirePolicy(fresh) != seal.wirePolicy {
		return denied, nativeDispatchError("BPS route", ErrNativeDispatchScopeChanged)
	}
	if err := b.service.settingService.checkManagedAccountModels(ctx, fresh, r.RequestedModel, seal.model); err != nil {
		return denied, nativeDispatchError("BPS managed model policy", err)
	}
	if err := a.checks.ValidateVendorSupply(ctx, fresh, a.groupID()); err != nil {
		return denied, nativeDispatchError("BPS supply", err)
	}
	grant, valid := b.service.excelBPSGrantForToken(ctx, fresh, seal.token)
	if !valid || grant != seal.grant || excelBPSAccountID(fresh, seal.token) != seal.accountID {
		return denied, nativeDispatchError("BPS credential", ErrNativeDispatchGrantUnproven)
	}
	if fresh.IsExcelOAuth() {
		if expiry := fresh.GetOpenAITokenExpiresAt(); expiry != nil {
			deadline = nativeDispatchEarlier(deadline, *expiry)
		}
	} else {
		_, stored, err := b.service.openAITokenProvider.openAIOAuthService.readExcelGrant(ctx, fresh)
		if err != nil {
			return denied, nativeDispatchError("BPS grant lookup", err)
		}
		info, err := decodeStoredExcelOAuthTokenInfo(fresh, stored, false)
		if err != nil || stored[excelAccessTokenRejectedKey] == true {
			return denied, nativeDispatchError("BPS grant validity", errors.Join(err, ErrNativeDispatchGrantUnproven))
		}
		deadline = nativeDispatchEarlier(deadline, time.Unix(info.ExpiresAt, 0))
	}
	bound := context.WithValue(ctx, excelBPSDispatchGrantKey{}, grant)
	validator := b.service.accountRepo.(AccountExcelBPSDispatchRepository)
	if err := validator.ValidateExcelBPSDispatch(bound, seal.account, a.groupID(), seal.model); err != nil {
		return denied, nativeDispatchError("BPS dispatch", err)
	}
	if err := b.service.validateResponseBPSAvailability(bound, fresh, seal.model, seal.endpoint); err != nil {
		return denied, err
	}
	b.owner.dispatch.budget.mu.Lock()
	recovery := b.owner.dispatch.budget.recovery
	b.owner.dispatch.budget.mu.Unlock()
	if recovery != nil {
		if err := recovery.beforeSend(ctx, fresh); err != nil {
			return denied, err
		}
	}
	leases, err := a.leases.RevalidateNativeDispatchLeases(ctx, identity)
	if err != nil || leases.Fingerprint == ([32]byte{}) {
		return denied, nativeDispatchError("BPS capacity", errors.Join(err, ErrNativeDispatchLeaseUnproven))
	}
	deadline = nativeDispatchEarlier(deadline, leases.ValidUntil)
	if fresh.AutoPauseOnExpired && fresh.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *fresh.ExpiresAt)
	}
	if !deadline.IsZero() && !a.now().Before(deadline) {
		return denied, nativeDispatchError("BPS deadline", execution.ErrDispatchExpired)
	}
	fingerprint, err := a.authorizationFingerprint(key, sub, fresh, NativeDispatchCredential{Owner: identity.Owner, GrantDigest: identity.GrantDigest})
	if err != nil {
		return denied, err
	}
	return execution.DispatchValidation{Purpose: identity.Purpose, Authorization: fingerprint, Leases: leases.Fingerprint, ValidUntil: deadline}, nil
}

func (s *OpenAIGatewayService) validateResponseBPSAvailability(ctx context.Context, account *Account, upstreamModel, endpoint string) error {
	enabled, err := s.settingService.excelBPSEnabled(ctx)
	if err != nil {
		return newExcelBPSStateUnavailable(err, true)
	}
	if !enabled {
		return newExcelBPSRouteChangedError()
	}
	until, err := s.excelBPSModelCooldownDeadline(ctx, account.ID, upstreamModel)
	if err != nil {
		return newExcelBPSStateUnavailable(err, true)
	}
	if time.Now().Before(until) {
		failure := newExcelBPSModelPermissionFailoverError()
		failure.RequestNotSent = true
		return failure
	}
	until, err = s.excelBPSRateLimitDeadline(ctx, account.ID, excelBPSRateLimitScope{model: upstreamModel, endpoint: endpoint})
	if err != nil {
		return newExcelBPSStateUnavailable(err, true)
	}
	if time.Now().Before(until) {
		failure := newExcelBPSRateLimitedFailoverError(until.UTC().Format(http.TimeFormat))
		failure.RequestNotSent = true
		return failure
	}
	return nil
}

func (b *responseBPSExecution) send(ctx context.Context, account *Account, request *http.Request, proxy string) (*http.Response, error) {
	role, _ := ctx.Value(responseBPSRoleKey{}).(model.AttemptRole)
	if !responseBPSRoleEndpoint(role, request.URL.String()) {
		_ = request.Body.Close()
		return nil, execution.ErrInvalidPreparedIdentity
	}
	upstreamModel, _ := ctx.Value(excelBPSUpstreamModelKey{}).(string)
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	grant, ok := b.service.excelBPSGrantForToken(ctx, account, token)
	if !ok || !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") || request.Header.Get("X-Openai-Account-Id") != request.Header.Get("Chatgpt-Account-Id") ||
		HTTPUpstreamProfileFromContext(ctx) != HTTPUpstreamProfileExcelBPS {
		_ = request.Body.Close()
		return nil, ErrNativeDispatchGrantUnproven
	}
	r := b.owner.dispatch.request
	grantWire, _ := json.Marshal([]any{grant.fingerprint, grant.identity, token, request.Header.Get("Chatgpt-Account-Id")})
	purpose, _ := role.Purpose()
	identity := model.PreparedIdentity{Purpose: purpose, Role: role, Binding: model.ModeBinding{BillingScope: r.OperationID},
		OperationID: r.OperationID, ResourceID: account.ID, Owner: recoveryOwner(account), UserID: r.UserID, APIKeyID: r.APIKeyID,
		GroupID: r.GroupID, GroupBound: r.GroupBound, Mode: VendorOpenAIModeBPS, Transport: "http", GrantDigest: sha256.Sum256(grantWire), BodyKind: model.BodyReplayable}
	binder, ok := b.owner.dispatch.authority.leases.(nativeDispatchLeaseBinder)
	if !ok {
		_ = request.Body.Close()
		return nil, ErrNativeDispatchLeaseUnproven
	}
	bound, detach, err := binder.BindNativeDispatchLeases(ctx, identity)
	if err != nil {
		_ = request.Body.Close()
		return nil, err
	}
	request = request.WithContext(bound)
	wire, err := b.owner.dispatch.transport.PrepareNativeRequest(request, b.owner.dispatch.maxBody)
	if err != nil {
		detach()
		return nil, err
	}
	if wire.Body != nil && wire.Body != http.NoBody && wire.GetBody == nil {
		identity.BodyKind = model.BodyExclusiveStream
	}
	targetCtx := withTurnRecoveryTarget(bound, account, "http", wire.URL.Path, proxy, wire.Header.Get("Authorization"))
	target, _ := targetCtx.Value(turnRecoveryTargetKey{}).(model.AttemptTarget)
	target.Mode, target.Model = VendorOpenAIModeBPS, upstreamModel
	wireLimit := b.owner.dispatch.maxBody
	if wire.Header.Get("Content-Encoding") == "zstd" && wire.ContentLength > wireLimit {
		wireLimit = wire.ContentLength
	}
	prepared, err := PrepareResponseModeHTTP(bound, native.Spec{Request: wire, Identity: identity,
		Transport:          native.TransportOptions{Profile: native.ProfileExcelBPS, AccountID: account.ID, AccountConcurrency: account.Concurrency, ProxyURL: proxy, PublicHostsOnly: HTTPUpstreamPublicHostsOnly(bound)},
		MaxReplayBodyBytes: wireLimit}, b.owner.dispatch.transport, target)
	if err != nil {
		detach()
		return nil, err
	}
	// Derive the registered target from the final sealed bytes, never the
	// pre-encoding input or an asserted digest from the adapter.
	prepared.target.InputDigest = prepared.Identity().BodyDigest
	b.mu.Lock()
	b.seals[prepared.Identity()] = responseBPSSeal{account: snapshotOAuthRefreshAccount(account), token: token, model: upstreamModel,
		accountID: request.Header.Get("Chatgpt-Account-Id"), endpoint: request.URL.String(), grant: grant, wirePolicy: nativeDispatchWirePolicy(account)}
	b.mu.Unlock()
	b.admitMu.Lock()
	releaseAdmission := sync.OnceFunc(b.admitMu.Unlock)
	defer releaseAdmission()
	attempt, err := b.controller.Register(&responseBPSPrepared{ResponseModeHTTPAttempt: prepared, owner: b, releaseAdmission: releaseAdmission})
	if err == nil {
		err = attempt.Start(bound)
	}
	if err != nil {
		if attempt != nil {
			_ = attempt.Close()
		}
		detach()
		state := model.SendNotSent
		if result := prepared.Result(); result != nil {
			state = result.SendState
		}
		return nil, &OpenAITransportAttemptError{Cause: err, SendState: state}
	}
	response := prepared.Result().Response
	observation := &responseBPSObservation{attempt: attempt, decoder: nativeOperationDecoder{kind: nativeUpstreamResponses, sse: true}}
	b.owner.core.RequestID(attempt.Sequence(), response.Header.Get("x-request-id"))
	b.mu.Lock()
	b.responses[response] = observation
	b.mu.Unlock()
	response.Body = &responseBPSBody{ReadCloser: response.Body, observation: observation, detach: detach, status: response.StatusCode, service: b.service}
	return response, nil
}

// The shared core has already admitted and registered this exact sequence, but
// native.Open has not consumed the permit or performed any I/O. An uncertain
// journal write cannot grant permission; the concrete permit still revalidates
// current authority after this durable pre-send boundary.
type responseBPSPrepared struct {
	*ResponseModeHTTPAttempt
	owner            *responseBPSExecution
	releaseAdmission func()
}

func (p *responseBPSPrepared) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	if err := p.owner.preserveBeforeSend(ctx, permit.AttemptSequence(), p.Identity()); err != nil {
		p.releaseAdmission()
		return model.SendNotSent, execution.PreRequestFailure{}, err
	}
	p.releaseAdmission()
	return p.ResponseModeHTTPAttempt.Open(ctx, permit)
}

func (b *responseBPSExecution) preserveBeforeSend(ctx context.Context, sequence uint64, identity model.PreparedIdentity) error {
	journal, ok := b.service.usageBillingRepo.(BPSUsageJournal)
	if !ok || nativeDispatchNil(journal) {
		return ErrResponseBPSJournalUnavailable
	}
	current := b.currentUnfinalized()
	var sequences []uint64
	registered := false
	for _, attempt := range current.Attempts {
		if attempt.Identity.Mode != VendorOpenAIModeBPS {
			continue
		}
		sequences = append(sequences, attempt.Sequence)
		if attempt.Sequence == sequence {
			registered = attempt.Identity == identity && attempt.Send == model.SendUnknown && !attempt.Closed && attempt.Usage == nil && attempt.RequestID == "" && attempt.ResponseID == ""
		}
	}
	if !registered {
		return execution.ErrOperationFacts
	}
	timeout := usageRecordPoolOptionsFromConfig(b.service.cfg).TaskTimeout
	if timeout <= 0 {
		return ErrResponseBPSJournalUnavailable
	}
	persistCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	b.mu.Lock()
	observations := make([]*responseBPSObservation, 0, len(b.responses))
	for _, observed := range b.responses {
		observations = append(observations, observed)
	}
	b.mu.Unlock()
	usage := make(map[uint64]OpenAIUsage, len(observations))
	for _, observed := range observations {
		observed.mu.Lock()
		if observed.decoder.usage != nil {
			usage[observed.attempt.Sequence()] = observed.usage
		}
		observed.mu.Unlock()
	}
	result, err := journal.RecordBPSUsageEvidence(persistCtx, BPSUsageJournalInput{BeforeSendSequence: sequence, Finalization: BPSOperationFinalization{
		Operation: b.owner.core.Snapshot(), ClientTransport: OpenAIClientTransport(b.owner.dispatch.request.Transport), CurrentSequences: sequences, UsageByAttempt: usage}})
	if err != nil || persistCtx.Err() != nil {
		return errors.Join(err, context.Cause(persistCtx))
	}
	for _, receipt := range result.Receipts {
		if receipt.APIKeyID != current.APIKeyID || receipt.ProbeID != "" || receipt.RootRequestID != "" || !receipt.Receipt.Durable || receipt.Receipt.EvidenceID == "" || receipt.Receipt.Disposition != model.BillingPendingUsage {
			continue
		}
		for _, observed := range receipt.Attempts {
			if observed == sequence {
				return nil
			}
		}
	}
	return ErrNativeOperationBillingReceipt
}

// HTTP Execute advances its cycle boundary; a recovering WS root retains the
// entire attempt history. Durable receipts already identify completed journal
// groups. Keep those facts on the core while preserving the new group's actual
// monotonically increasing sequences (including concurrently open controls).
func (b *responseBPSExecution) currentUnfinalized() model.OperationResult {
	current := b.owner.core.CycleResult()
	attempts := current.Attempts[:0]
	for _, attempt := range current.Attempts {
		if attempt.Identity.Mode == VendorOpenAIModeBPS && !attempt.Billing.Durable {
			attempts = append(attempts, attempt)
		}
	}
	current.Attempts = attempts
	return current
}

type responseBPSObservation struct {
	attempt *ResponseModeAttempt
	mu      sync.Mutex // converter callback and body EOF may race cancellation
	decoder nativeOperationDecoder
	usage   OpenAIUsage // original upstream counters for the existing BPS calculator
	err     error
}

// Observe converter input, never client-projected/compaction-merged output.
// Each correction response has its own decoder and attempt/usage identity.
func observeResponseBPSEvent(ctx context.Context, response *http.Response, raw []byte) error {
	b := responseBPSFromContext(ctx)
	if b == nil {
		return nil
	}
	b.mu.Lock()
	observed := b.responses[response]
	b.mu.Unlock()
	if observed == nil {
		return execution.ErrOperationFacts
	}
	observed.mu.Lock()
	defer observed.mu.Unlock()
	if observed.err != nil {
		return observed.err
	}
	previous := model.CloneUsageFact(observed.decoder.usage)
	previousBilling := observed.usage
	b.service.parseSSEUsageBytes(raw, &observed.usage)
	facts := observed.decoder.observePayload(raw)
	if gjson.GetBytes(raw, "type").String() == "error" && facts.Terminal == model.TerminalUnknown {
		observed.decoder.terminal = model.TerminalFailed
		facts = observed.decoder.facts()
	}
	if previous != nil && facts.Usage != nil && facts.Terminal != model.TerminalUnknown {
		// BPS has emitted positive watermarks followed by an all-zero terminal.
		// Keep measured positives as lower-bound evidence instead of allowing a
		// confidence downgrade to erase them. Missing counters remain missing.
		contradiction := false
		for _, counter := range []struct {
			prior          *model.TokenMeasurement
			current, saved **model.TokenMeasurement
		}{
			{previous.Input, &facts.Usage.Input, &observed.decoder.usage.Input}, {previous.Output, &facts.Usage.Output, &observed.decoder.usage.Output},
			{previous.CacheRead, &facts.Usage.CacheRead, &observed.decoder.usage.CacheRead}, {previous.CacheWrite, &facts.Usage.CacheWrite, &observed.decoder.usage.CacheWrite},
			{previous.ImageInput, &facts.Usage.ImageInput, &observed.decoder.usage.ImageInput}, {previous.ImageOutput, &facts.Usage.ImageOutput, &observed.decoder.usage.ImageOutput},
			{previous.HostedImageInput, &facts.Usage.HostedImageInput, &observed.decoder.usage.HostedImageInput}, {previous.HostedImageOutput, &facts.Usage.HostedImageOutput, &observed.decoder.usage.HostedImageOutput},
			{previous.Total, &facts.Usage.Total, &observed.decoder.usage.Total}, {previous.Reasoning, &facts.Usage.Reasoning, &observed.decoder.usage.Reasoning},
		} {
			if counter.prior != nil && counter.prior.Value > 0 && *counter.current != nil && (*counter.current).Value == 0 {
				*counter.current, *counter.saved = counter.prior, counter.prior
				contradiction = true
			}
		}
		if contradiction {
			facts.Usage.Confidence, observed.decoder.finalUsage = model.UsagePartial, false
			for _, counter := range []struct {
				current *int
				prior   int
			}{
				{&observed.usage.InputTokens, previousBilling.InputTokens}, {&observed.usage.OutputTokens, previousBilling.OutputTokens},
				{&observed.usage.CacheReadInputTokens, previousBilling.CacheReadInputTokens}, {&observed.usage.CacheCreationInputTokens, previousBilling.CacheCreationInputTokens},
				{&observed.usage.CacheCreation5mTokens, previousBilling.CacheCreation5mTokens}, {&observed.usage.CacheCreation1hTokens, previousBilling.CacheCreation1hTokens},
				{&observed.usage.CacheCreation30mTokens, previousBilling.CacheCreation30mTokens}, {&observed.usage.CacheCreation12hTokens, previousBilling.CacheCreation12hTokens},
				{&observed.usage.ImageInputTokens, previousBilling.ImageInputTokens}, {&observed.usage.ImageOutputTokens, previousBilling.ImageOutputTokens}, {&observed.usage.ImageCacheReadTokens, previousBilling.ImageCacheReadTokens},
			} {
				if *counter.current == 0 && counter.prior > 0 {
					*counter.current = counter.prior
				}
			}
		}
	}
	if usage := facts.Usage; usage != nil && usage.Confidence == model.UsageFinal &&
		((usage.Input != nil && usage.Input.Value != int64(observed.usage.InputTokens)) || (usage.Output != nil && usage.Output.Value != int64(observed.usage.OutputTokens))) {
		// BPS keeps positive progressive counters when an all-zero terminal
		// contradicts them. Retain both facts without certifying a zero bill.
		observed.decoder.finalUsage = false
		usage.Confidence = model.UsagePartial
	}
	observed.err = errors.Join(observed.err, observed.attempt.Observe(facts))
	if observed.err != nil {
		b.controller.Cancel(observed.err)
	}
	return observed.err
}

func completeResponseBPSAttachment(ctx context.Context, response *http.Response) error {
	b := responseBPSFromContext(ctx)
	if b == nil {
		return nil
	}
	b.mu.Lock()
	observed := b.responses[response]
	b.mu.Unlock()
	if observed == nil {
		return execution.ErrOperationFacts
	}
	return observed.attempt.Observe(model.FactDelta{Terminal: model.TerminalCompleted})
}

type responseBPSBody struct {
	io.ReadCloser
	observation *responseBPSObservation
	detach      context.CancelFunc
	once        sync.Once
	err         error
	status      int
	service     *OpenAIGatewayService
	errorBody   []byte // only bounded original non-2xx JSON; SSE stays converter-owned
	overflow    bool
}

func (b *responseBPSBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.status >= 300 && n > 0 && !b.overflow {
		if len(b.errorBody)+n > 512<<10 {
			b.errorBody, b.overflow = nil, true
		} else {
			b.errorBody = append(b.errorBody, p[:n]...)
		}
	}
	if errors.Is(err, io.EOF) {
		if b.status >= 300 {
			b.observation.mu.Lock()
			facts := model.FactDelta{Terminal: model.TerminalFailed}
			if !b.overflow {
				b.service.parseSSEUsageBytes(b.errorBody, &b.observation.usage)
				facts = b.observation.decoder.observePayload(b.errorBody)
				facts.Terminal = model.TerminalFailed
				if facts.Usage == nil && !IsUpstreamContentRefusal(b.errorBody) && !gjson.GetBytes(b.errorBody, "usage").Exists() && !gjson.GetBytes(b.errorBody, "response.usage").Exists() &&
					(b.status == http.StatusUnauthorized || b.status == http.StatusForbidden || b.status == http.StatusTooManyRequests) {
					digest := sha256.Sum256(append([]byte(fmt.Sprint(b.status)), b.errorBody...))
					facts.Generation, facts.RejectionEvidenceID = model.GenerationRejected, fmt.Sprintf("%x", digest)
				}
			}
			b.observation.err = errors.Join(b.observation.err, b.observation.attempt.Observe(facts))
			b.observation.mu.Unlock()
		}
		if observedErr := b.observation.attempt.Observe(model.FactDelta{EndOfStream: true}); observedErr != nil {
			err = errors.Join(err, observedErr)
		}
	}
	return n, err
}

func (b *responseBPSBody) Close() error {
	b.once.Do(func() {
		b.err = b.observation.attempt.Close()
		b.detach()
	})
	return b.err
}

func recordResponseBPSDelivery(ctx context.Context, raw []byte, err error) {
	b := responseBPSFromContext(ctx)
	if b == nil {
		return
	}
	business := nativeClientBytesCommit(append([]byte("data: "), raw...), "text/event-stream")
	receipt := model.DeliveryReceipt{FlushAttempted: true, Flushed: err == nil, BusinessCommitted: business && err == nil, PossiblyCommitted: business && err != nil, Closed: err != nil}
	if err == nil {
		receipt.BytesWritten = int64(len(raw))
	}
	b.owner.core.Delivered(b.owner.active.Load(), receipt)
}

// Only public customer roots enter this path. Existing admin/probe callers
// retain their separate owner; a present customer identity cannot fall back.
func (s *OpenAIGatewayService) sendResponseBPS(ctx context.Context, c *gin.Context, account *Account, request *http.Request, proxy string) (*http.Response, error) {
	b, err := s.bindResponseBPS(ctx)
	if err != nil {
		_ = request.Body.Close()
		return nil, err
	}
	if c != nil {
		c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
	}
	return b.send(ctx, account, request, proxy)
}

var ErrResponseBPSJournalUnavailable = errors.New("BPS operation usage journal is unavailable")

// BPSOperationFinalization keeps the original caller/pricing inputs and the
// exact converter observations. CanonicalRequestID may be unknown; OperationID
// and BillingScope are technical identities and must never replace it.
type BPSOperationFinalization struct {
	Operation          model.OperationResult
	ClientTransport    OpenAIClientTransport
	CurrentSequences   []uint64
	CanonicalRequestID string
	UsageByAttempt     map[uint64]OpenAIUsage
	UsageInput         *OpenAIRecordUsageInput
}

type BPSOperationBilling interface {
	RecordBPSOperationUsage(context.Context, BPSOperationFinalization) ([]model.ModeBillingReceipt, error)
}

var _ BPSOperationBilling = (*OpenAIGatewayService)(nil)

// BPSUsageJournalInput requires the ORIGINAL usage journal to preserve these
// mode facts. Command is produced by the existing BPS amount calculator only
// for measured final usage or a stopped stream's confirmed lower bound and a
// true money ID. Unmeasured attempts retain separate pending receipts.
// A failed compaction can have its own canonical ID; no implicit scope-wide ACK.
type BPSUsageJournalInput struct {
	// Nonzero means the core registered this not-yet-opened attempt. It grants
	// no send permission and carries no monetary ID/command or customer quote.
	BeforeSendSequence uint64
	Finalization       BPSOperationFinalization
	Command            *UsageBillingCommand
	UsageLog           *UsageLog
	SimpleUsage        bool
	ChargedSequences   []uint64
}

type BPSUsageJournalResult struct {
	Receipts    []model.ModeBillingReceipt
	ActualApply *UsageBillingApplyResult
}

// The repository owner must implement this alongside the existing monetary
// transaction/recovery journal. No default, asynchronous enqueue or RecordUsage
// error-only return can manufacture its durable acknowledgement.
type BPSUsageJournal interface {
	RecordBPSUsageEvidence(context.Context, BPSUsageJournalInput) (BPSUsageJournalResult, error)
}

// BPSAttemptBillable preserves confirmed terminal usage and the original BPS
// stopped-stream lower-bound policy. Close plus a measured positive counter
// proves consumed work; it never supplies an absent counter or proves success.
func BPSAttemptBillable(attempt model.AttemptFact) (billable, lowerBound bool) {
	u := attempt.Usage
	if attempt.Identity.Purpose != model.DispatchGeneration || !attempt.Closed || u == nil || attempt.Send == model.SendNotSent {
		return false, false
	}
	if u.Confidence == model.UsageFinal && u.TerminalConfirmed && u.Input != nil && u.Output != nil {
		return true, false
	}
	for _, value := range []*model.TokenMeasurement{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.ImageInput, u.ImageOutput} {
		if value != nil && value.Value > 0 {
			return true, true
		}
	}
	return false, false
}

// Non-generating controls need no customer monetary command once the actual
// upload result was validated. Unknown controls remain evidence, not replay permission.
func BPSAttemptNotRequired(attempt model.AttemptFact) bool {
	return attempt.Closed && attempt.Usage == nil && (attempt.Send == model.SendNotSent || attempt.Generation == model.GenerationRejected ||
		(attempt.Identity.Purpose != model.DispatchGeneration && attempt.Terminal == model.TerminalCompleted))
}

// Preserve the original ingress contract: WS billing follows response.id;
// HTTP prefers x-request-id and falls back only when that header is absent.
// Both values must come from the original upstream attempt, never operation ID
// or the client-projected response. The root supplies and journals transport.
func BPSCanonicalRequestID(attempt model.AttemptFact, transport OpenAIClientTransport) string {
	if transport == OpenAIClientTransportWS && attempt.ResponseID != "" {
		return attempt.ResponseID
	}
	if attempt.RequestID != "" {
		return attempt.RequestID
	}
	return attempt.ResponseID
}

func (s *OpenAIGatewayService) RecordBPSOperationUsage(ctx context.Context, in BPSOperationFinalization) ([]model.ModeBillingReceipt, error) {
	if s == nil || ctx == nil {
		return nil, ErrResponseBPSJournalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	if in.Operation.Principal != model.PrincipalCustomer || in.Operation.UserID <= 0 || in.Operation.APIKeyID <= 0 || in.Operation.OperationID == "" || len(in.CurrentSequences) == 0 {
		return nil, execution.ErrOperationFacts
	}
	journal, ok := s.usageBillingRepo.(BPSUsageJournal)
	if !ok || nativeDispatchNil(journal) {
		return nil, ErrResponseBPSJournalUnavailable
	}
	write := BPSUsageJournalInput{Finalization: in}
	current := make(map[uint64]bool, len(in.CurrentSequences))
	for _, sequence := range in.CurrentSequences {
		current[sequence] = true
	}
	hasUsage, matchedID, lowerBound := false, false, false
	generations := 0
	var usage OpenAIUsage
	for _, attempt := range in.Operation.Attempts {
		if !current[attempt.Sequence] {
			continue
		}
		matchedID = matchedID || (in.CanonicalRequestID != "" && BPSCanonicalRequestID(attempt, in.ClientTransport) == in.CanonicalRequestID)
		if (attempt.Send == model.SendNotSent || attempt.Generation == model.GenerationRejected) && attempt.Usage == nil {
			continue
		}
		if attempt.Identity.Purpose == model.DispatchGeneration {
			generations++
		}
		observed, exists := in.UsageByAttempt[attempt.Sequence]
		billable, partial := BPSAttemptBillable(attempt)
		if exists && billable && in.UsageInput != nil && in.UsageInput.Account != nil && attempt.Identity.ResourceID == in.UsageInput.Account.ID {
			addImagePolicyUsage(&usage, observed)
			hasUsage = true
			lowerBound = lowerBound || partial
			write.ChargedSequences = append(write.ChargedSequences, attempt.Sequence)
		}
	}
	var record *openAIUsageRecord
	var prepareErr error
	input := in.UsageInput
	if hasUsage && matchedID && input != nil && input.Result != nil && input.Account != nil &&
		input.APIKey != nil && input.APIKey.ID == in.Operation.APIKeyID && input.User != nil && input.User.ID == in.Operation.UserID {
		copyInput, copyResult := *input, *input.Result
		// Billing uses the ORIGINAL BPS decoder snapshots; client model/cache/
		// compact-window projection is not a new source of monetary counters.
		copyResult.Usage, copyResult.RequestID = usage, in.CanonicalRequestID
		copyResult.UsageLowerBound = lowerBound || len(write.ChargedSequences) != generations
		copyInput.Result = &copyResult
		record, prepareErr = s.prepareOpenAIUsage(ctx, &copyInput)
		if prepareErr == nil && record != nil && record.requestID == in.CanonicalRequestID {
			write.UsageLog, write.SimpleUsage = record.log, record.simpleUsageOnly
			record.params.UsageLogOnly = record.simpleUsageOnly
			write.Command = buildUsageBillingCommand(record.requestID, record.log, record.params)
		}
	}
	if write.Command == nil {
		write.ChargedSequences = nil
	}
	result, err := journal.RecordBPSUsageEvidence(ctx, write)
	if result.ActualApply != nil && record != nil && write.Command != nil {
		_, effectsErr := finishUsageBillingEffects(ctx, record.params, s.billingDeps(), result.ActualApply)
		err = errors.Join(err, effectsErr)
	}
	return result.Receipts, errors.Join(prepareErr, err)
}

func finalizeResponseBPSUsage(ctx context.Context, input *OpenAIRecordUsageInput, finalizer NativeOperationBilling, timeout time.Duration) (bool, error) {
	b := responseBPSFromContext(ctx)
	owner := b.owner
	if owner.finalized {
		return true, owner.finalizeErr
	}
	owner.finalized = true
	current := b.currentUnfinalized()
	transport := OpenAIClientTransport(owner.dispatch.request.Transport)
	var sequences []uint64
	for _, attempt := range current.Attempts {
		if attempt.Identity.Mode != VendorOpenAIModeBPS {
			continue
		}
		sequences = append(sequences, attempt.Sequence)
	}
	if len(sequences) == 0 {
		return true, nil
	}
	if err := b.controller.RecordBilling(model.ModeBillingReceipt{APIKeyID: current.APIKeyID, Attempts: sequences, Receipt: model.BillingReceipt{Disposition: model.BillingPendingUsage}}); err != nil {
		owner.finalizeErr = err
		return true, err
	}
	port, available := finalizer.(BPSOperationBilling)
	if !available || nativeDispatchNil(port) || timeout <= 0 {
		owner.finalizeErr = ErrResponseBPSJournalUnavailable
		return true, owner.finalizeErr
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	var releaseReservation func()
	if reservation := InflightReservationFromContext(ctx); reservation != nil {
		releaseReservation = reservation.Acquire()
	}
	b.mu.Lock()
	observations := make([]*responseBPSObservation, 0, len(b.responses))
	for _, observed := range b.responses {
		observations = append(observations, observed)
	}
	b.mu.Unlock()
	usage := make(map[uint64]OpenAIUsage, len(observations))
	for _, observed := range observations {
		observed.mu.Lock()
		if observed.decoder.usage != nil {
			usage[observed.attempt.Sequence()] = observed.usage
		}
		owner.finalizeErr = errors.Join(owner.finalizeErr, observed.err)
		observed.mu.Unlock()
	}
	canonical := ""
	candidate := ""
	if input != nil && input.Result != nil {
		candidate = input.Result.RequestID
		for _, attempt := range current.Attempts {
			if candidate != "" && candidate == BPSCanonicalRequestID(attempt, transport) {
				canonical = candidate
				break
			}
		}
	}
	if candidate == "" {
		// Withheld tools can report identity before any client projection. The
		// original generation (or a compact-only failure) owns its money ID.
		for _, role := range []model.AttemptRole{model.AttemptGenerate, model.AttemptCompact} {
			for _, attempt := range current.Attempts {
				if attempt.Identity.Role == role && attempt.Usage != nil {
					canonical = BPSCanonicalRequestID(attempt, transport)
					if canonical != "" {
						break
					}
				}
			}
			if canonical != "" {
				break
			}
		}
	}
	snapshot := owner.core.Snapshot()
	snapshot.Failure = owner.cycleErr
	receipts, err := port.RecordBPSOperationUsage(cleanup, BPSOperationFinalization{Operation: snapshot, ClientTransport: transport, CurrentSequences: sequences, CanonicalRequestID: canonical, UsageByAttempt: usage, UsageInput: input})
	covered := make(map[uint64]bool, len(sequences))
	wanted := make(map[uint64]bool, len(sequences))
	finalUsage := make(map[uint64]bool, len(sequences))
	notRequired := make(map[uint64]bool, len(sequences))
	observedRoots := make(map[uint64]string, len(sequences))
	for _, attempt := range current.Attempts {
		finalUsage[attempt.Sequence], _ = BPSAttemptBillable(attempt)
		notRequired[attempt.Sequence] = BPSAttemptNotRequired(attempt)
		observedRoots[attempt.Sequence] = BPSCanonicalRequestID(attempt, transport)
	}
	for _, sequence := range sequences {
		wanted[sequence] = true
	}
	for _, receipt := range receipts {
		if receipt.APIKeyID != snapshot.APIKeyID || receipt.ProbeID != "" || !receipt.Receipt.Durable || receipt.Receipt.EvidenceID == "" {
			err = errors.Join(err, ErrNativeOperationBillingReceipt)
			continue
		}
		valid := true
		for _, sequence := range receipt.Attempts {
			valid = valid && wanted[sequence] && !covered[sequence]
			if receipt.Receipt.Disposition == model.BillingNotRequired {
				valid = valid && notRequired[sequence] && receipt.RootRequestID == ""
			} else if receipt.Receipt.Disposition == model.BillingPendingUsage {
				valid = valid && (receipt.RootRequestID == observedRoots[sequence] || receipt.RootRequestID == "")
			} else {
				valid = valid && receipt.RootRequestID == canonical
			}
			if receipt.Receipt.Disposition == model.BillingSettled || receipt.Receipt.Disposition == model.BillingPendingPricing {
				valid = valid && finalUsage[sequence] && canonical != ""
			}
		}
		if !valid || len(receipt.Attempts) == 0 {
			err = errors.Join(err, ErrNativeOperationBillingReceipt)
			continue
		}
		if recordErr := b.controller.RecordBilling(receipt); recordErr != nil {
			err = errors.Join(err, recordErr)
			continue
		}
		for _, sequence := range receipt.Attempts {
			covered[sequence] = true
		}
	}
	if len(covered) != len(wanted) {
		err = errors.Join(err, ErrNativeOperationBillingReceipt)
	}
	owner.finalizeErr = errors.Join(owner.finalizeErr, err, context.Cause(cleanup))
	if len(covered) == len(wanted) && releaseReservation != nil {
		releaseReservation()
	}
	return true, owner.finalizeErr
}
