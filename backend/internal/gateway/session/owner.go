package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrPreempted = errors.New("openai ws session preempted by newer request")

const (
	OwnerTTL           = 2 * time.Hour
	OwnerWatchInterval = 2 * time.Second
	OwnerCloseGrace    = time.Second
)

// OwnerScope is an already authenticated execution lane. An opaque session ID
// alone is not a scope or authorization. Group zero remains ineligible for
// native session preemption, matching the existing ingress contract.
type OwnerScope struct {
	GroupID     int64
	APIKeyID    int64
	SessionHash string
}

func (s OwnerScope) normalized() (OwnerScope, bool) {
	s.SessionHash = strings.TrimSpace(s.SessionHash)
	return s, s.GroupID > 0 && s.APIKeyID > 0 && s.SessionHash != ""
}

// OwnerCache retains the current Redis atomic window protocol and token wire
// format. Local epoch, opaque remote token and access revocation are distinct.
type OwnerCache interface {
	ClaimOpenAIResponsesSessionWindow(context.Context, int64, string, []byte, time.Duration) ([]byte, error)
	CompareAndRefreshOpenAIResponsesSessionWindow(context.Context, int64, string, []byte, time.Duration) (bool, error)
	CompareAndDeleteOpenAIResponsesSessionWindow(context.Context, int64, string, []byte) (bool, error)
}

func OwnerCacheHash(apiKeyID int64, sessionHash string) string {
	return fmt.Sprintf("wspreempt:%d:%s", apiKeyID, strings.TrimSpace(sessionHash))
}

// OwnerRegistry owns the sole active registration per native execution lane.
// The zero value is ready to use. Its mutex protects epochs and registrations;
// network work is always outside the lock. The invalidation callback and
// Publish may only mutate bounded in-memory state, under registry -> store lock
// order. This prevents a replaced owner's invalidation from clearing new state.
type OwnerRegistry struct {
	mu     sync.Mutex
	next   uint64
	active map[OwnerScope]*Owner
}

// Owner is a process-local registration, never a serializable socket or grant.
// Acquire starts one worker; Close cancels and joins it before releasing the
// remote token. A notification must have a bounded transport close and observe
// its context. Cancellation does not wait for the close handshake to finish;
// joining does. No path replays a generation.
type Owner struct {
	registry    *OwnerRegistry
	scope       OwnerScope
	epoch       uint64
	token       string
	cache       OwnerCache
	remote      bool
	ctx         context.Context
	cancel      context.CancelCauseFunc
	stop        context.CancelFunc
	notify      func(context.Context)
	onPreempt   func()
	preempted   atomic.Bool
	closing     atomic.Bool
	preemptOnce sync.Once
	closeOnce   sync.Once
	preemptCh   chan struct{}
	done        chan struct{}
}

// Acquire preserves optional shared preemption: a missing/failed claim leaves
// local preemption active, not a shared ownership authorization. Resource and
// grant authorization must still be revalidated before dispatch. The caller
// owns Close, including when the parent context has already ended.
func (r *OwnerRegistry) Acquire(ctx context.Context, scope OwnerScope, token string, cache OwnerCache, onPreempt func(), notify func(context.Context)) (*Owner, bool) {
	scope, valid := scope.normalized()
	if r == nil || !valid || strings.TrimSpace(token) == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	ownerCtx, cancel := context.WithCancelCause(ctx)
	watchCtx, stop := context.WithCancel(ownerCtx)
	o := &Owner{registry: r, scope: scope, token: strings.TrimSpace(token), cache: cache, ctx: ownerCtx, cancel: cancel, stop: stop, notify: notify, onPreempt: onPreempt, preemptCh: make(chan struct{}), done: make(chan struct{})}
	previous, claimed := claimOwner(ctx, cache, scope, o.token)
	o.remote = claimed
	r.mu.Lock()
	if r.active == nil {
		r.active = make(map[OwnerScope]*Owner)
	}
	r.next++
	o.epoch = r.next
	localPrevious := r.active[scope]
	replaced := localPrevious != nil || claimed && previous != "" && previous != o.token
	if replaced && onPreempt != nil {
		onPreempt()
	}
	r.active[scope] = o
	r.mu.Unlock()
	go o.run(watchCtx)
	if localPrevious != nil {
		localPrevious.preempt()
	}
	return o, replaced
}

