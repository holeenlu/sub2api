package service

// ModelQuote is a standard-period quote. Pricing already includes RateMultiplier;
// context tiers, reasoning tiers and time windows remain explicit conditions.
type ModelQuote struct {
	Unit              string
	ImageTokenPricing *PlazaImageTokenPricing
	Status            string
	Scope             string
	Source            string
	Reason            string
	RateMultiplier    float64
	Pricing           *ChannelModelPricing
	Conditions        []string
}

func QuotePlazaModel(model *PlazaModel, group *PlazaGroup, personalRate *float64, personalUnavailable bool) ModelQuote {
	quote := ModelQuote{Unit: model.PriceUnit, Status: "resolved", Scope: "group", Source: model.PricingSource, RateMultiplier: group.RateMultiplier, Conditions: []string{}}
	if personalRate != nil {
		quote.Scope = "personal"
		quote.RateMultiplier = *personalRate
	}
	if personalUnavailable {
		quote.Scope = "group_fallback"
		quote.Reason = "personal_rate_unavailable"
	}
	if model.QuoteReason != "" {
		quote.Status = "conditional"
		quote.Reason = model.QuoteReason
		return quote
	}
	if model.Pricing == nil || (pricingNeedsFallback(model.Pricing) && model.Pricing.ImageInputPrice == nil) {
		quote.Status = "unavailable"
		quote.Reason = "pricing_unavailable"
		return quote
	}
	p := model.Pricing.Clone()
	key := &APIKey{Group: &Group{ImageRateIndependent: group.ImageRateIndependent, ImageRateMultiplier: group.ImageRateMultiplier, VideoRateIndependent: group.VideoRateIndependent, VideoRateMultiplier: group.VideoRateMultiplier}}
	mode := p.BillingMode
	if model.MediaKind != "" {
		mode = model.MediaKind
	}
	switch mode {
	case BillingModeImage:
		quote.RateMultiplier = resolveImageRateMultiplier(key, quote.RateMultiplier)
	case BillingModeVideo:
		quote.RateMultiplier = resolveVideoRateMultiplier(key, quote.RateMultiplier)
	}
	if quote.Unit == "" {
		switch p.BillingMode {
		case BillingModeImage:
			quote.Unit = "image"
		case BillingModeVideo:
			quote.Unit = "second"
		case BillingModePerRequest:
			quote.Unit = "request"
		default:
			quote.Unit = "token"
		}
	}
	if quote.Unit == "second" {
		quote.Conditions = append(quote.Conditions, "duration")
	}
	rate := quote.RateMultiplier
	scale := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		n := *v * rate
		return &n
	}
	if image := model.ImageTokenPricing; image != nil {
		quote.ImageTokenPricing = &PlazaImageTokenPricing{Input: scale(image.Input), Output: scale(image.Output), CacheRead: scale(image.CacheRead)}
	}
	p.InputPrice = scale(p.InputPrice)
	p.OutputPrice = scale(p.OutputPrice)
	p.CacheWritePrice = scale(p.CacheWritePrice)
	p.CacheWrite1hPrice = scale(p.CacheWrite1hPrice)
	p.CacheReadPrice = scale(p.CacheReadPrice)
	p.ImageInputPrice = scale(p.ImageInputPrice)
	p.ImageOutputPrice = scale(p.ImageOutputPrice)
	p.PerRequestPrice = scale(p.PerRequestPrice)
	for i := range p.Intervals {
		iv := &p.Intervals[i]
		iv.InputPrice = scale(iv.InputPrice)
		iv.OutputPrice = scale(iv.OutputPrice)
		iv.CacheWritePrice = scale(iv.CacheWritePrice)
		iv.CacheWrite1hPrice = scale(iv.CacheWrite1hPrice)
		iv.CacheReadPrice = scale(iv.CacheReadPrice)
		iv.PerRequestPrice = scale(iv.PerRequestPrice)
	}
	if (p.BillingMode == "" || p.BillingMode == BillingModeToken) && (p.InputPrice == nil || p.OutputPrice == nil) {
		quote.Conditions = append(quote.Conditions, "partial_price")
	}
	if p.FastMultiplier != nil || p.FlexMultiplier != nil {
		quote.Conditions = append(quote.Conditions, "service_tier")
	}
	if len(p.Intervals) > 0 {
		quote.Conditions = append(quote.Conditions, "tiers")
	}
	if len(p.ReasoningEffortMultipliers) > 0 {
		quote.Conditions = append(quote.Conditions, "reasoning_effort")
	}
	if model.TimePricing != nil {
		quote.Conditions = append(quote.Conditions, "time_windows")
	}
	if group.PeakRateEnabled && (p.BillingMode == "" || p.BillingMode == BillingModeToken) {
		quote.Conditions = append(quote.Conditions, "peak_window")
	}
	if len(quote.Conditions) > 0 {
		quote.Status = "conditional"
	}
	quote.Pricing = &p
	return quote
}
