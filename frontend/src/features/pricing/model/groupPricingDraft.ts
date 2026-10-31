import { cloneDraftValue } from '@/shared/lib/useDraftSection'
import type { AdminGroup, GroupPlatform } from '@/features/access-policy/api/groupTypes'
import type { ChannelModelPricing } from '../api/types'
import type { PricingFormEntry } from './pricingForm'
import { apiIntervalsToForm, createDefaultTimePricingForm, formIntervalsToAPI, formReasoningEffortMultipliersToAPI, mTokToPerToken, perTokenToMTok, toNullableNumber } from './pricingForm'
import { createVideoModelPricesForm, serializeVideoModelPrices } from './groupsVideoModelPricing'
import { isProfitControlPlatform, profitPercentToDecimal, profitDecimalToPercent } from './groupsProfitControl'
import { canConfigureImageCountBilling } from './groupsImagePricing'
const decimalToPercent = profitDecimalToPercent
export function createGroupPricingDraft() {
 return {
  rate_multiplier: 1.0,
  long_context_pricing_enabled: true,
  free_openai_fast: false,
  model_pricing: [] as PricingFormEntry[],
  image_count_billing: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null as number | null,
  image_price_2k: null as number | null,
  image_price_4k: null as number | null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null as number | null,
  video_price_720p: null as number | null,
  video_price_1080p: null as number | null,
  video_model_prices: createVideoModelPricesForm(),
  web_search_price_per_call: null as number | null,
  search_price_per_1k: null as number | null,
  audio_realtime_price_per_min: null as number | null,
  audio_tts_price_per_million_chars: null as number | null,
  audio_stt_price_per_hour: null as number | null,
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1.0,
  profit_control_enabled: false,
  profit_min_margin_percent: 0,
  profit_safety_buffer_percent: 0
 }
}
export type GroupPricingDraft = ReturnType<typeof createGroupPricingDraft>

const groupPricingFromAPI = (
  pricing: ChannelModelPricing[] | undefined,
): PricingFormEntry[] =>
  (pricing || []).map((entry) => ({
    models: [...(entry.models || [])],
    billing_mode: entry.billing_mode || "token",
    input_price: perTokenToMTok(entry.input_price),
    output_price: perTokenToMTok(entry.output_price),
    cache_write_price: perTokenToMTok(entry.cache_write_price),
    cache_write_1h_price: perTokenToMTok(entry.cache_write_1h_price),
    cache_read_price: perTokenToMTok(entry.cache_read_price),
    reasoning_effort_multipliers: entry.reasoning_effort_multipliers
      ? { ...entry.reasoning_effort_multipliers }
      : null,
    image_input_price: perTokenToMTok(entry.image_input_price),
    image_output_price: perTokenToMTok(entry.image_output_price),
    per_request_price: entry.per_request_price,
    intervals: apiIntervalsToForm(entry.intervals || []),
    time_pricing: createDefaultTimePricingForm(),
  }));
const groupPricingToAPI = (
  pricing: PricingFormEntry[],
  platform: string,
): ChannelModelPricing[] =>
  pricing
    .filter((entry) => entry.models.length > 0)
    .map((entry) => ({
      platform,
      models: entry.models,
      billing_mode: entry.billing_mode,
      input_price: mTokToPerToken(entry.input_price),
      output_price: mTokToPerToken(entry.output_price),
      cache_write_price: mTokToPerToken(entry.cache_write_price),
      cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price),
      cache_read_price: mTokToPerToken(entry.cache_read_price),
      reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(
        entry.reasoning_effort_multipliers,
      ),
      image_input_price: mTokToPerToken(entry.image_input_price),
      image_output_price: mTokToPerToken(entry.image_output_price),
      per_request_price: toNullableNumber(entry.per_request_price),
      intervals:
        entry.billing_mode === "token"
          ? []
          : formIntervalsToAPI(entry.intervals || []),
      time_pricing: null,
    }));