func (o *Owner) Context() context.Context { return o.ctx }
func (o *Owner) Epoch() uint64            { return o.epoch }
func (o *Owner) Preempted() bool          { return o != nil && o.preempted.Load() }

// Current only proves this process's active owner epoch. It is not a remote
// lease, authorization proof, or permission to replay a disconnected turn.
func (o *Owner) Current() bool {
	if o == nil || o.closing.Load() || o.ctx.Err() != nil || o.Preempted() {
		return false
	}
	o.registry.mu.Lock()
	defer o.registry.mu.Unlock()
	return o.registry.active[o.scope] == o && !o.closing.Load() && o.ctx.Err() == nil && !o.Preempted()
}

// Publish atomically fences a bounded local state mutation against replacement,
// Close and preemption. It never authorizes a network dispatch or a shared store
// write. Callbacks must not block, call this registry, or perform I/O.
func (o *Owner) Publish(mutate func()) bool {
	if o == nil || mutate == nil {
		return false
	}
	o.registry.mu.Lock()
	defer o.registry.mu.Unlock()
	if o.registry.active[o.scope] != o || o.closing.Load() || o.ctx.Err() != nil || o.Preempted() {
		return false
	}
	mutate()
	return true
}

func (o *Owner) preempt() {
	o.preemptOnce.Do(func() {
		if o.closing.Load() {
			return
		}
		o.preempted.Store(true)
		o.registry.mu.Lock()
		if o.registry.active[o.scope] == o && o.onPreempt != nil {
			o.onPreempt()
		}
		o.registry.mu.Unlock()
		if o.notify == nil {
			o.cancel(ErrPreempted)
		}
		close(o.preemptCh)
	})
}

func (o *Owner) run(ctx context.Context) {
	defer close(o.done)
	var ticker *time.Ticker
	var ticks <-chan time.Time
	if o.remote {
		ticker = time.NewTicker(OwnerWatchInterval)
		ticks = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-o.preemptCh:
			if o.notify != nil {
				notifyCtx, cancel := context.WithTimeout(ctx, OwnerCloseGrace)
				canceled := make(chan struct{})
				stopCancel := context.AfterFunc(notifyCtx, func() {
					o.cancel(ErrPreempted)
					close(canceled)
				})
				o.notify(notifyCtx)
				if !stopCancel() {
					<-canceled
				}
				cancel()
			}
			o.cancel(ErrPreempted)
			return
		case <-ticks:
			cacheCtx, cancel := withCacheTimeout(ctx)
			owned, err := o.cache.CompareAndRefreshOpenAIResponsesSessionWindow(cacheCtx, o.scope.GroupID, OwnerCacheHash(o.scope.APIKeyID, o.scope.SessionHash), []byte(o.token), OwnerTTL)
			cancel()
			if err == nil && !owned {
				o.preempt()
			}
		}
	}
}

// Close is idempotent and joins the single worker and its bounded notification.
// Compare-and-delete cannot release a replacement's remote token.
func (o *Owner) Close() {
	if o == nil {
		return
	}
	o.closeOnce.Do(func() {
		o.closing.Store(true)
		o.stop()
		<-o.done
		o.registry.mu.Lock()
		if o.registry.active[o.scope] == o {
			delete(o.registry.active, o.scope)
		}
		o.registry.mu.Unlock()
		if o.remote {
			releaseOwner(o.cache, o.scope, o.token)
		}
		if o.Preempted() {
			o.cancel(ErrPreempted)
		} else {
			o.cancel(nil)
		}
	})
}

func claimOwner(ctx context.Context, cache OwnerCache, scope OwnerScope, token string) (string, bool) {
	if cache == nil {
		return "", false
	}
	cacheCtx, cancel := withCacheTimeout(ctx)
	defer cancel()
	previous, err := cache.ClaimOpenAIResponsesSessionWindow(cacheCtx, scope.GroupID, OwnerCacheHash(scope.APIKeyID, scope.SessionHash), []byte(token), OwnerTTL)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(previous)), true
}

func releaseOwner(cache OwnerCache, scope OwnerScope, token string) {
	// Releasing the acquired token has its own bounded cleanup context. It
	// cannot generate or erase a successor; an error expires via the old TTL.
	ctx, cancel := withCacheTimeout(context.Background())
	defer cancel()
	_, _ = cache.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, scope.GroupID, OwnerCacheHash(scope.APIKeyID, scope.SessionHash), []byte(token))
}
