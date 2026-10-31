package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

var (
	// ErrNativeDispatchDenied classifies local authorization/dependency failures.
	// Callers must not count these as upstream health failures or retry on another
	// account without a new authorized dispatch. errors.Is retains the cause.
	ErrNativeDispatchDenied        = errors.New("native dispatch authorization denied")
	ErrNativeDispatchGrantUnproven = errors.New("native credential generation cannot be proven")
	ErrNativeDispatchScopeChanged  = errors.New("native dispatch principal or scope changed")
	ErrNativeDispatchLeaseUnproven = errors.New("native dispatch lease cannot be proven")
)

// NativeDispatchRequest is frozen by the authenticated operation root, never
// decoded from provider/client assertions. APIKeyDigest hashes the actual
// authenticated downstream credential. ClientIP must come from the existing
// security IP resolver with configured proxy trust, not a raw forwarded header.
// RequestedModel is the ORIGINAL client model, before catalog/provider mapping.
// GroupBound preserves nil versus explicit zero. SimpleMode is boot config.
// Transport binds the actual selected native transport (currently http or ws).
type NativeDispatchRequest struct {
	OperationID                         string
	UserID, APIKeyID                    int64
	APIKeyDigest                        [32]byte
	GroupID                             int64
	GroupBound                          bool
	RequestedModel, ClientIP, Transport string
	SimpleMode                          bool
}

// NativeDispatchLeaseProof is a live proof of the operation's actual owned
// capacity/owner tokens. Never manufacture a token, epoch, or now+TTL. MAIN's
// routing adapter must verify every required user/key/resource lease and return
// their fingerprint and earliest real deadline. Explicit unlimited capacity
// requires a freshly verified policy fingerprint, but has no capacity deadline.
// A zero ValidUntil adds no deadline beyond key/grant/operation lifetime; it is
// never a substitute for proof. Missing verifiers and zero fingerprints deny.
type NativeDispatchLeaseProof struct {
	Fingerprint [32]byte
	ValidUntil  time.Time
}

type NativeDispatchLeaseVerifier interface {
	RevalidateNativeDispatchLeases(context.Context, model.PreparedIdentity) (NativeDispatchLeaseProof, error)
}

// NativeDispatchTokenSource is implemented by the existing OpenAITokenProvider.
// A cache/provider token is accepted only after matching a freshly reloaded
// credential owner and its durable expiry or existing PAT optional-expiry
// contract. This port cannot authorize sends.
type NativeDispatchTokenSource interface {
	GetAccessToken(context.Context, *Account) (string, error)
}

type NativeDispatchSubscriptionReader interface {
	GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error)
}

type nativeDispatchRepositoryChecks interface {
	ValidateVendorSupply(context.Context, *Account, *int64) error
	ValidateVendorAuthorizationMode(context.Context, *Account, *int64, string) error
}

// NativeDispatchAuthority is the application boundary for one native operation.
// It owns no cached authorization, billing/RPM writes, leases, or goroutines.
// MAIN creates it after authentication/model parsing, supplies the real routing
// lease verifier, calls ResolveCredential during provider preparation, then
// installs it in execution.NewDispatcher with the existing budget/clock. Only
// Dispatcher.Admit's concrete permit goes to provider.Open. No production
// handler/Wire integration is implied by constructing this boundary.
type NativeDispatchAuthority struct {
	keys          *APIKeyService
	accounts      AccountRepository
	checks        nativeDispatchRepositoryChecks
	subscriptions NativeDispatchSubscriptionReader
	tokens        NativeDispatchTokenSource
	leases        NativeDispatchLeaseVerifier
	request       NativeDispatchRequest
	now           func() time.Time
}

var _ execution.DispatchAuthority = (*NativeDispatchAuthority)(nil)
var _ NativeDispatchTokenSource = (*OpenAITokenProvider)(nil)
var _ NativeDispatchSubscriptionReader = (UserSubscriptionRepository)(nil)

