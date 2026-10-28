//go:build unit

package reservation

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type balanceReleaseFunc func(context.Context, int64, string) error

func (f balanceReleaseFunc) ReleaseInflightBalance(ctx context.Context, userID int64, requestID string) error {
	return f(ctx, userID, requestID)
}

type scopeReleaseFunc func(context.Context, string, string) error

func (f scopeReleaseFunc) ReleaseInflightScope(ctx context.Context, scope, requestID string) error {
	return f(ctx, scope, requestID)
}

type renewFunc func(context.Context, int64, string, time.Duration) (bool, error)

func (f renewFunc) RenewInflightBalance(ctx context.Context, userID int64, requestID string, ttl time.Duration) (bool, error) {
	return f(ctx, userID, requestID, ttl)
}

func TestNilAndZeroHandles(t *testing.T) {
	for _, r := range []*Handle{nil, {}} {
		r.Acquire()()
		r.StartRenewal(nil)
		r.StartRenewal(renewFunc(func(context.Context, int64, string, time.Duration) (bool, error) {
			t.Error("inactive handle started renewal")
			return false, nil
		}))
		r.AddScope(nil, "")
		r.HandlerDone()
		r.HandlerDone()
		r.Release()
		r.Release()
		if r.Amount() != 0 || r.RequestID() != "" || r.TTL() != 0 || !reflect.DeepEqual(r.Snapshot(), Snapshot{}) {
			t.Fatal("inactive handle returned nonzero facts")
		}
	}
}

func TestConstructionDoesNotStartRenewalAndStartIsIdempotent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var releases, renews atomic.Int32
		r := New(Options{TTL: 30 * time.Millisecond, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
			releases.Add(1)
			return nil
		})})
		defer r.Release()
		time.Sleep(time.Minute) // virtual time; construction must have no background work
		synctest.Wait()
		if releases.Load() != 0 || renews.Load() != 0 {
			t.Fatal("constructor performed cache I/O")
		}
		renewer := renewFunc(func(context.Context, int64, string, time.Duration) (bool, error) {
			renews.Add(1)
			return true, nil
		})
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() { r.StartRenewal(renewer) })
		}
		wg.Wait()
		synctest.Wait()
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		if renews.Load() != 1 {
			t.Fatalf("renewals after one interval = %d, want 1", renews.Load())
		}
		r.HandlerDone()
		r.StartRenewal(renewer)
		time.Sleep(time.Second)
		synctest.Wait()
		if renews.Load() != 1 || releases.Load() != 1 {
			t.Fatalf("stopped handle restarted: renews=%d releases=%d", renews.Load(), releases.Load())
		}
	})
}

func TestAsyncReferencesHoldUntilLastBillingCompletion(t *testing.T) {
	var releases atomic.Int32
	r := New(Options{UserID: 42, RequestID: "hold", Balance: balanceReleaseFunc(func(ctx context.Context, userID int64, requestID string) error {
		if ctx.Err() != nil || userID != 42 || requestID != "hold" {
			t.Errorf("release lost context or identity: %v %d %s", ctx.Err(), userID, requestID)
		}
		releases.Add(1)
		return nil
	})})
	defer r.Release()
	last := r.Acquire()
	var wg sync.WaitGroup
	for range 32 {
		done := r.Acquire()
		wg.Go(func() {
			r.HandlerDone()
			done()
			done()
		})
	}
	wg.Wait()
	if releases.Load() != 0 {
		t.Fatal("handler return or duplicate completion released pending billing")
	}
	// Existing billing work may hand off another reference after handler return.
	late := r.Acquire()
	last()
	if releases.Load() != 0 {
		t.Fatal("late billing handoff lost its reservation")
	}
	late()
	late()
	r.Acquire()()
	r.HandlerDone()
	r.Release()
	if releases.Load() != 1 {
		t.Fatalf("releases = %d, want 1", releases.Load())
	}
}

func TestConcurrentAcquireHandlerDoneReleaseAndStart(t *testing.T) {
	for range 32 {
		var releases atomic.Int32
		r := New(Options{TTL: time.Minute, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
			releases.Add(1)
			return nil
		})})
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range 32 {
			wg.Go(func() {
				<-start
				switch i % 4 {
				case 0:
					done := r.Acquire()
					done()
					done()
				case 1:
					r.HandlerDone()
				case 2:
					r.Release()
				case 3:
					r.StartRenewal(renewFunc(func(context.Context, int64, string, time.Duration) (bool, error) { return true, nil }))
				}
			})
		}
		close(start)
		wg.Wait()
		r.Release()
		r.Acquire()()
		if releases.Load() != 1 {
			t.Fatalf("concurrent releases = %d, want 1", releases.Load())
		}
	}
}

