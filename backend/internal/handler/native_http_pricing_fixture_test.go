//go:build unit

package handler

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestNativeHTTPFixtureCapturesRealPricingIntentBeforeSend(t *testing.T) {
	upstream := &nativeHandlerSuccessUpstream{}
	h, store, transport := newNativeHTTPHandlerFixture(t, nativeHandlerFixtureAccounts(0), upstream)
	journal, ok := store.journal.(*wsNativeFixtureJournal)
	require.True(t, ok, "the default fixture must use the shared validating intent journal")
	var checked atomic.Int32
	transport.onOpen = func(_ *http.Request, _ native.TransportOptions) {
		journal.mu.Lock()
		defer journal.mu.Unlock()
		require.Len(t, journal.intents, 1, "the real runtime must preserve policy before native Open")
		for _, record := range journal.intents {
			require.Equal(t, service.UsageBillingModeLogOnly, record.Request.Mode)
			require.False(t, record.Request.PricingAt.IsZero())
			require.Len(t, record.Attempts, 1)
			attempt := record.Attempts[0]
			require.Equal(t, "http", attempt.Prepared.Transport)
			require.Equal(t, int64(1), attempt.Prepared.ResourceID)
			require.NotNil(t, attempt.FrozenPricing)
			require.NotNil(t, attempt.FrozenPricing.Resolved)
			require.NotNil(t, attempt.FrozenPricing.Resolved.BasePricing)
			// These are captured by the production BillingService/Pin path,
			// not synthetic prices or a fabricated successful monetary Apply.
			require.Positive(t, attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken)
			require.Positive(t, attempt.FrozenPricing.Resolved.BasePricing.OutputPricePerToken)
		}
		checked.Add(1)
	}
	entry := nativeHandlerEntries()[0]
	c, rec := nativeEntryContext(t, t.Context(), entry, 0)
	entry.call(h, c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.EqualValues(t, 1, checked.Load())
	require.EqualValues(t, 1, upstream.calls.Load())
	require.EqualValues(t, 1, transport.opened.Load())
	journal.assertNoLegacyApply(t)
}

func TestNativeHTTPFixtureMissingIntentOrUnhydratedPricingStillBlocksSend(t *testing.T) {
	for _, missing := range []string{"intent_port", "hydrated_pricing"} {
		t.Run(missing, func(t *testing.T) {
			upstream := &nativeHandlerSuccessUpstream{}
			cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 32 << 20}}
			var h *OpenAIGatewayHandler
			var transport *nativeHandlerTransport
			if missing == "intent_port" {
				// An explicit caller-supplied nil must not be replaced by the default.
				h, _, transport = newConfiguredNativeHTTPHandlerFixture(t, nativeHandlerFixtureAccounts(0), upstream, cfg, nil, nil)
			} else {
				h, _, transport = newNativeHTTPHandlerFixture(t, nativeHandlerFixtureAccounts(0), upstream)
			}
			entry := nativeHandlerEntries()[0]
			c, rec := nativeEntryContext(t, t.Context(), entry, 0)
			if missing == "hydrated_pricing" {
				key, ok := middleware.GetAPIKeyFromContext(c)
				require.True(t, ok)
				key.Group.Hydrated = false
			}
			entry.call(h, c)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "Native pricing intent could not be established")
			require.Zero(t, transport.opened.Load())
			require.Zero(t, upstream.calls.Load())
		})
	}
}
