package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	protocol "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	httpattempt "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	provider "github.com/Wei-Shaw/sub2api/internal/providers/typesafe"
)

type systemOneDispatch struct {
	mu        sync.RWMutex
	svc       *GatewayService
	principal *NativeDispatchAuthority
	capacity  *nativeDispatchCapacity
	transport NativeHTTPUpstream
	request   NativeDispatchRequest
	// Root-owned registry: provider/client-supplied identities are not grants.
	prepared map[model.PreparedIdentity][32]byte
}

var ErrSystemOnePreparation = errors.New("System One preparation could not be preserved")

func (s *GatewayService) newSystemOneDispatch(ctx context.Context, in SystemOneExecutionInput) (*systemOneDispatch, error) {
	scope := routing.CapacityScopeFrom(ctx)
	transport, ok := s.httpUpstream.(NativeHTTPUpstream)
	if !ok || nativeDispatchNil(transport) || scope == nil || in.APIKey == nil || in.APIKey.User == nil || in.APIKey.GroupID == nil || s.cfg == nil || s.cfg.Gateway.MaxBodySize <= 0 || nativeDispatchNil(s.userRepo) {
		return nil, execution.ErrDispatchUnavailable
	}
	capacity := &nativeDispatchCapacity{users: s.userRepo, accounts: s.accountRepo, settings: s.settingService, scope: scope}
	request := NativeDispatchRequest{OperationID: in.OperationID, UserID: in.APIKey.UserID, APIKeyID: in.APIKey.ID, APIKeyDigest: sha256.Sum256([]byte(in.APIKey.Key)), GroupBound: true, GroupID: *in.APIKey.GroupID,
		RequestedModel: in.RequestedModel, ClientIP: in.SecurityClientIP, Transport: "http", SimpleMode: s.cfg.RunMode == config.RunModeSimple}
	principal, err := NewNativeDispatchAuthority(in.KeyService, s.accountRepo, s.userSubRepo, nil, capacity, request)
	if err != nil {
		return nil, err
	}
	return &systemOneDispatch{svc: s, principal: principal, capacity: capacity, transport: transport, request: request, prepared: make(map[model.PreparedIdentity][32]byte)}, nil
}

func (d *systemOneDispatch) loadPrincipal(ctx context.Context) (*APIKey, *UserSubscription, time.Time, error) {
	key, sub, until, err := d.principal.loadKey(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	switch key.Group.Platform {
	case PlatformTypeSafe:
		if d.request.RequestedModel != protocol.JevLatestModel {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("systemone model", ErrGroupNotAllowed)
		}
	case PlatformComposite:
		// Resolve current rules directly. The ingress decision in context is a
		// frozen routing hint and cannot authorize a later revoked/changed alias.
		decision, err := d.svc.compositeResolver.Resolve(ctx, key.Group.ID, d.request.RequestedModel, CompositeRouteEndpointAny)
		if err != nil {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("systemone composite dependency", err)
		}
		if !decision.Matched || decision.TargetPlatform != PlatformTypeSafe || decision.UpstreamModel != protocol.JevLatestModel {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("systemone composite scope", ErrGroupNotAllowed)
		}
	default:
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("systemone platform", ErrGroupNotAllowed)
	}
	return key, sub, until, nil
}

func (d *systemOneDispatch) resource(ctx context.Context, id int64) (*Account, string, string, [32]byte, error) {
	var empty [32]byte
	a, err := d.svc.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, "", "", empty, err
	}
	if a == nil || a.ID != id || !a.IsTypeSafe() || a.Type != AccountTypeAPIKey || !a.IsSchedulable() || a.IsShadow() || !a.IsModelSupported(protocol.JevLatestModel) {
		return nil, "", "", empty, ErrAccountNotFound
	}
	if !d.request.SimpleMode && !access.ResourceInGroup(&d.request.GroupID, a.GroupIDs) {
		return nil, "", "", empty, ErrGroupNotAllowed
	}
	if err = d.principal.checks.ValidateVendorSupply(ctx, a, &d.request.GroupID); err != nil {
		return nil, "", "", empty, err
	}
	if err := d.svc.settingService.checkManagedAccountModels(ctx, a, d.request.RequestedModel, protocol.JevLatestModel); err != nil {
		return nil, "", "", empty, err
	}
	base, err := d.svc.validateUpstreamBaseURL(a.GetTypeSafeBaseURL())
	if err != nil {
		return nil, "", "", empty, err
	}
	proxy := resolveAccountProxyURL(a)
	if a.ProxyID != nil && (a.Proxy == nil || a.Proxy.ID != *a.ProxyID) {
		return nil, "", "", empty, ErrNativeDispatchScopeChanged
	}
	if a.GetTypeSafeAPIKey() == "" {
		return nil, "", "", empty, ErrNativeDispatchGrantUnproven
	}
	// Include actual owner, credential, target and proxy generation. Digests
	// are evidence only; plaintext never enters operation/journal projections.
	var proxyGeneration any
	if a.Proxy != nil {
		proxyGeneration = []any{a.Proxy.ID, a.Proxy.ConfigVersion, a.Proxy.VendorID}
	}
	raw, err := json.Marshal([]any{a.ID, a.VendorID, a.Type, a.GetTypeSafeAPIKey(), base, proxy, proxyGeneration})
	if err != nil {
		return nil, "", "", empty, err
	}
	return a, base, proxy, sha256.Sum256(raw), nil
}