func TestStopCancelsAndJoinsInflightRenewal(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		name := "handler_done"
		if immediate {
			name = "explicit_release"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, canceled, allowReturn, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releases, errorsReported atomic.Int32
				r := New(Options{UserID: 7, RequestID: "stream", TTL: time.Second, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
					select {
					case <-exited:
					default:
						t.Error("released before renewal returned")
					}
					releases.Add(1)
					return nil
				}), OnRenewError: func(error) { errorsReported.Add(1) }})
				done := r.Acquire()
				r.StartRenewal(renewFunc(func(ctx context.Context, userID int64, requestID string, ttl time.Duration) (bool, error) {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) != 2*time.Second || userID != 7 || requestID != "stream" || ttl != time.Second {
						t.Error("renewal changed its budget or identity")
					}
					close(started)
					<-ctx.Done()
					close(canceled)
					<-allowReturn
					close(exited)
					return false, ctx.Err()
				}))
				<-started
				stopped := make(chan struct{})
				go func() {
					if immediate {
						r.Release()
					} else {
						r.HandlerDone()
					}
					close(stopped)
				}()
				<-canceled
				synctest.Wait()
				select {
				case <-stopped:
					t.Fatal("stop returned without joining renewal")
				default:
				}
				if releases.Load() != 0 {
					t.Fatal("cleanup overlapped renewal")
				}
				close(allowReturn)
				<-stopped
				if !immediate && releases.Load() != 0 {
					t.Fatal("HandlerDone released an unresolved billing reference")
				}
				done()
				r.HandlerDone()
				r.Release()
				if releases.Load() != 1 || errorsReported.Load() != 0 {
					t.Fatalf("releases=%d cancellation errors=%d", releases.Load(), errorsReported.Load())
				}
			})
		})
	}
}

func TestHandlerDoneStopsRenewalButPendingBillingKeepsTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const ttl = 90 * time.Millisecond
		var mu sync.Mutex
		expires := time.Now().Add(ttl)
		var renews, releases atomic.Int32
		r := New(Options{TTL: ttl, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
			releases.Add(1)
			return nil
		})})
		defer r.Release()
		r.StartRenewal(renewFunc(func(context.Context, int64, string, time.Duration) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if !expires.After(time.Now()) {
				return false, nil
			}
			expires = time.Now().Add(ttl)
			renews.Add(1)
			return true, nil
		}))
		synctest.Wait()
		time.Sleep(5 * ttl)
		synctest.Wait()
		mu.Lock()
		alive := expires.After(time.Now())
		mu.Unlock()
		if !alive || renews.Load() == 0 {
			t.Fatal("active stream lost its reservation")
		}
		done := r.Acquire()
		r.HandlerDone()
		stoppedAt := renews.Load()
		time.Sleep(2 * ttl)
		synctest.Wait()
		mu.Lock()
		alive = expires.After(time.Now())
		mu.Unlock()
		if alive || renews.Load() != stoppedAt || releases.Load() != 0 {
			t.Fatal("pending billing must expire by TTL without renewal or early release")
		}
		done()
		if releases.Load() != 1 {
			t.Fatal("late billing completion did not release exactly once")
		}
	})
}

func TestReleaseSharesIndependentBoundedCleanupBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var deadline time.Time
		var reported []string
		var r *Handle
		r = New(Options{UserID: 7, RequestID: "fixed", Balance: balanceReleaseFunc(func(ctx context.Context, userID int64, requestID string) error {
			var ok bool
			deadline, ok = ctx.Deadline()
			if !ok || ctx.Err() != nil || userID != 7 || requestID != "fixed" {
				t.Error("cleanup lacks a live bounded context or original identity")
			}
			<-ctx.Done()
			return ctx.Err()
		}), OnReleaseError: func(scope string, err error) {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("lost cleanup failure: %v", err)
			}
			_ = r.Snapshot() // reporting must not hold the state mutex
			reported = append(reported, scope)
		}})
		scopes := scopeReleaseFunc(func(ctx context.Context, scope, requestID string) error {
			scopeDeadline, _ := ctx.Deadline()
			if !deadline.Equal(scopeDeadline) || requestID != "fixed" {
				t.Error("scope cleanup reset the common budget or changed identity")
			}
			return ctx.Err()
		})
		r.AddScope(scopes, "sub:3")
		r.AddScope(scopes, "apikey:9")
		started := time.Now()
		r.Release()
		r.Release()
		if time.Since(started) != 2*time.Second || !reflect.DeepEqual(reported, []string{"", "sub:3", "apikey:9"}) {
			t.Fatalf("cleanup duration=%v errors=%v", time.Since(started), reported)
		}
	})
}

