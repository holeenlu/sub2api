package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCollectSelectionFailureStats(t *testing.T) {
	svc := &GatewayService{}
	model := "gpt-5.4"
	resetAt := time.Now().Add(2 * time.Minute).Format(time.RFC3339)

	accounts := []Account{
		// excluded
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
		},
		// unschedulable
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: false,
		},
		// platform filtered
		{
			ID:          3,
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
		},
		// model unsupported
		{
			ID:          4,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-image": "gpt-image",
				},
			},
		},
		// model rate limited
		{
			ID:          5,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					model: map[string]any{
						"rate_limit_reset_at": resetAt,
					},
				},
			},
		},
		// eligible
		{
			ID:          6,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
		},
	}

	excluded := map[int64]struct{}{1: {}}
	stats := svc.collectSelectionFailureStats(context.Background(), accounts, model, PlatformOpenAI, excluded, false)

	if stats.Total != 6 {
		t.Fatalf("total=%d want=6", stats.Total)
	}
	if stats.Excluded != 1 {
		t.Fatalf("excluded=%d want=1", stats.Excluded)
	}
	if stats.Unschedulable != 1 {
		t.Fatalf("unschedulable=%d want=1", stats.Unschedulable)
	}
	if stats.PlatformFiltered != 1 {
		t.Fatalf("platform_filtered=%d want=1", stats.PlatformFiltered)
	}
	if stats.ModelUnsupported != 1 {
		t.Fatalf("model_unsupported=%d want=1", stats.ModelUnsupported)
	}
	if stats.ModelRateLimited != 1 {
		t.Fatalf("model_rate_limited=%d want=1", stats.ModelRateLimited)
	}
	if stats.Eligible != 1 {
		t.Fatalf("eligible=%d want=1", stats.Eligible)
	}
}

func TestDiagnoseSelectionFailure_UnschedulableDetail(t *testing.T) {
	svc := &GatewayService{}
	acc := &Account{
		ID:          7,
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: false,
	}

	diagnosis := svc.diagnoseSelectionFailure(context.Background(), acc, "gpt-5.4", PlatformOpenAI, map[int64]struct{}{}, false)
	if diagnosis.Category != "unschedulable" {
		t.Fatalf("category=%s want=unschedulable", diagnosis.Category)
	}
	if diagnosis.Detail != "persistent_unschedulable" {
		t.Fatalf("detail=%s want=persistent_unschedulable", diagnosis.Detail)
	}
}

// 过载这类与限流无关的临时状态保留 generic 明细：只有账号级限流单独成类。
func TestDiagnoseSelectionFailure_OverloadStaysGenericUnschedulable(t *testing.T) {
	svc := &GatewayService{}
	overloadUntil := time.Now().Add(2 * time.Minute)
	acc := &Account{
		ID:            9,
		Platform:      PlatformOpenAI,
		Status:        StatusActive,
		Schedulable:   true,
		OverloadUntil: &overloadUntil,
	}

	diagnosis := svc.diagnoseSelectionFailure(context.Background(), acc, "gpt-5.4", PlatformOpenAI, map[int64]struct{}{}, false)
	if diagnosis.Category != "unschedulable" {
		t.Fatalf("category=%s want=unschedulable", diagnosis.Category)
	}
	if diagnosis.Detail != "generic_unschedulable" {
		t.Fatalf("detail=%s want=generic_unschedulable", diagnosis.Detail)
	}
}

func TestDiagnoseSelectionFailure_AccountCooldown(t *testing.T) {
	svc := &GatewayService{}
	resetAt := time.Now().Add(2 * time.Minute)
	acc := &Account{
		ID:               10,
		Platform:         PlatformOpenAI,
		Status:           StatusActive,
		Schedulable:      true,
		RateLimitResetAt: &resetAt,
	}

	diagnosis := svc.diagnoseSelectionFailure(context.Background(), acc, "gpt-5.4", PlatformOpenAI, map[int64]struct{}{}, false)
	if diagnosis.Category != "account_cooldown" {
		t.Fatalf("category=%s want=account_cooldown", diagnosis.Category)
	}
	if !strings.Contains(diagnosis.Detail, "remaining=") {
		t.Fatalf("detail=%s want contains remaining=", diagnosis.Detail)
	}
}

