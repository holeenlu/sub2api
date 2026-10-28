package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaintenanceOwnerDrainFencesAndStopCancelsIO(t *testing.T) {
	var owner maintenanceTaskOwner
	if err := owner.start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := owner.begin()
	if err != nil {
		t.Fatal(err)
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.drain(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain: %v", err)
	}
	if _, _, err := owner.begin(); !errors.Is(err, errMaintenanceStopped) {
		t.Fatalf("admission: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("Drain canceled already admitted work")
	}
	if err := owner.stop(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop: %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("Stop did not cancel IO context")
	}
	finish()
	if err := owner.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.start(context.Background(), nil); !errors.Is(err, errMaintenanceStopped) {
		t.Fatalf("restart: %v", err)
	}
}

func TestMaintenanceOwnerTimeoutRetainsActualCompletion(t *testing.T) {
	var owner maintenanceTaskOwner
	if err := owner.start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	if err := owner.launch(func(context.Context) { <-release }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := owner.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-owner.done:
		t.Fatal("unjoined task reported complete")
	default:
	}
	close(release)
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := owner.stop(joined); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceOwnerStopJoinsStartupIO(t *testing.T) {
	var owner maintenanceTaskOwner
	entered := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		started <- owner.start(context.Background(), func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := owner.stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("startup: %v", err)
	}
}

func TestMaintenanceOwnerPropagatesCloseErrorAfterJoin(t *testing.T) {
	var owner maintenanceTaskOwner
	if err := owner.start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	want := errors.New("socket close failed")
	if err := owner.launch(func(context.Context) { owner.recordCloseError(want) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := owner.stop(ctx); !errors.Is(err, want) {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("close error lost join state")
	}
}

func TestMaintenanceOwnerStopBeforeStartIsTerminal(t *testing.T) {
	var owner maintenanceTaskOwner
	if err := owner.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.start(context.Background(), nil); !errors.Is(err, errMaintenanceStopped) {
		t.Fatalf("start: %v", err)
	}
}

type stoppableMaintenance interface{ Stop(context.Context) error }

func stopMaintenanceForTest(t *testing.T, svc stoppableMaintenance) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := svc.Stop(ctx); err != nil {
		t.Errorf("maintenance stop: %v", err)
	}
}