func TestRenewalTimeoutRetriesWithoutReleasingHold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var attempts, reported, releases atomic.Int32
		r := New(Options{TTL: 9 * time.Second, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
			releases.Add(1)
			return nil
		}), OnRenewError: func(err error) {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("renewal error=%v", err)
			}
			reported.Add(1)
		}})
		defer r.Release()
		r.StartRenewal(renewFunc(func(ctx context.Context, _ int64, _ string, _ time.Duration) (bool, error) {
			attempts.Add(1)
			<-ctx.Done()
			return false, ctx.Err()
		}))
		synctest.Wait()
		time.Sleep(8 * time.Second)
		synctest.Wait()
		if attempts.Load() != 2 || reported.Load() != 2 || releases.Load() != 0 {
			t.Fatalf("attempts=%d reported=%d releases=%d", attempts.Load(), reported.Load(), releases.Load())
		}
		r.HandlerDone()
	})
}

func TestMissingCacheEntryStopsRenewalWithoutReleasingBilling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var attempts, releases atomic.Int32
		r := New(Options{TTL: 30 * time.Millisecond, Balance: balanceReleaseFunc(func(context.Context, int64, string) error {
			releases.Add(1)
			return nil
		})})
		defer r.Release()
		done := r.Acquire()
		r.StartRenewal(renewFunc(func(context.Context, int64, string, time.Duration) (bool, error) {
			attempts.Add(1)
			return false, nil
		}))
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if attempts.Load() != 1 || releases.Load() != 0 {
			t.Fatal("missing cache entry restarted renewal or released pending billing")
		}
		r.HandlerDone()
		if releases.Load() != 0 {
			t.Fatal("missing cache entry consumed the billing reference")
		}
		done()
		if releases.Load() != 1 {
			t.Fatal("billing completion did not release")
		}
	})
}

func TestScopeOnlyHoldAndDetachedSnapshot(t *testing.T) {
	var released []string
	scopes := scopeReleaseFunc(func(_ context.Context, scope, requestID string) error {
		if requestID != "fixed" {
			t.Errorf("scope identity=%s", requestID)
		}
		released = append(released, scope)
		return nil
	})
	r := New(Options{UserID: 4, RequestID: "fixed", Amount: 0.9, TTL: time.Minute})
	r.AddScope(scopes, "sub:3")
	r.AddScope(scopes, "apikey:9")
	want := Snapshot{UserID: 4, RequestID: "fixed", Scopes: []string{"sub:3", "apikey:9"}}
	snapshot := r.Snapshot()
	if !reflect.DeepEqual(snapshot, want) || r.Amount() != 0.9 || r.RequestID() != "fixed" || r.TTL() != time.Minute {
		t.Fatalf("scope-only identity or amount changed: %+v", snapshot)
	}
	snapshot.Scopes[0] = "changed"
	if !reflect.DeepEqual(r.Snapshot(), want) {
		t.Fatal("snapshot exposes mutable release identities")
	}
	done := r.Acquire()
	r.HandlerDone()
	if len(released) != 0 {
		t.Fatal("subscription/Key reservation released before billing")
	}
	done()
	done()
	r.Release()
	if !reflect.DeepEqual(released, want.Scopes) || !reflect.DeepEqual(r.Snapshot(), want) {
		t.Fatalf("scope releases or recovery identity changed: %v %+v", released, r.Snapshot())
	}
}

func TestScopeTransferRacingReleaseIsNotLost(t *testing.T) {
	var releases atomic.Int32
	scope := scopeReleaseFunc(func(context.Context, string, string) error {
		releases.Add(1)
		return nil
	})
	for i := range 64 {
		r := New(Options{RequestID: "fixed"})
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; r.AddScope(scope, "apikey:9") })
		wg.Go(func() { <-start; r.Release() })
		close(start)
		wg.Wait()
		if releases.Load() != int32(i+1) {
			t.Fatal("scope transfer was leaked or released twice")
		}
	}
}