// 残留限流时间的账号若同时处于与限流无关的不可用状态，不能被记成临时冷却，
// 否则手动排空或额度耗尽的池子会伪装成"全池限流"。
func TestDiagnoseSelectionFailure_StaleRateLimitIsNotCooldownWhenOtherwiseUnavailable(t *testing.T) {
	svc := &GatewayService{}
	resetAt := time.Now().Add(2 * time.Minute)
	past := time.Now().Add(-time.Hour)

	cases := []struct {
		name       string
		acc        *Account
		wantDetail string
	}{
		{
			name: "手动停调",
			acc: &Account{
				ID: 11, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false,
				RateLimitResetAt: &resetAt,
			},
			wantDetail: "persistent_unschedulable",
		},
		{
			name: "已禁用",
			acc: &Account{
				ID: 12, Platform: PlatformOpenAI, Status: StatusDisabled, Schedulable: true,
				RateLimitResetAt: &resetAt,
			},
			wantDetail: "persistent_unschedulable",
		},
		{
			name: "到期自动暂停",
			acc: &Account{
				ID: 13, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
				AutoPauseOnExpired: true, ExpiresAt: &past,
				RateLimitResetAt: &resetAt,
			},
			wantDetail: "generic_unschedulable",
		},
		{
			name: "额度超限",
			acc: &Account{
				ID: 14, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
				Extra:            map[string]any{"quota_limit": 10.0, "quota_used": 10.0},
				RateLimitResetAt: &resetAt,
			},
			wantDetail: "generic_unschedulable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnosis := svc.diagnoseSelectionFailure(context.Background(), tc.acc, "gpt-5.4", PlatformOpenAI, map[int64]struct{}{}, false)
			if diagnosis.Category != "unschedulable" {
				t.Fatalf("category=%s want=unschedulable", diagnosis.Category)
			}
			if diagnosis.Detail != tc.wantDetail {
				t.Fatalf("detail=%s want=%s", diagnosis.Detail, tc.wantDetail)
			}
		})
	}
}

// 摘要里每一类的计数之和必须等于 total，account_cooldown 也在其中；
// 字段名不能含 rate_limited 子串，否则 handler 的正则会把它当成限流计数。
func TestSummarizeSelectionFailureStats_AccountCooldownCounted(t *testing.T) {
	svc := &GatewayService{}
	resetAt := time.Now().Add(2 * time.Minute)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, RateLimitResetAt: &resetAt},
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, RateLimitResetAt: &resetAt},
		{ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false, RateLimitResetAt: &resetAt},
		{ID: 4, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
	}

	stats := svc.collectSelectionFailureStats(context.Background(), accounts, "gpt-5.4", PlatformOpenAI, map[int64]struct{}{}, false)
	if stats.AccountCooldown != 2 {
		t.Fatalf("account_cooldown=%d want=2", stats.AccountCooldown)
	}
	if stats.Unschedulable != 1 {
		t.Fatalf("unschedulable=%d want=1", stats.Unschedulable)
	}
	sum := stats.Eligible + stats.Excluded + stats.Unschedulable + stats.AccountCooldown +
		stats.PlatformFiltered + stats.ModelUnsupported + stats.ModelRateLimited +
		stats.ProfitThreshold + stats.ProfitInvalidRate
	if sum != stats.Total {
		t.Fatalf("sum of categories=%d want total=%d", sum, stats.Total)
	}

	summary := summarizeSelectionFailureStats(stats)
	if !strings.Contains(summary, "account_cooldown=2") {
		t.Fatalf("summary=%q want contains account_cooldown=2", summary)
	}
	if !strings.Contains(summary, "model_rate_limited=0") {
		t.Fatalf("summary=%q want contains model_rate_limited=0", summary)
	}
	if strings.Count(summary, "rate_limited=") != 1 {
		t.Fatalf("summary=%q must expose exactly one rate_limited= field for the handler regex", summary)
	}
}

func TestDiagnoseSelectionFailure_ModelRateLimitedDetail(t *testing.T) {
	svc := &GatewayService{}
	model := "gpt-5.4"
	resetAt := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	acc := &Account{
		ID:          8,
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				model: map[string]any{
					"rate_limit_reset_at": resetAt,
				},
			},
		},
	}

	diagnosis := svc.diagnoseSelectionFailure(context.Background(), acc, model, PlatformOpenAI, map[int64]struct{}{}, false)
	if diagnosis.Category != "model_rate_limited" {
		t.Fatalf("category=%s want=model_rate_limited", diagnosis.Category)
	}
	if !strings.Contains(diagnosis.Detail, "remaining=") {
		t.Fatalf("detail=%s want contains remaining=", diagnosis.Detail)
	}
}
