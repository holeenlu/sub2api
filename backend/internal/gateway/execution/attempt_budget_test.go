package execution

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

func testBudget() *AttemptBudget {
	return NewAttemptBudget(AttemptLimits{Total: 8, PerOwner: 3, Preparation: 2, RetryWait: 10 * time.Second})
}

func TestAttemptBudgetRedirectContinuationKeepsOwnerAndTargetBounds(t *testing.T) {
	b := NewAttemptBudget(AttemptLimits{Total: 3, PerOwner: 3})
	initial := model.AttemptTarget{Owner: model.ResourceOwner{AccountID: 1}, Mode: "http", Endpoint: "/start", Limit: 1}
	if err := b.Take(&initial); err != nil {
		t.Fatal(err)
	}
	retry := initial
	retry.Endpoint = "/other"
	if err := b.Take(&retry); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("normal no-retry policy bypassed: %v", err)
	}
	redirect := retry
	redirect.TransportContinuation = true
	if err := b.Take(&redirect); err != nil {
		t.Fatal(err)
	}
	if err := b.Take(&redirect); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("redirect target loop escaped: %v", err)
	}
	redirect.Endpoint = "/final"
	if err := b.Take(&redirect); err != nil {
		t.Fatal(err)
	}
	redirect.Endpoint = "/extra"
	if err := b.Take(&redirect); !errors.Is(err, ErrAttemptBudgetExhausted) {
		t.Fatalf("redirect reset operation budget: %v", err)
	}
	if got := b.Snapshot().Attempts; got != 3 {
		t.Fatalf("attempt count=%d, want 3", got)
	}
}

func TestAttemptBudgetSharesOwnerAcrossTargetRepairs(t *testing.T) {
	b := testBudget()
	target := model.AttemptTarget{Owner: model.ResourceOwner{AccountID: 1, VendorID: 2}, Mode: "native", Limit: 2}
	for range 2 {
		if err := b.Take(&target); err != nil {
			t.Fatal(err)
		}
	}
	before := b.Snapshot()
	if err := b.Take(&target); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("same target: %v", err)
	}
	if got := b.Snapshot(); got != before {
		t.Fatalf("denial spent allowance: %+v", got)
	}
	target.GrantDigest[0] = 1
	target.InputDigest[0] = 2
	if err := b.Take(&target); err != nil {
		t.Fatal(err)
	}
	target.Mode = "bps"
	if err := b.Take(&target); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("owner reset by mode: %v", err)
	}
	if b.OwnerAvailable(target.Owner) {
		t.Fatal("exhausted owner still eligible")
	}
	target.Owner.AccountID = 3
	if err := b.Take(&target); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot(); got.Attempts != 4 || got.Owners != 2 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestAttemptBudgetSingleAttemptOwnerCannotSwitchTarget(t *testing.T) {
	b := testBudget()
	target := model.AttemptTarget{Owner: model.ResourceOwner{AccountID: 1}, Limit: 1}
	if err := b.Take(&target); err != nil {
		t.Fatal(err)
	}
	target.GrantDigest[0] = 7
	if err := b.Take(&target); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("owner gained retry: %v", err)
	}
	if b.Snapshot().Attempts != 1 {
		t.Fatal("rejected repair consumed global budget")
	}
}

func TestAttemptBudgetConcurrentConsumersShareTotal(t *testing.T) {
	b := testBudget()
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			target := model.AttemptTarget{Owner: model.ResourceOwner{AccountID: int64(i + 1)}, Limit: 2}
			if err := b.Take(&target); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrAttemptBudgetExhausted) {
				t.Errorf("unexpected denial: %v", err)
			}
			_ = b.Snapshot()
		}()
	}
	wg.Wait()
	got := b.Snapshot()
	if successes.Load() != 8 || got.Attempts != 8 || got.Owners != 8 {
		t.Fatalf("attempts escaped cap: %d %+v", successes.Load(), got)
	}
}

func TestAttemptBudgetPreparationAndWaitAreIndependent(t *testing.T) {
	b := testBudget()
	for range 2 {
		if err := b.TakePreparation(); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.TakePreparation(); !errors.Is(err, ErrPreparationBudgetExhausted) {
		t.Fatalf("preparation: %v", err)
	}
	delay, err := b.ReserveWait(9*time.Second, 2*time.Second)
	if err != nil || delay != 10*time.Second {
		t.Fatalf("jitter must clip: %v %v", delay, err)
	}
	if _, err := b.ReserveWait(time.Nanosecond, 0); !errors.Is(err, ErrRetryWaitBudgetExhausted) {
		t.Fatalf("wait: %v", err)
	}
	for range 8 {
		if err := b.Take(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Take(nil); !errors.Is(err, ErrAttemptBudgetExhausted) {
		t.Fatalf("total: %v", err)
	}
	if got := b.Snapshot(); got.PreparationRetries != 2 || got.RetryWait != 10*time.Second {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestAttemptBudgetZeroValueRejectsWork(t *testing.T) {
	var b AttemptBudget
	if err := b.Take(nil); !errors.Is(err, ErrAttemptBudgetExhausted) {
		t.Fatalf("zero total: %v", err)
	}
	if err := b.TakePreparation(); !errors.Is(err, ErrPreparationBudgetExhausted) {
		t.Fatalf("zero preparation: %v", err)
	}
	if _, err := b.ReserveWait(time.Nanosecond, 0); !errors.Is(err, ErrRetryWaitBudgetExhausted) {
		t.Fatalf("zero wait: %v", err)
	}
	if b.OwnerAvailable(model.ResourceOwner{}) {
		t.Fatal("zero owner limit permits work")
	}
	if got := b.Snapshot(); got != (BudgetSnapshot{}) {
		t.Fatalf("zero budget mutated: %+v", got)
	}
}

func TestAttemptBudgetInvalidTargetDoesNotSpendAllowance(t *testing.T) {
	b := testBudget()
	if err := b.Take(&model.AttemptTarget{}); !errors.Is(err, ErrTargetBudgetExhausted) {
		t.Fatalf("zero target limit: %v", err)
	}
	if got := b.Snapshot(); got.Attempts != 0 || got.Owners != 0 {
		t.Fatalf("invalid target spent allowance: %+v", got)
	}
	delay, err := b.ReserveWait(time.Second, -time.Second)
	if err != nil || delay != time.Second {
		t.Fatalf("negative jitter shortens delay: %v %v", delay, err)
	}
}