const normalizeRateMultiplier = (
  value: number | string | null | undefined,
): number => {
  if (value === null || value === undefined || value === "") {
    return 1;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 1;
};
export function groupPricingDraftFromAPI(group: Readonly<AdminGroup>): GroupPricingDraft {
 const draft = createGroupPricingDraft()
draft.rate_multiplier = group.rate_multiplier;
draft.long_context_pricing_enabled =
    group.long_context_pricing_enabled ?? true;
draft.free_openai_fast = group.free_openai_fast ?? false;
draft.model_pricing = groupPricingFromAPI(group.model_pricing);
draft.image_count_billing = (group.image_count_billing ?? false) && canConfigureImageCountBilling(group.platform, group.allow_image_generation);
draft.image_rate_independent = group.image_rate_independent ?? false;
draft.image_rate_multiplier = group.image_rate_multiplier ?? 1;
draft.batch_image_discount_multiplier =
    group.batch_image_discount_multiplier ?? 0.5;
draft.batch_image_hold_multiplier = group.batch_image_hold_multiplier ?? 0.6;
draft.image_price_1k = group.image_price_1k;
draft.image_price_2k = group.image_price_2k;
draft.image_price_4k = group.image_price_4k;
draft.video_rate_independent = group.video_rate_independent ?? false;
draft.video_rate_multiplier = group.video_rate_multiplier ?? 1;
draft.video_price_480p = group.video_price_480p;
draft.video_price_720p = group.video_price_720p;
draft.video_price_1080p = group.video_price_1080p;
draft.video_model_prices = createVideoModelPricesForm(
    group.video_model_prices,
  );
draft.web_search_price_per_call = group.web_search_price_per_call ?? null;
draft.search_price_per_1k = group.search_price_per_1k ?? null;
draft.audio_realtime_price_per_min = group.audio_realtime_price_per_min ?? null;
draft.audio_tts_price_per_million_chars = group.audio_tts_price_per_million_chars ?? null;
draft.audio_stt_price_per_hour = group.audio_stt_price_per_hour ?? null;
draft.peak_rate_enabled = group.peak_rate_enabled ?? false;
draft.peak_start = group.peak_start ?? "";
draft.peak_end = group.peak_end ?? "";
draft.peak_rate_multiplier = group.peak_rate_multiplier ?? 1.0;
draft.profit_control_enabled = group.profit_control_enabled ?? false;
draft.profit_min_margin_percent = decimalToPercent(
    group.profit_min_margin ?? 0,
  );
draft.profit_safety_buffer_percent = decimalToPercent(
    group.profit_safety_buffer ?? 0,
  );
 return draft
}
export function projectGroupPricingDraft(value: Readonly<GroupPricingDraft>): GroupPricingDraft {
 const keys = Object.keys(createGroupPricingDraft()) as Array<keyof GroupPricingDraft>
 return Object.fromEntries(keys.map(key => [key, cloneDraftValue(value[key])])) as GroupPricingDraft
}

/** Admission facts, not a second eligibility decision or authoritative charge calculation. */
export interface GroupPricingContext {
 readonly platform: GroupPlatform
 readonly imageGenerationAllowed: boolean
 readonly openAIFastSupported: boolean
 readonly batchImageAllowed: boolean
}
export function resetGroupBatchPricing() { return { batch_image_discount_multiplier: 0.5, batch_image_hold_multiplier: 0.6 } }
export function resetGroupPeakPricing() { return { peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1.0 } }
export function buildGroupPricingPayload(value: Readonly<GroupPricingDraft>, context: GroupPricingContext, updating: boolean) {
 const draft = projectGroupPricingDraft(value)
 const { profit_min_margin_percent, profit_safety_buffer_percent, video_model_prices, ...fields } = draft
 const video = serializeVideoModelPrices(video_model_prices)
 const price = (value: number | string | null | undefined): number | null =>
  value === '' || value == null ? updating ? -1 : null : Number(value)
 const batch = context.batchImageAllowed ? draft : resetGroupBatchPricing()
 return cloneDraftValue({
  ...fields,
  image_count_billing: draft.image_count_billing && canConfigureImageCountBilling(context.platform, context.imageGenerationAllowed),
  free_openai_fast: context.openAIFastSupported && draft.free_openai_fast,
  model_pricing: groupPricingToAPI(draft.model_pricing, context.platform),
  ...(updating || Object.keys(video).length ? { video_model_prices: video } : {}),
  profit_control_enabled: isProfitControlPlatform(context.platform) && draft.profit_control_enabled,
  profit_min_margin: profitPercentToDecimal(profit_min_margin_percent),
  profit_safety_buffer: profitPercentToDecimal(profit_safety_buffer_percent),
  image_rate_multiplier: normalizeRateMultiplier(draft.image_rate_multiplier),
  batch_image_discount_multiplier: normalizeRateMultiplier(batch.batch_image_discount_multiplier),
  batch_image_hold_multiplier: normalizeRateMultiplier(batch.batch_image_hold_multiplier),
  video_rate_multiplier: normalizeRateMultiplier(draft.video_rate_multiplier),
  image_price_1k: price(draft.image_price_1k), image_price_2k: price(draft.image_price_2k), image_price_4k: price(draft.image_price_4k),
  video_price_480p: price(draft.video_price_480p), video_price_720p: price(draft.video_price_720p), video_price_1080p: price(draft.video_price_1080p),
  web_search_price_per_call: price(draft.web_search_price_per_call), search_price_per_1k: price(draft.search_price_per_1k),
  audio_realtime_price_per_min: price(draft.audio_realtime_price_per_min), audio_tts_price_per_million_chars: price(draft.audio_tts_price_per_million_chars), audio_stt_price_per_hour: price(draft.audio_stt_price_per_hour),
  peak_rate_multiplier: normalizeRateMultiplier(draft.peak_rate_multiplier),
 })
}

export function groupPricingAfterPlatformChange(value: Readonly<GroupPricingDraft>, platform: string): GroupPricingDraft {
 const draft = projectGroupPricingDraft(value)
 if (!isProfitControlPlatform(platform)) {
  draft.profit_control_enabled = false
  draft.profit_min_margin_percent = 0
  draft.profit_safety_buffer_percent = 0
 }
 return draft
}
