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

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// NativeHTTPUpstream is the controlled repository transport. Encoding and
// destination validation finish before PrepareHTTP seals a request; the send
// port performs one transport attempt with no internal application recovery.
type NativeHTTPUpstream interface {
	native.Transport
	PrepareNativeRequest(*http.Request, int64) (*http.Request, error)
	ValidateNativeRedirect(*http.Request, []*http.Request, string) error
}

type nativeDispatchOperationKey struct{}

type nativeDispatchOperation struct {
	request       NativeDispatchRequest
	authority     *NativeDispatchAuthority
	dispatcher    *execution.Dispatcher
	budget        *OpenAITurnBudget
	transport     NativeHTTPUpstream
	maxBody       int64
	mu            sync.Mutex
	grants        map[int64]NativeDispatchCredential
	policies      map[model.PreparedIdentity][32]byte
	pricingFacts  map[model.PreparedIdentity]NativeIntentModelFacts
	responsePlans map[int64]responseExecutionPlan
	plugins       *PluginManager
	settings      *SettingService
}

// BindNativeHTTPDispatch attaches permission issuance to the already existing
// logical operation. The authenticated ingress supplies the original model/IP
// and its real capacity scope. Neither a fresh budget nor an unbounded lifetime
// is substituted when that ingress context is absent.
func (s *OpenAIGatewayService) BindNativeHTTPDispatch(ctx context.Context, keys *APIKeyService, request NativeDispatchRequest) (context.Context, error) {
	if request.Transport != "http" {
		return nil, execution.ErrDispatchUnavailable
	}
	return s.bindNativeDispatch(ctx, keys, request)
}

// BindNativeWSDispatch runs before routing acquisitions, using the same root
// constructor. Same-turn retries retain their actual operation and budget.
func (s *OpenAIGatewayService) BindNativeWSDispatch(ctx context.Context, keys *APIKeyService, request NativeDispatchRequest) (context.Context, error) {
	if ctx == nil || request.Transport != "ws" {
		return nil, execution.ErrDispatchUnavailable
	}
	if previous := nativeDispatchFromContext(ctx); previous != nil {
		budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
		request.OperationID = previous.request.OperationID
		if request != previous.request || budget != previous.budget {
			return nil, execution.ErrDispatchChanged
		}
		return ctx, nil
	}
	return s.bindNativeDispatch(ctx, keys, request)
}

func (s *OpenAIGatewayService) bindNativeDispatch(ctx context.Context, keys *APIKeyService, request NativeDispatchRequest) (context.Context, error) {
	if s == nil || ctx == nil || (request.Transport != "http" && request.Transport != "ws") || s.cfg == nil || s.cfg.Gateway.MaxBodySize <= 0 {
		return nil, execution.ErrDispatchUnavailable
	}
	if previous, _ := ctx.Value(nativeDispatchOperationKey{}).(*nativeDispatchOperation); previous != nil {
		return nil, fmt.Errorf("%w: operation already bound", execution.ErrDispatchChanged)
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	transport, ok := s.httpUpstream.(NativeHTTPUpstream)
	if !ok || nativeDispatchNil(transport) {
		return nil, execution.ErrDispatchUnavailable
	}
	ctx, capacity := routing.WithCapacityScope(ctx)
	leases, err := s.NativeDispatchCapacity(capacity)
	if err != nil {
		return nil, err
	}
	authority, err := NewNativeDispatchAuthority(keys, s.accountRepo, s.userSubRepo, s.openAITokenProvider, leases, request)
	if err != nil {
		return nil, err
	}
	budget.mu.Lock()
	control, attempts, output := budget.control, budget.attemptBudget, budget.firstOutput
	budget.mu.Unlock()
	op := &nativeDispatchOperation{request: request, authority: authority, budget: budget, transport: transport, maxBody: s.cfg.Gateway.MaxBodySize, grants: make(map[int64]NativeDispatchCredential), policies: make(map[model.PreparedIdentity][32]byte), pricingFacts: make(map[model.PreparedIdentity]NativeIntentModelFacts), plugins: s.pluginManager, settings: s.settingService}
	op.dispatcher, err = execution.NewDispatcher(control, request.OperationID, attempts, output, op)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, nativeDispatchOperationKey{}, op), nil
}

