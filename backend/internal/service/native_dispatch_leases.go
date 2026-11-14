package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

// NativeCapacityLookup exposes the actual acquisitions for one operation. It
// owns no second semaphore, renewal worker or synthetic lease. Implementations
// must not substitute telemetry-only API-key counters for admission capacity.
type NativeCapacityLookup interface {
	Lookup(user bool, id int64) *routing.AcquireResult
}

type nativeDispatchLeaseBinder interface {
	BindNativeDispatchLeases(context.Context, model.PreparedIdentity) (context.Context, context.CancelFunc, error)
}

type nativeBoundCapacityKey struct{}
type nativeBoundCapacity struct{ user, account *routing.AcquireResult }

type nativeDispatchCapacity struct {
	users    UserRepository
	accounts AccountRepository
	settings *SettingService
	scope    NativeCapacityLookup
}

// NativeDispatchCapacity joins the existing user/account policy owners with
// the acquired routing tokens. Current limits are recomputed using the same
// scheduler policy, including quota protection and explicit unlimited capacity.
func (s *OpenAIGatewayService) NativeDispatchCapacity(scope NativeCapacityLookup) (NativeDispatchLeaseVerifier, error) {
	if s == nil || nativeDispatchNil(scope) || nativeDispatchNil(s.userRepo) || nativeDispatchNil(s.accountRepo) {
		return nil, ErrNativeDispatchLeaseUnproven
	}
	return &nativeDispatchCapacity{users: s.userRepo, accounts: s.accountRepo, settings: s.settingService, scope: scope}, nil
}

func (l *nativeDispatchCapacity) acquisitions(identity model.PreparedIdentity) (*routing.AcquireResult, *routing.AcquireResult, error) {
	if identity.UserID <= 0 || identity.ResourceID <= 0 {
		return nil, nil, ErrNativeDispatchLeaseUnproven
	}
	user, account := l.scope.Lookup(true, identity.UserID), l.scope.Lookup(false, identity.ResourceID)
	if user == nil || account == nil || !user.Acquired || !account.Acquired ||
		user.Policy().ResourceID != identity.UserID || !user.Policy().User ||
		account.Policy().ResourceID != identity.ResourceID || account.Policy().User {
		return nil, nil, ErrNativeDispatchLeaseUnproven
	}
	return user, account, nil
}

func (l *nativeDispatchCapacity) RevalidateNativeDispatchLeases(ctx context.Context, identity model.PreparedIdentity) (NativeDispatchLeaseProof, error) {
	var denied NativeDispatchLeaseProof
	if ctx == nil {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	userLease, accountLease, err := l.acquisitions(identity)
	if err != nil {
		return denied, err
	}
	if bound, ok := ctx.Value(nativeBoundCapacityKey{}).(nativeBoundCapacity); ok &&
		(bound.user != userLease || bound.account != accountLease) {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	user, err := l.users.GetByID(ctx, identity.UserID)
	if err != nil {
		return denied, err
	}
	account, err := l.accounts.GetByID(ctx, identity.ResourceID)
	if err != nil {
		return denied, err
	}
	if user == nil || user.ID != identity.UserID || user.DeletedAt != nil || user.Status != StatusActive || account == nil || account.ID != identity.ResourceID {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	userEvidence, err := userLease.Verify(ctx, routing.CapacityPolicy{ResourceID: user.ID, User: true, Limit: user.Concurrency})
	if err != nil {
		return denied, err
	}
	accountEvidence, err := accountLease.Verify(ctx, routing.CapacityPolicy{ResourceID: account.ID, Limit: nativeSchedulingConcurrency(ctx, account, l.settings)})
	if err != nil {
		return denied, err
	}
	u, a := userEvidence.Fingerprint(), accountEvidence.Fingerprint()
	if u == ([32]byte{}) || a == ([32]byte{}) {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("sub2api-native-user-account-capacity-v1"))
	_, _ = hash.Write(u[:])
	_, _ = hash.Write(a[:])
	var fingerprint [32]byte
	copy(fingerprint[:], hash.Sum(nil))
	return NativeDispatchLeaseProof{Fingerprint: fingerprint, ValidUntil: nativeDispatchEarlier(userEvidence.ValidUntil(), accountEvidence.ValidUntil())}, nil
}

// Bind links only the selected account and the operation's user to this send.
// Releasing another rejected scheduler candidate cannot cancel this attempt.
// Detaching cancels the attempt context, while the ingress retains ownership of
// ReleaseFunc. Each lease still has exactly one original renewal/release owner.
func (l *nativeDispatchCapacity) BindNativeDispatchLeases(parent context.Context, identity model.PreparedIdentity) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, ErrNativeDispatchLeaseUnproven
	}
	user, account, err := l.acquisitions(identity)
	if err != nil {
		return nil, nil, err
	}
	ctx := parent
	var stops []context.CancelFunc
	var once sync.Once
	stop := func() {
		once.Do(func() {
			for i := len(stops) - 1; i >= 0; i-- {
				stops[i]()
			}
		})
	}
	for _, result := range []*routing.AcquireResult{user, account} {
		if result.Unlimited {
			if result.Lease != nil || result.Policy().Limit > 0 {
				stop()
				return nil, nil, ErrNativeDispatchLeaseUnproven
			}
			continue
		}
		if result.Lease == nil {
			stop()
			return nil, nil, ErrNativeDispatchLeaseUnproven
		}
		bound, detach := result.Lease.Bind(ctx)
		ctx = bound
		stops = append(stops, detach)
		if err := ctx.Err(); err != nil {
			stop()
			return nil, nil, errors.Join(ErrNativeDispatchLeaseUnproven, context.Cause(ctx))
		}
	}
	return context.WithValue(ctx, nativeBoundCapacityKey{}, nativeBoundCapacity{user: user, account: account}), stop, nil
}