func NewNativeDispatchAuthority(keys *APIKeyService, accounts AccountRepository, subscriptions NativeDispatchSubscriptionReader, tokens NativeDispatchTokenSource, leases NativeDispatchLeaseVerifier, request NativeDispatchRequest) (*NativeDispatchAuthority, error) {
	if keys == nil || nativeDispatchNil(keys.apiKeyRepo) || nativeDispatchNil(accounts) || nativeDispatchNil(leases) || (!request.SimpleMode && nativeDispatchNil(subscriptions)) {
		return nil, execution.ErrDispatchUnavailable
	}
	checks, ok := accounts.(nativeDispatchRepositoryChecks)
	if !ok || nativeDispatchNil(checks) {
		return nil, execution.ErrDispatchUnavailable
	}
	address := net.ParseIP(request.ClientIP)
	if request.OperationID == "" || request.UserID <= 0 || request.APIKeyID <= 0 || request.APIKeyDigest == ([32]byte{}) || strings.TrimSpace(request.RequestedModel) == "" || address == nil || (!request.GroupBound && request.GroupID != 0) || (request.Transport != "http" && request.Transport != "ws") {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	request.ClientIP = address.String()
	return &NativeDispatchAuthority{keys: keys, accounts: accounts, checks: checks, subscriptions: subscriptions, tokens: tokens, leases: leases, request: request, now: time.Now}, nil
}

func nativeDispatchNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// Denial provenance is assigned by the live authority, never inferred from
// an error message or a provider code. A resource change may share the same
// cause as a principal change, but must not revoke the client connection.
type nativeDispatchDenial struct {
	stage     string
	cause     error
	principal bool
}

func (e *nativeDispatchDenial) Error() string {
	return fmt.Sprintf("%s: %s: %v", ErrNativeDispatchDenied, e.stage, e.cause)
}
func (e *nativeDispatchDenial) Unwrap() error        { return e.cause }
func (e *nativeDispatchDenial) Is(target error) bool { return target == ErrNativeDispatchDenied }

// IsNativeDispatchPrincipalFailure identifies the authoritative lookup/policy
// boundary. Callers still distinguish a proven rejection from a dependency
// failure by its typed cause; this flag alone never makes an outage a 403.
func IsNativeDispatchPrincipalFailure(err error) bool {
	var denied *nativeDispatchDenial
	return errors.As(err, &denied) && denied.principal
}
func nativeDispatchError(stage string, err error) *nativeDispatchDenial {
	return &nativeDispatchDenial{stage: stage, cause: err, principal: IsNativeDispatchPrincipalFailure(err)}
}
func nativeDispatchPrincipalError(stage string, err error) error {
	denied := nativeDispatchError(stage, err)
	denied.principal = true
	return denied
}

func (a *NativeDispatchAuthority) ready() bool {
	return a != nil && a.keys != nil && !nativeDispatchNil(a.keys.apiKeyRepo) && !nativeDispatchNil(a.accounts) && !nativeDispatchNil(a.checks) && !nativeDispatchNil(a.leases) && a.now != nil
}

func (a *NativeDispatchAuthority) groupID() *int64 {
	if !a.request.GroupBound {
		return nil
	}
	id := a.request.GroupID
	return &id
}

func (a *NativeDispatchAuthority) loadKey(ctx context.Context) (*APIKey, *UserSubscription, time.Time, error) {
	key, err := a.keys.GetByID(ctx, a.request.APIKeyID)
	if err != nil {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key lookup", err)
	}
	if key == nil {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key lookup", ErrAPIKeyNotFound)
	}
	actor := access.KeyActor{ExpectedKeyID: a.request.APIKeyID, ExpectedUserID: a.request.UserID, KeyID: key.ID, KeyUserID: key.UserID}
	if key.User != nil {
		actor.UserPresent, actor.UserID, actor.UserStatus = true, key.User.ID, key.User.Status
		actor.UserDeleted, actor.Vendor = key.User.DeletedAt != nil, key.User.IsVendor()
	}
	if err := access.ValidateKeyActor(actor); err != nil {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key actor", err)
	}
	if !access.KeyStatusPermitsAuthentication(key.Status) {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key status", access.ErrKeyDisabled)
	}
	if key.Key == "" || sha256.Sum256([]byte(key.Key)) != a.request.APIKeyDigest || (key.GroupID != nil) != a.request.GroupBound || (key.GroupID != nil && *key.GroupID != a.request.GroupID) {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key scope", ErrNativeDispatchScopeChanged)
	}
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		if (len(key.IPWhitelist) > 0 && key.CompiledIPWhitelist == nil) || (len(key.IPBlacklist) > 0 && key.CompiledIPBlacklist == nil) {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("IP rules", execution.ErrDispatchUnavailable)
		}
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(a.request.ClientIP, key.CompiledIPWhitelist, key.CompiledIPBlacklist)
		if !allowed {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("IP restriction", ErrNativeDispatchScopeChanged)
		}
	}
	var group *access.Group
	if key.Group != nil {
		group = &access.Group{ID: key.Group.ID, Status: key.Group.Status, IsExclusive: key.Group.IsExclusive, Subscription: key.Group.IsSubscriptionType()}
	}
	if access.APIKeyGroupAvailable(a.request.GroupBound, group) != access.GroupAvailable {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("group unavailable", ErrGroupNotAllowed)
	}
	if a.request.GroupBound && key.Group.ID != a.request.GroupID {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("group edge", ErrNativeDispatchScopeChanged)
	}
	binding := access.GroupBinding{Vendor: key.User.IsVendor(), RestrictPublicGroups: key.User.RestrictPublicGroups, AllowedGroups: key.User.AllowedGroups}
	if !access.APIKeyGroupAllowed(a.request.GroupBound, &binding, group) {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("group grant", ErrGroupNotAllowed)
	}
	if a.request.GroupBound && !key.Group.ModelAllowlist.Allows(a.request.RequestedModel) {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("model allowlist", ErrGroupNotAllowed)
	}
	now := a.now()
	if err := access.KeyQuotaError(access.KeyState{Status: key.Status, ExpiresAt: key.ExpiresAt, Quota: key.Quota, QuotaUsed: key.QuotaUsed}, a.request.SimpleMode, now); err != nil {
		return nil, nil, time.Time{}, nativeDispatchPrincipalError("key quota", err)
	}
	var deadline time.Time
	if !a.request.SimpleMode && key.ExpiresAt != nil {
		deadline = *key.ExpiresAt
	}
	var sub *UserSubscription
	if !a.request.SimpleMode && a.request.GroupBound && key.Group.IsSubscriptionType() {
		if nativeDispatchNil(a.subscriptions) {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("subscription authority", execution.ErrDispatchUnavailable)
		}
		sub, err = a.subscriptions.GetActiveByUserIDAndGroupID(ctx, a.request.UserID, a.request.GroupID)
		if err != nil {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("subscription lookup", err)
		}
		if sub == nil || sub.ID <= 0 || sub.UserID != a.request.UserID || sub.GroupID != a.request.GroupID || sub.DeletedAt != nil || sub.Status != SubscriptionStatusActive || !now.Before(sub.ExpiresAt) {
			return nil, nil, time.Time{}, nativeDispatchPrincipalError("subscription revoked", ErrGroupNotAllowed)
		}
		deadline = nativeDispatchEarlier(deadline, sub.ExpiresAt)
	}
	return key, sub, deadline, nil
}