func nativeDispatchFromContext(ctx context.Context) *nativeDispatchOperation {
	if ctx == nil {
		return nil
	}
	op, _ := ctx.Value(nativeDispatchOperationKey{}).(*nativeDispatchOperation)
	return op
}

// RevalidateDispatch retains the existing recovery-probe owner in addition to
// the concrete principal, grant and capacity authority. No probe permission is
// inferred from an account score or an earlier selection snapshot.
func (o *nativeDispatchOperation) RevalidateDispatch(ctx context.Context, identity model.PreparedIdentity) (execution.DispatchValidation, error) {
	if identity.Transport == "ws" {
		return o.revalidateWS(ctx, identity)
	}
	if o.request.Transport == "ws" && identity.Transport == "http" {
		return o.revalidateWSHTTPBridge(ctx, identity)
	}
	return o.authority.revalidatePreparedTransport(ctx, identity, o.request.Transport, o.preparedResourceCheck(identity))
}

// preparedResourceCheck runs on the resource freshly loaded by the authority
// at this gate. Loading it once for wire policy and again for authorization
// could validate two different revisions, as well as duplicate repository I/O.
// It is never retained between Admit/Consume or used as cached authorization.
func (o *nativeDispatchOperation) preparedResourceCheck(identity model.PreparedIdentity) nativeDispatchResourceCheck {
	return func(ctx context.Context, account *Account) error {
		o.mu.Lock()
		facts := o.pricingFacts[identity]
		plan := o.responsePlans[identity.ResourceID]
		o.mu.Unlock()
		upstream := facts.UpstreamModel
		if upstream == "" {
			upstream = plan.upstreamModel
		}
		if err := o.settings.checkManagedAccountModels(ctx, account, o.request.RequestedModel, upstream); err != nil {
			return nativeDispatchError("managed model policy", err)
		}

		o.mu.Lock()
		policy, exists := o.policies[identity]
		o.mu.Unlock()
		if !exists || policy != nativeDispatchWirePolicy(account) || o.plugins.ShouldRouteOpenAIOAuth(account) {
			return nativeDispatchError("wire policy changed", ErrNativeDispatchScopeChanged)
		}
		o.budget.mu.Lock()
		recovery := o.budget.recovery
		o.budget.mu.Unlock()
		if recovery != nil {
			return recovery.beforeSend(ctx, account)
		}
		return nil
	}
}

func (o *nativeDispatchOperation) resolveCredential(ctx context.Context, accountID int64) (string, string, error) {
	grant, err := o.authority.ResolveCredential(ctx, accountID)
	if err != nil {
		return "", "", err
	}
	o.mu.Lock()
	o.grants[accountID] = grant
	o.mu.Unlock()
	token, kind := grant.Bearer()
	return token, kind, nil
}

