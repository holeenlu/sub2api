package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

type cloudResponseAuthority struct {
	gateway  *OpenAIGatewayService
	store    OpenAICloudExecutionStore
	claim    CloudExecutionClaim
	owner    OpenAICloudOwner
	handoff  ResponseModeHandoff
	binding  model.ModeBinding
	mu       sync.Mutex
	prepared map[model.PreparedIdentity]bool
}

func cloudExecutionHash(value any) [32]byte { raw, _ := json.Marshal(value); return sha256.Sum256(raw) }
func cloudExecutionGrant(owner OpenAICloudOwner) [32]byte {
	return cloudExecutionHash([]any{"cloud-native-generation-v1", owner.AccountID, owner.GrantGeneration, owner.SettingsDigest})
}

func (s *OpenAIGatewayService) cloudExecutionKeys() *APIKeyService {
	s.cloudBillingMu.Lock()
	defer s.cloudBillingMu.Unlock()
	keys, _ := s.cloudBillingQuotaUpdater.(*APIKeyService)
	return keys
}

func (a *cloudResponseAuthority) ClaimResponseModeOperation(ctx context.Context, h ResponseModeHandoff) (execution.TransferClaim, error) {
	if !h.valid() || h.Execution.Digest() != a.claim.TransferDigest || h.Execution.CommandID != a.claim.CommandID {
		return execution.TransferClaim{}, execution.ErrDispatchChanged
	}
	if _, err := a.revalidateScope(ctx, a.commandIdentity(), false); err != nil {
		return execution.TransferClaim{}, err
	}
	started := time.Now()
	remaining, err := a.store.ClaimCloudExecution(ctx, a.claim)
	if err != nil {
		return execution.TransferClaim{}, err
	}
	return execution.TransferClaim{OperationID: a.claim.OperationID, CommandID: a.claim.CommandID, Digest: a.claim.TransferDigest, SessionID: a.claim.SessionID, OwnerEpoch: sha256.Sum256([]byte(a.claim.OwnerToken)), Leases: cloudExecutionHash(a.claim), ValidUntil: started.Add(remaining)}, nil
}

func (a *cloudResponseAuthority) RevalidateDispatch(ctx context.Context, i model.PreparedIdentity) (execution.DispatchValidation, error) {
	var denied execution.DispatchValidation
	if ctx == nil || a.gateway == nil || a.store == nil {
		return denied, execution.ErrDispatchUnavailable
	}
	a.mu.Lock()
	registered := a.prepared[i]
	a.mu.Unlock()
	purpose, valid := i.Role.Purpose()
	if !registered || !valid || purpose != i.Purpose || i.Binding != a.binding || i.Mode != UsageAuthorizationModeCloud || i.Transport != "ws" || i.ResourceID != a.owner.AccountID || i.GrantDigest != cloudExecutionGrant(a.owner) {
		return denied, execution.ErrInvalidPreparedIdentity
	}
	return a.revalidateScope(ctx, i, true)
}

func (a *cloudResponseAuthority) commandIdentity() model.PreparedIdentity {
	r := a.handoff.Execution.Operation
	return model.PreparedIdentity{OperationID: r.OperationID, UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound, Principal: r.Principal, ProbeID: r.ProbeID, ProbeClaimDigest: r.ProbeClaimDigest, ResourceID: a.owner.AccountID, Owner: model.ResourceOwner{AccountID: a.owner.AccountID}, Mode: UsageAuthorizationModeCloud, Transport: "ws", Binding: a.binding, GrantDigest: cloudExecutionGrant(a.owner)}
}

// Command admission and cached replay share the real principal/capacity checks
// with dispatch. They do not manufacture a prepared seal or issue a permit.
func (a *cloudResponseAuthority) revalidateCommand(ctx context.Context) error {
	_, err := a.revalidateScope(ctx, a.commandIdentity(), true)
	return err
}