func (a *NativeDispatchAuthority) loadResource(ctx context.Context, id int64) (*Account, *Account, error) {
	selected, err := a.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, nil, nativeDispatchError("resource lookup", err)
	}
	if selected == nil || selected.ID != id || id <= 0 || selected.Platform != PlatformOpenAI || !selected.IsSchedulable() {
		return nil, nil, nativeDispatchError("resource eligibility", ErrAccountNotFound)
	}
	// Preserve openAIAccountMatchesSchedulingGroup's configured simple-mode
	// full-pool contract. This skips only resource membership, not the fresh
	// key/group/model checks or vendor ownership/mode checks against the group.
	if !a.request.SimpleMode && !access.ResourceInGroup(a.groupID(), selected.GroupIDs) {
		return nil, nil, nativeDispatchError("resource membership", ErrAccountNotFound)
	}
	if selected.IsExcelOAuth() || selected.IsOpenAICloudConfigured() {
		return nil, nil, nativeDispatchError("resource requires another mode", ErrNativeDispatchGrantUnproven)
	}
	if err := a.checks.ValidateVendorSupply(ctx, selected, a.groupID()); err != nil {
		return nil, nil, nativeDispatchError("resource ownership", err)
	}
	if err := a.checks.ValidateVendorAuthorizationMode(ctx, selected, a.groupID(), VendorOpenAIModeNative); err != nil {
		return nil, nil, nativeDispatchError("native mode", err)
	}
	owner, err := resolveCredentialAccount(ctx, a.accounts, selected)
	if err != nil {
		return nil, nil, nativeDispatchError("credential owner", err)
	}
	if owner == nil || owner.ID != recoveryOwner(selected).AccountID || !owner.IsActive() || owner.IsVendorArchived() || owner.VendorPausedAt != nil || !SameProxyOwner(selected.VendorID, owner.VendorID) {
		return nil, nil, nativeDispatchError("credential owner changed", ErrNativeDispatchScopeChanged)
	}
	if owner.ID != selected.ID {
		// A shadow owns its scheduling lane, but shares the parent's credential
		// and transport health. Reuse the scheduler's exact parent predicate:
		// parent global rate limits and manual scheduling do not block spark.
		if !owner.IsCredentialUsableForShadow() {
			return nil, nil, nativeDispatchError("parent credential unavailable", ErrNativeDispatchScopeChanged)
		}
		if err := a.checks.ValidateVendorSupply(ctx, owner, nil); err != nil {
			return nil, nil, nativeDispatchError("parent ownership", err)
		}
		if err := a.checks.ValidateVendorAuthorizationMode(ctx, owner, nil, VendorOpenAIModeNative); err != nil {
			return nil, nil, nativeDispatchError("parent mode", err)
		}
	}
	return selected, owner, nil
}

