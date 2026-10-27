package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunCleanupStepsWaitsForWorkersAndHonorsSharedDeadline(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		result <- runCleanupSteps(ctx, []cleanupStep{{"blocked", func() error {
			close(started)
			<-release
			close(finished)
			return nil
		}}}, true)
	}()
	<-started
	select {
	case <-result:
		t.Fatal("cleanup returned while the worker was still running")
	default:
	}
	cancel()
	select {
	case complete := <-result:
		if complete {
			t.Fatal("timed out cleanup was reported as complete")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not honor its deadline")
	}
	close(release)
	<-finished
}

func TestRunCleanupStepsReportsFailureAndSkipsExpiredPhase(t *testing.T) {
	if runCleanupSteps(context.Background(), []cleanupStep{{"failed", func() error { return errors.New("stop failed") }}}, true) {
		t.Fatal("failed cleanup was reported as complete")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	if runCleanupSteps(ctx, []cleanupStep{{"infrastructure", func() error { calls.Add(1); return nil }}}, false) {
		t.Fatal("expired cleanup was reported as complete")
	}
	if calls.Load() != 0 {
		t.Fatal("infrastructure cleanup started after the deadline")
	}
}
