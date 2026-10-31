import type { Channel, ChannelModelPricing, AccountStatsPricingRule } from '../api/types'
import type { AdminGroup, GroupPlatform } from '@/features/access-policy/api/groupTypes';
import type { PricingFormEntry } from '@/features/pricing/model/pricingForm'
import { apiIntervalsToForm, apiTimePricingToForm, createDefaultTimePricingForm, formIntervalsToAPI, formReasoningEffortMultipliersToAPI, formTimePricingToAPI, mTokToPerToken, perTokenToMTok } from '@/features/pricing/model/pricingForm'
// ── Form-level pricing rule type (per-platform) ──
export interface FormPricingRule {
  name: string
  group_ids: number[]
  account_ids: number[]
  pricing: PricingFormEntry[]
}

// ── Platform Section type ──
export interface PlatformSection {
  platform: GroupPlatform
  enabled: boolean
  collapsed: boolean
  group_ids: number[]
  model_mapping: Record<string, string>
  model_pricing: PricingFormEntry[]
  web_search_emulation: boolean
  codex_image_generation_bridge: boolean
  bedrock_cc_compat: boolean
  account_stats_pricing_rules: FormPricingRule[]
}

export function accountStatsRulesToAPI(platforms: PlatformSection[]): AccountStatsPricingRule[] {
  const rules: AccountStatsPricingRule[] = []
  for (const section of platforms) {
    if (!section.enabled) continue
    for (const rule of section.account_stats_pricing_rules) {
      rules.push({
        name: rule.name,
        group_ids: rule.group_ids,
        account_ids: rule.account_ids,
        pricing: rule.pricing
          .filter(p => p.models.length > 0)
          .map(p => ({
            platform: section.platform,
            models: p.models,
            billing_mode: p.billing_mode,
            input_price: mTokToPerToken(p.input_price),
            output_price: mTokToPerToken(p.output_price),
            cache_write_price: mTokToPerToken(p.cache_write_price),
            cache_write_1h_price: mTokToPerToken(p.cache_write_1h_price),
            cache_read_price: mTokToPerToken(p.cache_read_price),
            reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(p.reasoning_effort_multipliers),
            image_input_price: mTokToPerToken(p.image_input_price),
            image_output_price: mTokToPerToken(p.image_output_price),
            per_request_price: p.per_request_price != null && p.per_request_price !== '' ? Number(p.per_request_price) : null,
            intervals: formIntervalsToAPI(p.intervals || []),
            time_pricing: null
          }))
      })
    }
  }
  return rules
}