func (a *NativeDispatchAuthority) RevalidateDispatch(ctx context.Context, prepared model.PreparedIdentity) (execution.DispatchValidation, error) {
	if a == nil {
		return execution.DispatchValidation{}, execution.ErrDispatchUnavailable
	}
	return a.revalidatePreparedTransport(ctx, prepared, a.request.Transport, nil)
}

// nativeDispatchResourceCheck consumes only the authority-owned read from this gate.
// Implementations cannot substitute a cached Account or suppress live eligibility checks.
type nativeDispatchResourceCheck func(context.Context, *Account) error

// revalidatePreparedTransport checks the sealed upstream transport without
// rewriting the immutable client ingress or its authorization fingerprint.
// Cross-transport callers must first prove registration in the same operation.
func (a *NativeDispatchAuthority) revalidatePreparedTransport(ctx context.Context, prepared model.PreparedIdentity, upstreamTransport string, check nativeDispatchResourceCheck) (execution.DispatchValidation, error) {
	var denied execution.DispatchValidation
	if !a.ready() || ctx == nil {
		return denied, execution.ErrDispatchUnavailable
	}
	if err := ctx.Err(); err != nil {
		return denied, context.Cause(ctx)
	}
	r := a.request
	if !prepared.Valid() || prepared.OperationID != r.OperationID || prepared.UserID != r.UserID || prepared.APIKeyID != r.APIKeyID || prepared.GroupBound != r.GroupBound || prepared.GroupID != r.GroupID || prepared.Mode != VendorOpenAIModeNative || prepared.Transport != upstreamTransport {
		return denied, nativeDispatchError("prepared identity", execution.ErrInvalidPreparedIdentity)
	}
	key, sub, deadline, err := a.loadKey(ctx)
	if err != nil {
		return denied, err
	}
	resource, owner, err := a.loadResource(ctx, prepared.ResourceID)
	if err != nil {
		return denied, err
	}
	if check != nil {
		if err := check(ctx, resource); err != nil {
			return denied, err
		}
	}
	if recoveryOwner(resource) != prepared.Owner || recoveryOwner(owner) != prepared.Owner {
		return denied, nativeDispatchError("prepared owner", ErrNativeDispatchScopeChanged)
	}
	grant, err := a.resolveCredential(ctx, owner, false)
	if err != nil {
		return denied, err
	}
	if grant.GrantDigest != prepared.GrantDigest || grant.Owner != prepared.Owner {
		return denied, nativeDispatchError("prepared credential", ErrNativeDispatchGrantUnproven)
	}
	leases, err := a.leases.RevalidateNativeDispatchLeases(ctx, prepared)
	if err != nil {
		return denied, nativeDispatchError("leases", err)
	}
	if leases.Fingerprint == ([32]byte{}) || (!leases.ValidUntil.IsZero() && !a.now().Before(leases.ValidUntil)) {
		return denied, nativeDispatchError("leases", ErrNativeDispatchLeaseUnproven)
	}
	deadline = nativeDispatchEarlier(deadline, nativeDispatchEarlier(grant.ValidUntil, leases.ValidUntil))
	if resource.AutoPauseOnExpired && resource.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *resource.ExpiresAt)
	}
	if owner.AutoPauseOnExpired && owner.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *owner.ExpiresAt)
	}
	if err := ctx.Err(); err != nil {
		return denied, context.Cause(ctx)
	}
	if !deadline.IsZero() && !a.now().Before(deadline) {
		return denied, nativeDispatchError("deadline", execution.ErrDispatchExpired)
	}
	fingerprint, err := a.authorizationFingerprint(key, sub, resource, grant)
	if err != nil {
		return denied, nativeDispatchError("authorization fingerprint", err)
	}
	return execution.DispatchValidation{Authorization: fingerprint, Leases: leases.Fingerprint, ValidUntil: deadline}, nil
}