func (o *nativeDispatchOperation) prepare(request *http.Request, account *Account, proxy string, anonymousRedirect bool) (*native.PreparedHTTP, model.AttemptTarget, error) {
	var target model.AttemptTarget
	if request == nil || request.URL == nil || account == nil {
		return nil, target, execution.ErrInvalidPreparedIdentity
	}
	o.mu.Lock()
	grant, exists := o.grants[account.ID]
	o.mu.Unlock()
	token, _ := grant.Bearer()
	anonymousRedirect = anonymousRedirect && request.Header.Get("Authorization") == ""
	// Existing explicit 401/WS recovery may have refreshed the bearer. Prepare
	// must resolve and seal that real persisted generation again, never accept
	// the caller's changed header as evidence of a credential replacement.
	if !exists || (!anonymousRedirect && sha256.Sum256([]byte(request.Header.Get("Authorization"))) != sha256.Sum256([]byte("Bearer "+token))) {
		var err error
		grant, err = o.authority.ResolveCredential(request.Context(), account.ID)
		if err != nil {
			if request.Body != nil {
				_ = request.Body.Close()
			}
			return nil, target, err
		}
		token, _ = grant.Bearer()
		exists = true
	}
	if !exists || token == "" || (!anonymousRedirect && sha256.Sum256([]byte(request.Header.Get("Authorization"))) != sha256.Sum256([]byte("Bearer "+token))) {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, target, nativeDispatchError("unsealed bearer", ErrNativeDispatchGrantUnproven)
	}
	fresh, owner, err := o.authority.loadResource(request.Context(), account.ID)
	if err != nil || resolveAccountProxyURL(fresh) != proxy || nativeDispatchWirePolicy(account) != nativeDispatchWirePolicy(fresh) {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		if err == nil {
			err = ErrNativeDispatchScopeChanged
		}
		return nil, target, nativeDispatchError("prepared resource", err)
	}
	// The token and tenant headers must describe the same durable credential
	// owner, including shadow accounts. A matching bearer alone does not prove
	// that a caller's cached ChatGPT workspace or FedRAMP header is current.
	currentGrant, err := o.authority.resolveCredential(request.Context(), owner, false)
	if err != nil || currentGrant.Owner != grant.Owner || currentGrant.GrantDigest != grant.GrantDigest {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		if err == nil {
			err = ErrNativeDispatchGrantUnproven
		}
		return nil, target, nativeDispatchError("credential header generation", err)
	}
	request = request.WithContext(request.Context())
	request.Header = request.Header.Clone()
	for name := range request.Header {
		if strings.EqualFold(name, "authorization") || (owner.IsOpenAIOAuthLike() &&
			(strings.EqualFold(name, "chatgpt-account-id") || strings.EqualFold(name, "x-openai-fedramp"))) {
			delete(request.Header, name)
		}
	}
	// Cross-origin redirects that lost Authorization must not reacquire
	// credential-owned headers while preparing their next hop. Rebuild even
	// Authorization to eliminate noncanonical duplicate keys on the wire.
	if !anonymousRedirect {
		request.Header.Set("Authorization", "Bearer "+token)
		if owner.IsOpenAIOAuthLike() {
			setOpenAIChatGPTAccountHeaders(request.Header, owner)
		}
	}
	if request.GetBody != nil && request.ContentLength > o.maxBody {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, target, native.ErrReplayBodyLimit
	}
	ctx := withTurnRecoveryTarget(request.Context(), account, "http", request.URL.Path, proxy, request.Header.Get("Authorization"))
	request = request.WithContext(ctx)
	target, _ = ctx.Value(turnRecoveryTargetKey{}).(model.AttemptTarget)
	facts, err := nativeIntentHTTPModelFacts(request, o.maxBody)
	if err != nil {
		return nil, target, errors.Join(err, request.Body.Close())
	}
	wire, err := o.transport.PrepareNativeRequest(request, o.maxBody)
	if err != nil {
		return nil, target, err
	}
	kind := model.BodyReplayable
	if wire.Body != nil && wire.Body != http.NoBody && wire.GetBody == nil {
		kind = model.BodyExclusiveStream
	}
	profile := native.ProfileDefault
	switch HTTPUpstreamProfileFromContext(ctx) {
	case HTTPUpstreamProfileDefault:
	case HTTPUpstreamProfileOpenAI:
		profile = native.ProfileOpenAI
	default:
		if wire.Body != nil {
			_ = wire.Body.Close()
		}
		return nil, target, execution.ErrInvalidPreparedIdentity
	}
	r := o.request
	wireLimit := o.maxBody
	// Compression is performed by the bounded repository encoder above. Its
	// framing may be larger than the already bounded source at the exact limit.
	if wire.Header.Get("Content-Encoding") == "zstd" && wire.ContentLength > wireLimit {
		wireLimit = wire.ContentLength
	}
	prepared, err := native.PrepareHTTP(ctx, native.Spec{Request: wire,
		Identity: model.PreparedIdentity{OperationID: r.OperationID, ResourceID: account.ID, Owner: grant.Owner,
			UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound,
			Mode: VendorOpenAIModeNative, Transport: "http", GrantDigest: grant.GrantDigest, BodyKind: kind},
		Transport:          native.TransportOptions{Profile: profile, AccountID: account.ID, AccountConcurrency: account.Concurrency, ProxyURL: proxy, PublicHostsOnly: HTTPUpstreamPublicHostsOnly(ctx)},
		MaxReplayBodyBytes: wireLimit}, o.transport)
	if err == nil {
		target.InputDigest = prepared.Identity().BodyDigest
		o.mu.Lock()
		o.policies[prepared.Identity()] = nativeDispatchWirePolicy(fresh)
		o.pricingFacts[prepared.Identity()] = facts
		o.mu.Unlock()
	}
	return prepared, target, err
}