func (d *systemOneDispatch) RevalidateDispatch(ctx context.Context, id model.PreparedIdentity) (validation execution.DispatchValidation, err error) {
	defer func() {
		if err != nil {
			err = nativeDispatchError("systemone dispatch", err)
		}
	}()
	var denied execution.DispatchValidation
	d.mu.RLock()
	seal, registered := d.prepared[id]
	d.mu.RUnlock()
	if !registered || !id.Valid() || id.OperationID != d.request.OperationID || id.UserID != d.request.UserID || id.APIKeyID != d.request.APIKeyID || !id.GroupBound || id.GroupID != d.request.GroupID || id.Mode != "native" || id.Transport != "http" || id.Purpose != model.DispatchGeneration {
		return denied, execution.ErrInvalidPreparedIdentity
	}
	key, sub, deadline, err := d.loadPrincipal(ctx)
	if err != nil {
		return denied, err
	}
	a, _, _, grant, err := d.resource(ctx, id.ResourceID)
	if err != nil {
		return denied, err
	}
	if grant != id.GrantDigest || grant != seal || id.Owner != recoveryOwner(a) {
		return denied, execution.ErrDispatchChanged
	}
	proof, err := d.capacity.RevalidateNativeDispatchLeases(ctx, id)
	if err != nil {
		return denied, err
	}
	deadline = nativeDispatchEarlier(deadline, proof.ValidUntil)
	if a.AutoPauseOnExpired && a.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *a.ExpiresAt)
	}
	fingerprint, err := d.principal.authorizationFingerprint(key, sub, a, NativeDispatchCredential{Owner: recoveryOwner(a), GrantDigest: grant})
	if err != nil {
		return denied, err
	}
	return execution.DispatchValidation{Authorization: fingerprint, Leases: proof.Fingerprint, ValidUntil: deadline}, nil
}

func (d *systemOneDispatch) prepare(ctx context.Context, in SystemOneExecutionInput, accountID int64) (*provider.Attempt, *Account, error) {
	a, base, proxy, grant, err := d.resource(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	req, err := protocol.NewSystemOneRequest(ctx, base, a.GetTypeSafeAPIKey(), in.Body)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := d.transport.PrepareNativeRequest(req, d.svc.cfg.Gateway.MaxBodySize)
	if err != nil {
		return nil, nil, err
	}
	limit := d.svc.cfg.Gateway.MaxBodySize
	if encoded.Header.Get("Content-Encoding") == "zstd" && encoded.ContentLength > limit {
		limit = encoded.ContentLength
	}
	r := d.request
	identity := model.PreparedIdentity{OperationID: r.OperationID, UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound, ResourceID: a.ID, Owner: recoveryOwner(a), Mode: "native", Transport: "http", GrantDigest: grant, BodyKind: model.BodyReplayable}
	target := model.AttemptTarget{Owner: identity.Owner, Mode: "native", Endpoint: encoded.URL.Path, Model: protocol.JevLatestModel, InputDigest: sha256.Sum256(in.Body), GrantDigest: grant, ProxyDigest: sha256.Sum256([]byte(proxy)), Limit: max(2, 1+a.GetPoolModeRetryCount())}
	prepared, err := provider.Prepare(ctx, provider.Spec{HTTP: httpattempt.Spec{Request: encoded, Identity: identity, Transport: httpattempt.TransportOptions{Profile: httpattempt.ProfileDefault, AccountID: a.ID, AccountConcurrency: a.Concurrency, ProxyURL: proxy}, MaxReplayBodyBytes: limit}, Target: target}, d.transport)
	if err != nil {
		return nil, nil, err
	}
	d.mu.Lock()
	d.prepared[prepared.Identity()] = grant
	d.mu.Unlock()
	return prepared, a, nil
}

// Establish the original shared budgets/capacity receipts before ingress takes
// its user slot. A caller must not invoke this again inside an attempt loop.
func (s *GatewayService) SystemOneOperationContext(ctx context.Context) (context.Context, error) {
	if s == nil || s.cfg == nil || ctx == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	if routing.CapacityScopeFrom(ctx) != nil {
		return nil, execution.ErrOperationConsumed
	}
	// Optional argument is reasoning effort, not the model. System One has no
	// reasoning-effort field; establish its default policy once before queuing.
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg, "")
	ctx, _ = routing.WithCapacityScope(ctx)
	return ctx, nil
}

