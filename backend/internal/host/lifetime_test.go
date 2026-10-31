package host

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifetimeOrdersRealJoinsBeforeUsageAndStorage(t *testing.T) {
	l := NewLifetime()
	var order []string
	add := func(name string, phase ShutdownPhase, start int) {
		l.Add(Component{Name: name, Phase: phase, StartOrder: start, Constructed: true, Start: func(context.Context) error { order = append(order, "start:"+name); return nil }, Stop: func(context.Context) error { order = append(order, "stop:"+name); return nil }})
	}
	add("postgres", Infrastructure, 0)
	add("redis", Infrastructure, 1)
	add("cache", Caches, 2)
	add("usage", Usage, 3)
	add("producer", Producers, 4)
	if err := l.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"start:postgres", "start:redis", "start:cache", "start:usage", "start:producer", "stop:producer", "stop:usage", "stop:cache", "stop:redis", "stop:postgres"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v", order)
	}
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != len(want) {
		t.Fatal("closed completed resources twice")
	}
}

func TestLifetimeConstructionRollbackOwnsStartedBeforeFailure(t *testing.T) {
	l := NewLifetime()
	closed := false
	l.Add(Component{Name: "db", Phase: Infrastructure, Constructed: true, Stop: func(context.Context) error { closed = true; return nil }})
	l.Add(Component{Name: "not-started", Phase: Producers, Start: func(context.Context) error { return nil }, Stop: func(context.Context) error { t.Error("pure unstarted constructor was treated as running"); return nil }})
	// Mirrors a later Wire provider returning an error before Application exists.
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("constructed DB not released")
	}
}

func TestLifetimeRetainsBlockedShutdownAndDoesNotCloseDependencies(t *testing.T) {
	l := NewLifetime()
	release := make(chan struct{})
	var stops atomic.Int32
	closed := false
	owner := OwnBlockingShutdown("legacy", func() error { stops.Add(1); <-release; return nil }, true)
	l.Add(Component{Name: "db", Phase: Infrastructure, Constructed: true, Stop: func(context.Context) error { closed = true; return nil }})
	l.Add(Component{Name: "legacy", Phase: Producers, Constructed: true, Stop: owner.Stop})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := l.Cleanup(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if closed || len(l.Pending()) != 2 {
		t.Fatalf("unjoined ownership lost: %v", l.Pending())
	}
	close(release)
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := l.Cleanup(bounded); err != nil {
		t.Fatal(err)
	}
	if !closed || stops.Load() != 1 {
		t.Fatal("retry restarted shutdown or failed to release DB")
	}
}

func TestLifetimeSignalOnlyStopCannotAdvanceToUsage(t *testing.T) {
	l := NewLifetime()
	usageStopped := false
	owner := OwnBlockingShutdown("signal-only", func() error { return nil }, false)
	l.Add(Component{Name: "usage", Phase: Usage, Constructed: true, Stop: func(context.Context) error { usageStopped = true; return nil }})
	l.Add(Component{Name: "signal-only", Phase: Producers, Constructed: true, Stop: owner.Stop})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := l.Cleanup(ctx); err == nil {
		t.Fatal("signal was treated as a join")
	}
	if usageStopped {
		t.Fatal("advanced despite an unjoined producer")
	}
}

func TestLifetimeFailedStartRetainsPartiallyStartedComponent(t *testing.T) {
	l := NewLifetime()
	stopped := false
	failed := errors.New("startup failed")
	l.Add(Component{Name: "partial", Phase: Producers, Start: func(context.Context) error { return failed }, Stop: func(context.Context) error { stopped = true; return nil }})
	if err := l.Start(context.Background()); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("failed Start escaped rollback")
	}
}

func TestLifetimeSignalsIndependentProducersBeforeWaitingForPeers(t *testing.T) {
	l := NewLifetime()
	release := make(chan struct{})
	a := OwnBlockingShutdown("a", func() error { <-release; return nil }, true)
	b := OwnBlockingShutdown("b", func() error { close(release); return nil }, true)
	// Reverse construction visits a first. b must receive its stop request before
	// waiting on a, but billing/storage must still wait for both real joins.
	l.Add(Component{Name: "b", Phase: Producers, Constructed: true, RequestStop: b.RequestStop, Stop: b.Stop})
	l.Add(Component{Name: "a", Phase: Producers, Constructed: true, RequestStop: a.RequestStop, Stop: a.Stop})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := l.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(l.Pending()) != 0 {
		t.Fatal(l.Pending())
	}
}