// These are the current account-owned target and credential-bearing header
// policies. Token refresh itself is separate and bound by GrantDigest. A new
// endpoint/proxy/identity policy cannot authorize an already prepared old wire.
func nativeDispatchWirePolicy(account *Account) [32]byte {
	if account == nil {
		return [32]byte{}
	}
	var proxyVersion int64
	if account.Proxy != nil {
		proxyVersion = account.Proxy.ConfigVersion
	}
	data, _ := json.Marshal([]any{"native-wire-policy-v1", account.ID, account.Platform, account.Type,
		account.ParentAccountID, account.VendorID, account.ProxyID, resolveAccountProxyURL(account), proxyVersion,
		account.GetOpenAIBaseURL(), account.GetAPIProtocol(), account.UsesOpenAICodexProtocol(),
		account.Credentials[AccountModelReferencesKey], account.Credentials["model_mapping"],
		account.GetOpenAIUserAgent(), account.GetOpenAIDeviceID(), account.GetOpenAISessionID(), account.GetOpenAIOrganizationID()})
	return sha256.Sum256(data)
}

// roundTrip is the operation owner's explicit recovery loop. Each new wire
// attempt must pass Prepare, Admit and Open and spend the same original budget.
// The transport's observed pre-request TLS fact permits at most one repeat.

func (o *nativeDispatchOperation) roundTrip(request *http.Request, account *Account, proxy string) (response *http.Response, err error) {
	// This fact spans every TLS attempt and redirect hop belonging to this
	// logical HTTP exchange. A later pre-send failure cannot erase an earlier
	// sent or ambiguous request. No attempt has started at entry.
	state := model.SendNotSent
	defer func() {
		if err != nil {
			err = &OpenAITransportAttemptError{Cause: err, SendState: state}
		}
	}()
	if request == nil || account == nil {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	binder, ok := o.authority.leases.(nativeDispatchLeaseBinder)
	if !ok {
		return nil, ErrNativeDispatchLeaseUnproven
	}
	ctx, detach, err := binder.BindNativeDispatchLeases(request.Context(), model.PreparedIdentity{UserID: o.request.UserID, ResourceID: account.ID})
	if err != nil {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, nativeDispatchError("capacity lifetime", err)
	}
	transferred := false
	defer func() {
		if !transferred {
			detach()
		}
	}()
	request = request.WithContext(ctx)
	prepared, target, err := o.prepare(request, account, proxy, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !transferred {
			_ = prepared.Close()
		}
	}()
	retries := 0
	if prepared.Identity().BodyKind == model.BodyReplayable {
		response, err = o.followHTTPRedirects(ctx, prepared, target, account, proxy, &retries, &state)
	} else {
		// An opaque body has no second reader or automatic replay contract.
		response, err = o.sendPrepared(ctx, prepared, target, &retries, &state)
	}
	if err != nil {
		return nil, err
	}
	if response.Body != nil {
		response.Body = &nativeDispatchResponseBody{ReadCloser: response.Body, detach: detach}
		transferred = true
	}
	return response, nil
}

// The standard HTTP controller retains method/header/relative-URL redirect
// semantics, but its transport is this operation's admission boundary. Every
// hop is prepared and accounted for, including a bodyless 303 continuation.
func (o *nativeDispatchOperation) followHTTPRedirects(ctx context.Context, first *native.PreparedHTTP, target model.AttemptTarget, account *Account, proxy string, retries *int, state *model.SendState) (*http.Response, error) {
	initial, err := native.HTTPControlRequest(ctx, first)
	if err != nil {
		return nil, err
	}
	if initial.Body != nil {
		defer initial.Body.Close()
	}
	approved := make(map[*http.Request]bool)
	firstCall := true
	client := &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if err := o.transport.ValidateNativeRedirect(request, via, proxy); err != nil {
			return err
		}
		approved[request] = true
		return nil
	}}
	client.Transport = nativeAdmissionRoundTripper(func(request *http.Request) (*http.Response, error) {
		var prepared *native.PreparedHTTP
		currentTarget := target
		if firstCall {
			firstCall = false
			prepared = first
			// This is a second reader over the same sealed bytes, supplied only
			// for the controller's replay bookkeeping; Open owns the first one.
			if request.Body != nil {
				_ = request.Body.Close()
			}
		} else {
			if !approved[request] {
				if request.Body != nil {
					_ = request.Body.Close()
				}
				return nil, execution.ErrInvalidPreparedIdentity
			}
			delete(approved, request)
			var err error
			prepared, currentTarget, err = o.prepare(request, account, proxy, request.Header.Get("Authorization") == "")
			if err != nil {
				return nil, err
			}
			currentTarget.TransportContinuation = true
		}
		response, err := o.sendPrepared(request.Context(), prepared, currentTarget, retries, state)
		if err != nil {
			_ = prepared.Close()
			return nil, err
		}
		if response.Request == nil {
			response.Request = request
		}
		return response, nil
	})
	return client.Do(initial)
}

