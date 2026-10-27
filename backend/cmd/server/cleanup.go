package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
)

type cleanupStep struct {
	name string
	fn   func() error
}

// runCleanupSteps shares the application's shutdown deadline across phases.
// An unfinished worker phase must never be mistaken for permission to close
// the database and Redis clients still in use by those workers.
func runCleanupSteps(ctx context.Context, steps []cleanupStep, parallel bool) bool {
	if ctx.Err() != nil {
		return false
	}
	var failed atomic.Bool
	run := func(step cleanupStep) {
		if err := step.fn(); err != nil {
			failed.Store(true)
			log.Printf("[Cleanup] %s failed: %v", step.name, err)
			return
		}
		log.Printf("[Cleanup] %s succeeded", step.name)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if parallel {
			var workers sync.WaitGroup
			for _, step := range steps {
				workers.Add(1)
				go func() { defer workers.Done(); run(step) }()
			}
			workers.Wait()
			return
		}
		for _, step := range steps {
			if ctx.Err() != nil {
				return
			}
			run(step)
		}
	}()
	select {
	case <-done:
		return !failed.Load() && ctx.Err() == nil
	case <-ctx.Done():
		log.Printf("[Cleanup] phase did not finish before shutdown deadline: %v", ctx.Err())
		return false
	}
}