func (a *NativeDispatchAuthority) authorizationFingerprint(key *APIKey, sub *UserSubscription, resource *Account, grant NativeDispatchCredential) ([32]byte, error) {
	grants := slices.Clone(key.User.AllowedGroups)
	slices.Sort(grants)
	var memberships []int64
	if !a.request.SimpleMode {
		memberships = slices.Clone(resource.GroupIDs)
		slices.Sort(memberships)
	}
	var group any
	if a.request.GroupBound {
		group = []any{key.Group.ID, key.Group.Status, key.Group.IsExclusive, key.Group.SubscriptionType, key.Group.ModelAllowlist}
	}
	var subscription any
	if sub != nil {
		subscription = []any{sub.ID, sub.UserID, sub.GroupID, sub.Status, sub.ExpiresAt}
	}
	var proxy any
	if resource.ProxyID != nil {
		if resource.Proxy == nil || resource.Proxy.ID != *resource.ProxyID {
			return [32]byte{}, ErrNativeDispatchScopeChanged
		}
		proxy = []any{*resource.ProxyID, resource.Proxy.ConfigVersion, resource.Proxy.VendorID}
	}
	// Mutable spend counters are rechecked, not hashed: unrelated authorized
	// usage below the limit must not invalidate an otherwise unchanged permit.
	raw, err := json.Marshal([]any{"native-dispatch-v1", a.request, key.ID, key.UserID, key.Status, key.ExpiresAt, key.Quota, key.IPWhitelist, key.IPBlacklist, key.User.ID, key.User.Role, key.User.Status, key.User.RestrictPublicGroups, grants, group, subscription, resource.ID, resource.VendorID, resource.ParentAccountID, memberships, proxy, grant.GrantDigest})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func nativeDispatchEarlier(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}