type systemOneRecoveryPorts struct {
	owner   *systemOneExecution
	account *Account
	failure *UpstreamFailoverError
}

func (p *systemOneRecoveryPorts) OutputCommitted(context.Context) bool {
	return p.owner.core.Committed()
}
func (p *systemOneRecoveryPorts) RetryAllowed(ctx context.Context) bool {
	return OpenAITurnRetryAllowed(ctx, p.account.ID, p.failure.ResponseBody)
}
func (p *systemOneRecoveryPorts) Prepare(ctx context.Context) error {
	return PrepareTurnRecovery(ctx, p.failure)
}
func (p *systemOneRecoveryPorts) Wait(ctx context.Context, d time.Duration) error {
	return WaitOpenAITurnRetry(ctx, d)
}
func (p *systemOneRecoveryPorts) Pin(context.Context)     {}
func (p *systemOneRecoveryPorts) Exclude(context.Context) {}
func (p *systemOneRecoveryPorts) TempUnschedule(ctx context.Context) {
	p.owner.svc.TempUnscheduleRetryableError(ctx, p.account.ID, p.failure)
}
func (p *systemOneRecoveryPorts) StopSwitch(context.Context, int) bool { return false }

// HTTP failures retain the original account policy. Replay eligibility is
// decided separately by the root from send/generation evidence.
func (s *GatewayService) systemOneHTTPFailure(ctx context.Context, account *Account, response *provider.Response) error {
	if IsSystemOneRequestErrorStatus(response.StatusCode) {
		return &SystemOneUpstreamError{StatusCode: response.StatusCode}
	}
	disabled := false
	if s.rateLimitService != nil {
		disabled = s.rateLimitService.HandleUpstreamError(ctx, account, response.StatusCode, response.Headers, response.Body, protocol.JevLatestModel)
	}
	if !disabled && !s.shouldFailoverUpstreamError(response.StatusCode) {
		return &SystemOneUpstreamError{StatusCode: response.StatusCode}
	}
	err := &UpstreamFailoverError{StatusCode: response.StatusCode, ResponseBody: response.Body, ResponseHeaders: response.Headers.Clone(), RetryableOnSameAccount: !disabled && account.IsPoolMode() && account.IsPoolModeRetryableStatus(response.StatusCode)}
	if response.StatusCode == http.StatusUnauthorized {
		err.Stage, err.Scope, err.Reason, err.NextAccountAction = GatewayFailureStageAccountAuth, GatewayFailureScopeAccount, TypeSafeCredentialRejectedReason, NextAccountRetry
	}
	return err
}

func systemOneCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

// No transport ambiguity is upgraded into an unsent proof by error text.
func systemOneTransportFailure(ctx context.Context, err error, send model.SendState) *UpstreamFailoverError {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || systemOneLocalFailure(err) {
		return nil
	}
	if send != model.SendNotSent {
		return nil
	}
	return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Cause: err, RequestNotSent: true, RetryableOnSameAccount: true, SameAccountRetryMax: 1, ResponseBody: gatewayTransportFailoverBody}
}

func systemOneLocalFailure(err error) bool {
	return IsNativeDispatchDenied(err) || errors.Is(err, ErrSystemOnePreparation) ||
		errors.Is(err, execution.ErrDispatchUnavailable) || errors.Is(err, execution.ErrDispatchExpired) || errors.Is(err, execution.ErrDispatchChanged) || errors.Is(err, execution.ErrInvalidPreparedIdentity) ||
		errors.Is(err, execution.ErrSendPermitConsumed) || IsTurnBudgetError(err)
}

// SystemOneUpstreamError carries only a status, never an upstream credential or body.
type SystemOneUpstreamError struct{ StatusCode int }

const TypeSafeCredentialRejectedReason GatewayFailureReason = "typesafe_api_key_rejected"

func (e *SystemOneUpstreamError) Error() string {
	return fmt.Sprintf("typesafe upstream rejected request with status %d", e.StatusCode)
}

func IsSystemOneRequestErrorStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