type nativeAdmissionRoundTripper func(*http.Request) (*http.Response, error)

func (f nativeAdmissionRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// Sent records at least one written request; ambiguous/unknown facts prevent
// the complete exchange from becoming NotSent after a later failed hop.
func combineNativeSendState(previous, next model.SendState) model.SendState {
	switch {
	case previous == model.SendSent || next == model.SendSent:
		return model.SendSent
	case previous == model.SendAmbiguous || next == model.SendAmbiguous:
		return model.SendAmbiguous
	case previous == model.SendNotSent && next == model.SendNotSent:
		return model.SendNotSent
	default:
		return model.SendUnknown
	}
}

type nativeDispatchResponseBody struct {
	io.ReadCloser
	detach context.CancelFunc
	once   sync.Once
	err    error
}

func (b *nativeDispatchResponseBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close(); b.detach() })
	return b.err
}

func (b *nativeDispatchResponseBody) MarkComplete() {
	if complete, ok := b.ReadCloser.(interface{ MarkComplete() }); ok {
		complete.MarkComplete()
	}
}

func nativeDispatchBearerAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && !account.IsExcelOAuth() &&
		!account.IsOpenAICloudConfigured() && !account.IsOpenAIAgentIdentity() &&
		(account.Type == AccountTypeAPIKey || account.IsOpenAIOAuthLike())
}

func IsNativeDispatchDenied(err error) bool {
	return errors.Is(err, ErrNativeDispatchDenied) || errors.Is(err, execution.ErrDispatchUnavailable) ||
		errors.Is(err, execution.ErrSendPermitConsumed) ||
		errors.Is(err, execution.ErrDispatchChanged) || errors.Is(err, execution.ErrInvalidPreparedIdentity) ||
		errors.Is(err, execution.ErrDispatchExpired) || errors.Is(err, execution.ErrDispatchOutputCommitted) ||
		errors.Is(err, ErrNativeDispatchLeaseUnproven) || errors.Is(err, routing.ErrCapacityLeaseLost) ||
		errors.Is(err, routing.ErrCapacityPolicyChanged) || errors.Is(err, routing.ErrCapacityDependencyUnavailable)
}
