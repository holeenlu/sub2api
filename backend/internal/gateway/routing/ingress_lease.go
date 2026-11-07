package routing

import (
	"context"
	"errors"
	"time"
)

// IngressLeaseStore preserves the existing API-key ZSET/token Redis contract.
// It limits live client connections, independently of per-turn request slots.
type IngressLeaseStore interface {
	AcquireOpenAIWSIngressLease(context.Context, int64, int, string) (bool, error)
	RefreshOpenAIWSIngressLease(context.Context, int64, string) (bool, error)
	ReleaseOpenAIWSIngressLease(context.Context, int64, string) error
}

// ErrIngressLeaseLost preserves the ingress boundary's established error identity.
var ErrIngressLeaseLost = errors.New("openai websocket ingress lease lost")

// IngressLease owns an acquired connection token. Release joins renewal and
// releases only that token. Parent cancellation also reclaims it. A zero value
// has no permission or background work and is safe to release.
type IngressLease struct{ lifetime *leaseLifetime }

func (l *IngressLease) Context() context.Context {
	if l == nil || l.lifetime == nil {
		return context.Background()
	}
	return l.lifetime.ctx
}

func (l *IngressLease) Release() {
	if l != nil {
		_ = l.lifetime.release()
	}
}

// AcquireIngressLease starts renewal only after successful atomic acquisition.
// token is the caller's unique request token, never an API key or credential.
// observe reports bounded renewal/release errors; it must not block. A disabled
// limit returns (nil,true,nil), preserving the existing explicit opt-out.
func AcquireIngressLease(ctx context.Context, store IngressLeaseStore, keyID int64, limit int, token string, observe func(error, bool)) (*IngressLease, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	if store == nil || keyID <= 0 || token == "" {
		return nil, false, errors.New("openai websocket ingress lease cache is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	started := time.Now()
	acquireCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	acquired, err := store.AcquireOpenAIWSIngressLease(acquireCtx, keyID, limit, token)
	cancel()
	if err != nil || !acquired {
		return nil, acquired, err
	}
	l := &leaseLifetime{
		ttl: 60 * time.Second, interval: 20 * time.Second, timeout: 2 * time.Second,
		confirmedUntil: started.Add(60 * time.Second), lost: ErrIngressLeaseLost, observe: observe,
		renew: func(ctx context.Context) (bool, error) { return store.RefreshOpenAIWSIngressLease(ctx, keyID, token) },
		cleanup: func(ctx context.Context) error {
			err := store.ReleaseOpenAIWSIngressLease(ctx, keyID, token)
			if err != nil && observe != nil {
				observe(err, false)
			}
			return err
		},
	}
	l.start(ctx)
	if err := ctx.Err(); err != nil {
		_ = l.release()
		return nil, false, err
	}
	return &IngressLease{lifetime: l}, true, nil
}
