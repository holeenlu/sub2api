package service

import "github.com/Wei-Shaw/sub2api/internal/billing"

// These are the original builder's policy inputs, not hydrated entities.
// Nil on historical intents means unavailable, never disabled or zero.
type NativeIntentRequestBillingPolicy struct {
	Version                int
	QuotaPlatform          string
	RequestPayloadHash     string
	IsSubscriptionBill     bool
	GroupImageCountBilling *bool
	KnownCanonical         *billing.Identity
	ChannelID              int64
	ModelMappingChain      string
	Hosted                 *NativeIntentHostedPricing
}

type NativeIntentAttemptBillingPolicy struct {
	Version                                                  int
	AccountType, AccountPlatform                             string
	CredentialOAuthLike                                      bool
	FreeFastEligible *bool
	OpenAICacheWriteTTL, CacheCreationAsInput                bool
	AccountQuotaHasDailyLimit, AccountQuotaHasWeeklyLimit    bool
	VendorIDSnapshot                                         *int64
	AccountNameSnapshot, GroupNameSnapshot, PlatformSnapshot *string
	Stats                                                    *NativeIntentStatsPricing
	// Only actual pricing candidates captured for this prepared attempt.
	// Model-independent tool/audio policy remains on Request.Billing.Hosted.
	HostedPrices map[string]*NativeIntentHostedPricing
}

// State distinguishes an observed absence of a channel/statistics override
// from a failed dependency read. The original statistics calculator consumes
// the selected custom/default cards, including its existing nil fallback.
type NativeIntentStatsState string

const (
	NativeIntentStatsNotApplicable NativeIntentStatsState = "not_applicable"
	NativeIntentStatsCaptured      NativeIntentStatsState = "captured"
)

type NativeIntentStatsPricing struct {
	State              NativeIntentStatsState
	Model              string
	CustomPricing      *ChannelModelPricing
	ApplyCustomerPrice bool
	DefaultPrice       *NativeResolvedPricingSnapshot
	LongContextEnabled bool
}

// Optional overrides retain nil versus explicit zero. Defaults must contain
// the actual prices resolved before send, not a flag to reload a live group.
// A missing policy for an observed hosted dimension remains pending.
type NativeIntentHostedPricing struct {
	Version                                                 int
	ImageOverrides, ImageDefaults                           *ImagePriceConfig
	VideoOverrides, VideoDefaults                           *VideoPriceConfig
	WebSearchPerCall, SearchPer1k                           *float64
	AudioRealtimePerMin, AudioTTSPerMChars, AudioSTTPerHour *float64
}
