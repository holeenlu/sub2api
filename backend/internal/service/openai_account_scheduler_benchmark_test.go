package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

func buildOpenAISchedulerBenchmarkCandidates(size int) []openAIAccountCandidateScore {
	if size <= 0 {
		return nil
	}
	candidates := make([]openAIAccountCandidateScore, 0, size)
	for i := 0; i < size; i++ {
		accountID := int64(10_000 + i)
		candidates = append(candidates, openAIAccountCandidateScore{
			account: &Account{
				ID:       accountID,
				Priority: i % 7,
			},
			loadInfo: &AccountLoadInfo{
				AccountID:    accountID,
				LoadRate:     (i * 17) % 100,
				WaitingCount: (i * 11) % 13,
			},
			score:     float64((i*29)%1000) / 100,
			errorRate: float64((i * 5) % 100 / 100),
			ttft:      float64(30 + (i*3)%500),
			hasTTFT:   i%3 != 0,
		})
	}
	return candidates
}

// Benchmark the production candidate planner, including full-pool sampling.
func BenchmarkOpenAIUnifiedLargePool(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("accounts_%d", size), func(b *testing.B) {
			candidates := buildOpenAISchedulerBenchmarkCandidates(size)
			scheduler := &defaultOpenAIAccountScheduler{}
			plan := openAIAccountLoadPlan{candidates: candidates, topK: 64}
			req := OpenAIAccountScheduleRequest{SessionHash: "benchmark", Platform: PlatformOpenAI}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(scheduler.buildOpenAISelectionOrder(req, plan)) == 0 {
					b.Fatal("empty plan")
				}
			}
		})
	}
}

// This replay exercises the whole selector with instrumented storage doubles.
// Delays and SQL/Redis executions are logical, not production network latency.
type replayConcurrencyCache struct {
	finalBudgetConcurrency
	full                       bool
	delay                      time.Duration
	probes, denied             int
	loadAccounts, maxLoadBatch int
}

func (c *replayConcurrencyCache) AcquireAccountSlot(ctx context.Context, id int64, limit int, requestID string) (bool, error) {
	acquired, err := c.finalBudgetConcurrency.AcquireAccountSlot(ctx, id, limit, requestID)
	c.probes++
	if err != nil || !acquired {
		c.denied++
	}
	return acquired, err
}

func (c *replayConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	c.loadAccounts += len(accounts)
	c.maxLoadBatch = max(c.maxLoadBatch, len(accounts))
	if c.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.delay):
		}
	}
	out, err := c.finalBudgetConcurrency.GetAccountsLoadBatch(ctx, accounts)
	if c.full {
		for _, info := range out {
			info.CurrentConcurrency = 10
			info.LoadRate = 100
		}
	}
	return out, err
}

type replayAccountRepo struct {
	finalBudgetAccountRepo
	vendorRejectBelow int64
}

func (r replayAccountRepo) ValidateVendorSupply(ctx context.Context, a *Account, _ *int64) error {
	if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
		return err
	}
	if a.ID <= r.vendorRejectBelow {
		return ErrVendorSupplyUnavailable
	}
	return nil
}
func (r replayAccountRepo) FilterVendorSupply(ctx context.Context, accounts []Account, _ *int64) ([]Account, error) {
	if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
		return nil, err
	}
	return accounts, nil
}

type replaySnapshotCache struct{ *openAISnapshotCacheStub }

func (c replaySnapshotCache) GetAccount(ctx context.Context, id int64) (*Account, error) {
	if err := selectionbudget.Take(ctx, "redis", 1); err != nil {
		return nil, err
	}
	return c.openAISnapshotCacheStub.GetAccount(ctx, id)
}
func BenchmarkSchedulerSelectionReplay(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		for _, scenario := range []string{"healthy", "full", "stale", "redis_delay", "vendor_fault"} {
			b.Run(fmt.Sprintf("%d/%s", size, scenario), func(b *testing.B) {
				accounts := make([]Account, size)
				snapshot := &openAISnapshotCacheStub{accountsByID: make(map[int64]*Account, size)}
				for i := range accounts {
					priority := 0
					if i >= 70 {
						priority = 1
					}
					accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10, Priority: priority}
					snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &accounts[i])
					snapshot.accountsByID[int64(i+1)] = &accounts[i]
				}
				cache := &replayConcurrencyCache{full: scenario == "full"}
				if scenario == "stale" {
					cache.acceptAfter = 70
				}
				if scenario == "redis_delay" {
					cache.delay = 5 * time.Millisecond
				}
				repo := replayAccountRepo{finalBudgetAccountRepo: finalBudgetAccountRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}}
				if scenario == "vendor_fault" {
					repo.vendorRejectBelow = 70
				}
				svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{cache: replaySnapshotCache{snapshot}, accountRepo: repo}, concurrencyService: NewConcurrencyService(cache)}
				svc.concurrencyService.SetAccountLoadBatchCacheTTL(0)
				scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
				latencies := make([]int64, 0, b.N)
				var sql, redis, commands, ops, exhausted int
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ctx, budget, cancel := selectionbudget.Start(context.Background())
					start := time.Now()
					result, _, err := scheduler.Select(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI})
					latencies = append(latencies, time.Since(start).Nanoseconds())
					if err != nil && !errors.Is(err, selectionbudget.ErrExhausted) {
						b.Fatal(err)
					}
					if errors.Is(err, selectionbudget.ErrExhausted) {
						exhausted++
					}
					if result != nil && result.ReleaseFunc != nil {
						result.ReleaseFunc()
					}
					cancel()
					state := budget.Snapshot()
					sql += state.SQL
					redis += state.Redis
					commands += state.Commands
					ops += state.Operations
					if state.Operations > 64 {
						b.Fatal(state)
					}
				}
				b.StopTimer()
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				if len(latencies) > 0 {
					b.ReportMetric(float64(latencies[min(len(latencies)-1, len(latencies)*95/100)])/1e6, "p95-ms")
					b.ReportMetric(float64(latencies[min(len(latencies)-1, len(latencies)*99/100)])/1e6, "p99-ms")
					b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "selections/s")
					b.ReportMetric(float64(sql)/float64(b.N), "sql/select")
					b.ReportMetric(float64(redis)/float64(b.N), "redis-exec/select")
					b.ReportMetric(float64(commands)/float64(b.N), "redis-cmd/select")
					b.ReportMetric(float64(ops)/float64(b.N), "ops/select")
					b.ReportMetric(float64(exhausted)/float64(b.N), "budget-exhaustion-ratio")
					b.ReportMetric(float64(cache.probes)/float64(b.N), "slot-probes/select")
					if cache.probes > 0 {
						b.ReportMetric(float64(cache.denied)/float64(cache.probes), "slot-failure-ratio")
					}
				}
			})
		}
	}
}