func TestLifetimeDoesNotCloseLaterDependencyCheckpointAfterUnjoinedPeer(t *testing.T) {
	l := NewLifetime()
	transportClosed := false
	signal := OwnBlockingShutdown("jobs", func() error { return nil }, false)
	l.Add(Component{Name: "transport", Phase: Producers, ShutdownOrder: 10, Constructed: true, Stop: func(context.Context) error { transportClosed = true; return nil }})
	l.Add(Component{Name: "jobs", Phase: Producers, ShutdownOrder: 0, Constructed: true, RequestStop: signal.RequestStop, Stop: signal.Stop})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := l.Cleanup(ctx); err == nil {
		t.Fatal("unverified join accepted")
	}
	if transportClosed {
		t.Fatal("transport closed under an unjoined generator")
	}
}

func TestLifetimeCacheFillCheckpointPrecedesEveryUsageCallback(t *testing.T) {
	l := NewLifetime()
	var order []string
	l.Add(Component{Name: "redis", Phase: Infrastructure, Constructed: true, Stop: func(context.Context) error { order = append(order, "redis-stop"); return nil }})
	l.Add(Component{Name: "cache", Phase: Caches, Constructed: true, DrainBeforeUsage: true,
		Drain: func(context.Context) error { order = append(order, "cache-drain"); return nil },
		Stop:  func(context.Context) error { order = append(order, "cache-stop"); return nil }})
	for i, name := range []string{"cloud", "usage"} {
		name := name
		l.Add(Component{Name: name, Phase: Usage, ShutdownOrder: i, Constructed: true,
			RequestStop: func() { order = append(order, name+"-request") },
			Drain:       func(context.Context) error { order = append(order, name+"-drain"); return nil },
			Stop:        func(context.Context) error { order = append(order, name+"-stop"); return nil }})
	}
	l.Add(Component{Name: "producer", Phase: Producers, Constructed: true, Stop: func(context.Context) error { order = append(order, "producer-stop"); return nil }})
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"producer-stop", "cache-drain", "cloud-request", "cloud-drain", "cloud-stop", "usage-request", "usage-drain", "usage-stop", "cache-stop", "redis-stop"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v; want=%v", order, want)
	}
	if err := l.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatal("completed cache drain or stop repeated")
	}
}

func TestLifetimeFailedCacheFillCheckpointRetainsUsageAndDependencies(t *testing.T) {
	for _, failed := range []error{errors.New("cache fill failed"), context.DeadlineExceeded} {
		t.Run(failed.Error(), func(t *testing.T) {
			l := NewLifetime()
			drainCalls, usageCalls, cacheStops, storageStops := 0, 0, 0, 0
			fail := true
			l.Add(Component{Name: "cache", Phase: Caches, Constructed: true, DrainBeforeUsage: true,
				Drain: func(context.Context) error {
					drainCalls++
					if fail {
						return failed
					}
					return nil
				},
				Stop: func(context.Context) error { cacheStops++; return nil }})
			l.Add(Component{Name: "usage", Phase: Usage, Constructed: true,
				RequestStop: func() { usageCalls++ },
				Drain:       func(context.Context) error { usageCalls++; return nil },
				Stop:        func(context.Context) error { usageCalls++; return nil }})
			l.Add(Component{Name: "storage", Phase: Infrastructure, Constructed: true, Stop: func(context.Context) error { storageStops++; return nil }})
			if err := l.Cleanup(context.Background()); !errors.Is(err, failed) {
				t.Fatalf("error=%v", err)
			}
			if drainCalls != 1 || usageCalls != 0 || cacheStops != 0 || storageStops != 0 || len(l.Pending()) != 3 {
				t.Fatalf("failed checkpoint advanced: drains=%d usage=%d cache=%d storage=%d pending=%v", drainCalls, usageCalls, cacheStops, storageStops, l.Pending())
			}
			fail = false
			if err := l.Cleanup(context.Background()); err != nil {
				t.Fatal(err)
			}
			if drainCalls != 2 || usageCalls != 3 || cacheStops != 1 || storageStops != 1 || len(l.Pending()) != 0 {
				t.Fatalf("retry lost ownership: drains=%d usage=%d cache=%d storage=%d", drainCalls, usageCalls, cacheStops, storageStops)
			}
		})
	}
}