func (a *cloudResponseAuthority) revalidateScope(ctx context.Context, i model.PreparedIdentity, checkClaim bool) (execution.DispatchValidation, error) {
	var denied execution.DispatchValidation
	if ctx == nil || a.gateway == nil || a.store == nil || a.gateway.accountRepo == nil {
		return denied, execution.ErrDispatchUnavailable
	}
	if err := ctx.Err(); err != nil {
		return denied, context.Cause(ctx)
	}
	s := a.gateway
	if err := s.validateCloudSessionOwner(ctx, a.owner); err != nil {
		return denied, err
	}
	resource, err := s.accountRepo.GetByID(ctx, a.owner.AccountID)
	if err != nil {
		return denied, err
	}
	if resource == nil || !resource.IsOpenAICloudEligible() || resource.VendorID != nil || resource.IsShadow() || recoveryOwner(resource) != i.Owner || cloudSettingsDigest(resource) != a.owner.SettingsDigest {
		return denied, ErrNativeDispatchScopeChanged
	}
	if err := s.settingService.checkManagedAccountModels(ctx, resource, a.handoff.Request.RequestedModel, a.owner.Model); err != nil {
		return denied, nativeDispatchError("Cloud managed model policy", err)
	}
	var auth [32]byte
	var deadline time.Time
	var capacity NativeDispatchLeaseProof
	if a.owner.Probe {
		if i.Principal != model.PrincipalAdministratorProbe || i.APIKeyID != 0 || i.ProbeID != a.handoff.Execution.Operation.ProbeID || i.ProbeClaimDigest != a.handoff.Execution.Operation.ProbeClaimDigest || s.userRepo == nil {
			return denied, execution.ErrDispatchUnavailable
		}
		admin, err := s.userRepo.GetByID(ctx, a.owner.UserID)
		if err != nil {
			return denied, err
		}
		if admin == nil || !admin.IsStaff() || admin.Status != StatusActive || admin.DeletedAt != nil {
			return denied, ErrNativeDispatchScopeChanged
		}
		auth = cloudExecutionHash([]any{"cloud-admin-probe", admin.ID, admin.Role, admin.Status, i.ProbeID, i.ProbeClaimDigest, a.owner})
	} else {
		keys := s.cloudExecutionKeys()
		if keys == nil {
			return denied, execution.ErrDispatchUnavailable
		}
		principal := &NativeDispatchAuthority{keys: keys, subscriptions: s.userSubRepo, request: a.handoff.Request, now: time.Now}
		key, sub, until, err := principal.loadKey(ctx)
		if err != nil {
			return denied, err
		}
		deadline = until
		if i.UserID != key.UserID || i.APIKeyID != key.ID || i.Principal != model.PrincipalCustomer {
			return denied, ErrNativeDispatchScopeChanged
		}
		if !a.handoff.Request.SimpleMode && !access.ResourceInGroup(principal.groupID(), resource.GroupIDs) {
			return denied, ErrNativeDispatchScopeChanged
		}
		checks, ok := s.accountRepo.(nativeDispatchRepositoryChecks)
		if !ok {
			return denied, execution.ErrDispatchUnavailable
		}
		if err := checks.ValidateVendorSupply(ctx, resource, principal.groupID()); err != nil {
			return denied, err
		}
		// Cloud is restricted to ordinary non-vendor native OAuth accounts.
		// It does not add a third supplier mode or expand BPS-only supply.
		if err := checks.ValidateVendorAuthorizationMode(ctx, resource, principal.groupID(), VendorOpenAIModeNative); err != nil {
			return denied, err
		}
		if s.userRepo == nil || s.concurrencyService == nil {
			return denied, ErrNativeDispatchLeaseUnproven
		}
		user, err := s.userRepo.GetByID(ctx, key.UserID)
		if err != nil {
			return denied, err
		}
		if user == nil || user.DeletedAt != nil || user.Status != StatusActive {
			return denied, ErrNativeDispatchScopeChanged
		}
		leases, ok := s.concurrencyService.cache.(routing.CapacityLeaseStore)
		if !ok {
			return denied, ErrNativeDispatchLeaseUnproven
		}
		capacityCtx := ctx
		if a.handoff.CapacityQuotaChecked {
			// This projection is private to the capacity-policy calculation; it
			// is not forwarded or used as input-token estimation evidence.
			policyBody, _ := json.Marshal(map[string]string{"model": a.handoff.CapacityModel})
			capacityCtx = WithSchedulingQuotaRequest(ctx, policyBody, ContentModerationProtocolOpenAIResponses)
		}
		capacity, err = a.handoff.VerifyCapacity(ctx, leases, user.Concurrency, nativeSchedulingConcurrency(capacityCtx, resource, s.settingService))
		if err != nil {
			return denied, err
		}
		auth, err = principal.authorizationFingerprint(key, sub, resource, NativeDispatchCredential{Owner: i.Owner, GrantDigest: i.GrantDigest})
		if err != nil {
			return denied, err
		}
	}
	deadline = nativeDispatchEarlier(deadline, capacity.ValidUntil)
	if checkClaim {
		started := time.Now()
		remaining, err := a.store.CheckCloudExecution(ctx, a.claim)
		if err != nil {
			return denied, err
		}
		if remaining <= 0 {
			return denied, execution.ErrDispatchExpired
		}
		deadline = nativeDispatchEarlier(deadline, started.Add(remaining))
	}
	if resource.AutoPauseOnExpired && resource.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *resource.ExpiresAt)
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return denied, execution.ErrDispatchExpired
	}
	return execution.DispatchValidation{Purpose: i.Purpose, Authorization: cloudExecutionHash([]any{auth, a.owner, i.Binding}), Leases: cloudExecutionHash([]any{capacity.Fingerprint, a.claim}), ValidUntil: deadline}, nil
}
