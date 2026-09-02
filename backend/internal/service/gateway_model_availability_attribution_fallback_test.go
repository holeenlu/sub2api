//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiagnoseAcrossNoAccountFallbackPreciseAttribution(t *testing.T) {
	early := time.Now().Add(30 * time.Second)
	late := time.Now().Add(48 * time.Hour)
	fable := func(reset *time.Time) ModelAvailabilityDiagnosis {
		return ModelAvailabilityDiagnosis{
			HasAccountsInPool: true, HasModelSupport: true,
			AllModelCapableRateLimited: true, EarliestRateLimitResetAt: reset,
			RateLimit: &RateLimitAttribution{
				Scope: "model", Window: "7d_oi", Reason: AnthropicFableWindowExhaustedReason,
				Model: "claude-fable-5", ResetAt: reset,
			},
		}
	}
	ordinary := ModelAvailabilityDiagnosis{
		HasAccountsInPool: true, HasModelSupport: true,
		AllModelCapableRateLimited: true, EarliestRateLimitResetAt: &early,
	}
	tests := []struct {
		name    string
		pool    []ModelAvailabilityDiagnosis
		precise bool
		cooling bool
		reset   *time.Time
	}{
		{"fable then ordinary", []ModelAvailabilityDiagnosis{fable(&late), ordinary}, false, true, &early},
		{"ordinary then fable", []ModelAvailabilityDiagnosis{ordinary, fable(&late)}, false, true, &early},
		{"ordinary between fable pools", []ModelAvailabilityDiagnosis{fable(&late), ordinary, fable(&early)}, false, true, &early},
		{"all fable", []ModelAvailabilityDiagnosis{fable(&late), fable(&early)}, true, true, &early},
		{"unsupported hop", []ModelAvailabilityDiagnosis{fable(&late), {HasAccountsInPool: true}}, true, true, &late},
		{"live capable hop", []ModelAvailabilityDiagnosis{fable(&late), {HasAccountsInPool: true, HasModelSupport: true}}, false, false, &late},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := noAccountFallbackLinearChain(int64(len(tt.pool)), PlatformAnthropic)
			got := diagnoseAcrossNoAccountFallback(context.Background(),
				modelAvailabilityFallbackChain(t, 1, groups), tt.pool[0],
				func(_ context.Context, groupID *int64) ModelAvailabilityDiagnosis {
					return tt.pool[*groupID-1]
				})
			require.Equal(t, tt.cooling, got.AllModelCapableRateLimited)
			require.Equal(t, tt.reset, got.EarliestRateLimitResetAt)
			if tt.precise {
				require.NotNil(t, got.RateLimit)
				require.Equal(t, tt.reset, got.RateLimit.ResetAt)
			} else {
				require.Nil(t, got.RateLimit)
			}
		})
	}
}
