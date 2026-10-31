package routing

import (
	"context"
	"errors"
	"time"
)

// LiveLeaseStore retains the existing three Live ZSETs and shared token. The
// separate controller store remains authoritative for which socket may control
// a call; capacity ownership never manufactures a controller epoch.
type LiveLeaseStore interface {
	AcquireLiveLease(context.Context, int64, int, int64, int, int64, string, bool) (bool, error)
	RefreshLiveLease(context.Context, int64, int64, int64, string) (bool, error)
	ReleaseLiveLease(context.Context, int64, int64, int64, string) error
	CheckLiveLease(context.Context, int64, int64, int64, string) (time.Duration, error)
	RefreshLiveLeaseProof(context.Context, int64, int64, int64, string) (time.Duration, error)
}

type LiveCapacityRequest struct {
	Identity                LeaseIdentity
	AccountLimit, UserLimit int
	ReplacingRegularSlots   bool
}

// AcquireLiveCapacity atomically reserves the existing account/user/key members
// and starts a verified lifetime. The caller releases on failure or hands off to
// a separately authorized observer/controller after persisting the call record.
func AcquireLiveCapacity(ctx context.Context, store LiveLeaseStore, request LiveCapacityRequest) (*Lease, bool, error) {
	id := request.Identity
	if store == nil || id.AccountID <= 0 || id.UserID <= 0 || id.APIKeyID <= 0 || id.Token == "" {
		return nil, false, ErrCapacityDependencyUnavailable
	}
	acquired, err := store.AcquireLiveLease(ctx, id.AccountID, request.AccountLimit, id.UserID, request.UserLimit, id.APIKeyID, id.Token, request.ReplacingRegularSlots)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	lease, err := AdoptLiveLease(ctx, store, id)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = store.ReleaseLiveLease(cleanup, id.AccountID, id.UserID, id.APIKeyID, id.Token)
		return nil, false, err
	}
	return lease, true, nil
}

// AdoptLiveLease verifies an existing token before a new controller/dial. It
// never acquires or reconstructs missing capacity. Handoff keeps the token for
// the next controller; terminal finalization must call Release.
func AdoptLiveLease(ctx context.Context, store LiveLeaseStore, id LeaseIdentity) (*Lease, error) {
	if store == nil || id.AccountID <= 0 || id.UserID <= 0 || id.APIKeyID <= 0 || id.Token == "" {
		return nil, ErrCapacityDependencyUnavailable
	}
	started := time.Now()
	proofCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	remaining, err := store.CheckLiveLease(proofCtx, id.AccountID, id.UserID, id.APIKeyID, id.Token)
	cancel()
	if err != nil {
		return nil, errors.Join(ErrCapacityDependencyUnavailable, err)
	}
	lease, err := newOwnedLease(id, started, remaining,
		func(ctx context.Context) (time.Duration, error) {
			return store.CheckLiveLease(ctx, id.AccountID, id.UserID, id.APIKeyID, id.Token)
		},
		func(ctx context.Context) (time.Duration, error) {
			return store.RefreshLiveLeaseProof(ctx, id.AccountID, id.UserID, id.APIKeyID, id.Token)
		},
		func(ctx context.Context) error {
			return store.ReleaseLiveLease(ctx, id.AccountID, id.UserID, id.APIKeyID, id.Token)
		}, false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		lease.Handoff()
		return nil, err
	}
	return lease, nil
}