// ── Form ↔ API conversion ──
export function channelDraftToAPI(platforms: PlatformSection[], channel: Channel | null): { group_ids: number[], model_pricing: ChannelModelPricing[], model_mapping: Record<string, Record<string, string>>, features_config: Record<string, unknown> } {
  const group_ids: number[] = []
  const model_pricing: ChannelModelPricing[] = []
  const model_mapping: Record<string, Record<string, string>> = {}
  // Preserve existing features_config fields not managed by the form
  const featuresConfig: Record<string, unknown> = channel?.features_config
    ? { ...channel.features_config }
    : {}

  for (const section of platforms) {
    if (!section.enabled) continue
    group_ids.push(...section.group_ids)

    // Model mapping per platform
    if (Object.keys(section.model_mapping).length > 0) {
      model_mapping[section.platform] = { ...section.model_mapping }
    }

    // Model pricing with platform tag
    for (const entry of section.model_pricing) {
      if (entry.models.length === 0) continue
      model_pricing.push({
        platform: section.platform,
        models: entry.models,
        billing_mode: entry.billing_mode,
        input_price: mTokToPerToken(entry.input_price),
        output_price: mTokToPerToken(entry.output_price),
        cache_write_price: mTokToPerToken(entry.cache_write_price),
        cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price),
        cache_read_price: mTokToPerToken(entry.cache_read_price),
        fast_multiplier: entry.fast_multiplier != null && entry.fast_multiplier !== '' ? Number(entry.fast_multiplier) : null,
        flex_multiplier: entry.flex_multiplier != null && entry.flex_multiplier !== '' ? Number(entry.flex_multiplier) : null,
        reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(entry.reasoning_effort_multipliers),
        image_input_price: mTokToPerToken(entry.image_input_price),
        image_output_price: mTokToPerToken(entry.image_output_price),
        per_request_price: entry.per_request_price != null && entry.per_request_price !== '' ? Number(entry.per_request_price) : null,
        intervals: formIntervalsToAPI(entry.intervals || []),
        time_pricing: formTimePricingToAPI(entry.time_pricing)
      })
    }
  }
  const uniqueGroupIds = Array.from(new Set(group_ids))

  // Collect web_search_emulation for the supported gateway platforms.
  // Always write the key so that disabling in the UI correctly sets platform to false,
  // rather than leaving a stale true value from the cloned features_config.
  const wsEmulation: Record<string, boolean> = {}
  for (const section of platforms) {
    if (!section.enabled) continue
    if (section.platform === 'anthropic' || section.platform === 'openai') {
      wsEmulation[section.platform] = !!section.web_search_emulation
    }
  }
  if (Object.keys(wsEmulation).length > 0) {
    featuresConfig.web_search_emulation = wsEmulation
  } else {
    delete featuresConfig.web_search_emulation
  }

  const codexImageGenerationBridge: Record<string, boolean> = {}
  for (const section of platforms) {
    if (!section.enabled) continue
    if (section.platform === 'openai') {
      codexImageGenerationBridge[section.platform] = !!section.codex_image_generation_bridge
    }
  }
  if (Object.keys(codexImageGenerationBridge).length > 0) {
    featuresConfig.codex_image_generation_bridge = codexImageGenerationBridge
  } else {
    delete featuresConfig.codex_image_generation_bridge
  }

  const bedrockCCCompat: Record<string, boolean> = {}
  for (const section of platforms) {
    if (!section.enabled) continue
    if (section.platform === 'anthropic') {
      bedrockCCCompat[section.platform] = !!section.bedrock_cc_compat
    }
  }
  if (Object.keys(bedrockCCCompat).length > 0) {
    featuresConfig.bedrock_cc_compat = bedrockCCCompat
  } else {
    delete featuresConfig.bedrock_cc_compat
  }

  return { group_ids: uniqueGroupIds, model_pricing, model_mapping, features_config: featuresConfig }
}

