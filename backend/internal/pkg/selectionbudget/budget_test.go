package selectionbudget

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBudgetSharedScopesAndCleanup(t *testing.T) {
	ctx, b, cancel := Start(context.Background())
	defer cancel()
	nested, same, done := Start(ctx)
	defer done()
	if same != b {
		t.Fatal("nested selection got a new budget")
	}
	upper := Reserve(nested, 16)
	for i := 0; i < 48; i++ {
		if err := Take(upper, "sql", 0); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(Take(upper, "redis", 1), ErrExhausted) {
		t.Fatal("upper tier consumed reserve")
	}
	for i := 0; i < 16; i++ {
		if err := Take(ctx, "redis", 256); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(Take(ctx, "sql", 0), ErrExhausted) {
		t.Fatal("budget not enforced")
	}
	cancel()
	if err := Take(CleanupContext(ctx), "redis", 1); err != nil {
		t.Fatal(err)
	}
	got := b.Snapshot()
	if got.Operations != 64 || got.SQL != 48 || got.Commands != 4096 || got.Cleanup != 1 || !got.Exhausted {
		t.Fatalf("bad counters: %+v", got)
	}
}
func TestBudgetConcurrentLimit(t *testing.T) {
	ctx, b, cancel := Start(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() { _ = Take(ctx, "redis", 1) })
	}
	wg.Wait()
	if b.Snapshot().Operations != 64 {
		t.Fatal(b.Snapshot())
	}
}
