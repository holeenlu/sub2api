import type { AdminGroup } from '../groupTypes'

/** Complete synthetic DTO for settings readers; overrides stay type checked. */
export function adminGroupFixture(overrides: Partial<AdminGroup> = {}): AdminGroup {
  return {
    id: 7, name: 'Existing group', description: null, platform: 'openai',
    rate_multiplier: 0, is_exclusive: false, status: 'active', subscription_type: 'subscription',
    daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null,
    long_context_pricing_enabled: false,
    allow_image_generation: false, allow_batch_image_generation: false,
    image_rate_independent: false, image_rate_multiplier: 1,
    image_count_billing: false,
    batch_image_discount_multiplier: 1, batch_image_hold_multiplier: 1,
    image_price_1k: null, image_price_2k: null, image_price_4k: null,
    video_rate_independent: false, video_rate_multiplier: 1,
    video_price_480p: null, video_price_720p: null, video_price_1080p: null,
    web_search_price_per_call: null, search_price_per_1k: null,
    audio_realtime_price_per_min: null, audio_tts_price_per_million_chars: null,
    audio_stt_price_per_hour: null,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    claude_code_only: false, fallback_group_id: null, fallback_group_id_on_invalid_request: null,
    allow_live: false, require_oauth_only: false, require_privacy_set: false,
    created_at: '2026-10-27T00:00:00Z', updated_at: '2026-10-27T00:00:00Z',
    force_openai_fast: false, free_openai_fast: false, model_pricing: [],
    profit_control_enabled: false, profit_min_margin: 0, profit_safety_buffer: 0,
    model_routing: null, model_routing_enabled: false, mcp_xml_inject: false, sort_order: 0,
    ...overrides
  }
}