export function channelToSections(channel: Channel, groups: AdminGroup[], platformOrder: GroupPlatform[]): PlatformSection[] {
  // Build a map: groupID → platform
  const groupPlatformMap = new Map<number, GroupPlatform>()
  for (const g of groups) {
    groupPlatformMap.set(g.id, g.platform)
  }

  // Determine which platforms are active (from groups + pricing + mapping)
  const activePlatforms = new Set<GroupPlatform>()
  for (const gid of channel.group_ids || []) {
    const p = groupPlatformMap.get(gid)
    if (p === 'composite') {
      platformOrder.forEach(platform => activePlatforms.add(platform))
    } else if (p) {
      activePlatforms.add(p)
    }
  }
  for (const p of channel.model_pricing || []) {
    if (p.platform) activePlatforms.add(p.platform as GroupPlatform)
  }
  for (const p of Object.keys(channel.model_mapping || {})) {
    activePlatforms.add(p as GroupPlatform)
  }

  // Build sections in platform order
  const sections: PlatformSection[] = []
  for (const platform of [...new Set([...platformOrder, ...activePlatforms])]) {
    if (!activePlatforms.has(platform)) continue

    const groupIds = (channel.group_ids || []).filter(gid => {
      const groupPlatform = groupPlatformMap.get(gid)
      return groupPlatform === platform ||
        (groupPlatform === 'composite' && platformOrder.includes(platform))
    })
    const mapping = (channel.model_mapping || {})[platform] || {}
    const pricing = (channel.model_pricing || [])
      .filter(p => (p.platform || 'anthropic') === platform)
      .map(p => ({
        models: p.models || [],
        billing_mode: p.billing_mode,
        input_price: perTokenToMTok(p.input_price),
        output_price: perTokenToMTok(p.output_price),
        cache_write_price: perTokenToMTok(p.cache_write_price),
        cache_write_1h_price: perTokenToMTok(p.cache_write_1h_price),
        cache_read_price: perTokenToMTok(p.cache_read_price),
        fast_multiplier: p.fast_multiplier,
        flex_multiplier: p.flex_multiplier,
        reasoning_effort_multipliers: p.reasoning_effort_multipliers ? { ...p.reasoning_effort_multipliers } : null,
        image_input_price: perTokenToMTok(p.image_input_price),
        image_output_price: perTokenToMTok(p.image_output_price),
        per_request_price: p.per_request_price,
        intervals: apiIntervalsToForm(p.intervals || []),
        time_pricing: apiTimePricingToForm(p.time_pricing)
      } as PricingFormEntry))

    // Read web_search_emulation from features_config
    const fc = channel.features_config
    const wsEmulation = fc?.web_search_emulation as Record<string, boolean> | undefined
    const webSearchEnabled = wsEmulation?.[platform] === true
    const codexImageGenerationBridge = fc?.codex_image_generation_bridge as Record<string, boolean> | undefined
    const codexImageGenerationBridgeEnabled = codexImageGenerationBridge?.[platform] === true
    const bedrockCompat = fc?.bedrock_cc_compat
    const bedrockCCCompatEnabled = bedrockCompat === true || (typeof bedrockCompat === 'object' && bedrockCompat !== null && (bedrockCompat as Record<string, unknown>)[platform] === true)

    sections.push({
      platform,
      enabled: true,
      collapsed: false,
      group_ids: groupIds,
      model_mapping: { ...mapping },
      model_pricing: pricing,
      web_search_emulation: webSearchEnabled,
      codex_image_generation_bridge: codexImageGenerationBridgeEnabled,
      bedrock_cc_compat: bedrockCCCompatEnabled,
      account_stats_pricing_rules: [],
    })
  }

  return sections
}

export function distributeRulesToPlatforms(apiRules: AccountStatsPricingRule[], groups: AdminGroup[], sections: PlatformSection[]) {
  // Build groupID → platform lookup
  const groupPlatformMap = new Map<number, GroupPlatform>()
  for (const g of groups) {
    groupPlatformMap.set(g.id, g.platform)
  }

  for (const apiRule of apiRules) {
    // Infer platform from group_ids
    const platforms = new Set<GroupPlatform>()
    for (const gid of apiRule.group_ids || []) {
      const p = groupPlatformMap.get(gid)
      if (p && p !== 'composite') platforms.add(p)
    }
    // If pricing has a platform field, use that as fallback
    if (platforms.size === 0 && apiRule.pricing?.length > 0) {
      const p = apiRule.pricing[0].platform as GroupPlatform | undefined
      if (p) platforms.add(p)
    }
    const targetPlatform = platforms.size >= 1 ? [...platforms][0] : null
    if (!targetPlatform) continue

    const section = sections.find(s => s.platform === targetPlatform)
    if (!section) continue

    const formRule: FormPricingRule = {
      name: apiRule.name || '',
      group_ids: [...(apiRule.group_ids || [])],
      account_ids: [...(apiRule.account_ids || [])],
      pricing: (apiRule.pricing || []).map(p => ({
        models: [...(p.models || [])],
        billing_mode: p.billing_mode,
        input_price: perTokenToMTok(p.input_price),
        output_price: perTokenToMTok(p.output_price),
        cache_write_price: perTokenToMTok(p.cache_write_price),
        cache_write_1h_price: perTokenToMTok(p.cache_write_1h_price),
        cache_read_price: perTokenToMTok(p.cache_read_price),
        reasoning_effort_multipliers: p.reasoning_effort_multipliers ? { ...p.reasoning_effort_multipliers } : null,
        image_input_price: perTokenToMTok(p.image_input_price),
        image_output_price: perTokenToMTok(p.image_output_price),
        per_request_price: p.per_request_price,
        intervals: apiIntervalsToForm(p.intervals || []),
        time_pricing: createDefaultTimePricingForm()
      } as PricingFormEntry))
    }
    section.account_stats_pricing_rules.push(formRule)
  }
}

