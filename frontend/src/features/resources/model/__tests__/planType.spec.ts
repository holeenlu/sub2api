import { describe, expect, it } from 'vitest'
import { accountSubscriptionPlan, openAIPlanTypeLabel } from '../planType'

describe('accountSubscriptionPlan', () => {
  it.each([
    ['default_claude_max_5x', 'Max 5x'],
    ['default_claude_max_20x', 'Max 20x'],
    ['default_claude_pro', 'Pro']
  ])('uses the recorded Claude rate tier %s before generic plan metadata', (tier, label) => {
    expect(accountSubscriptionPlan('anthropic', 'claude_max', tier, 'claude_pro')).toBe(label)
    expect(accountSubscriptionPlan('anthropic', undefined, tier)).toBe(label)
  })

  it.each([
    ['claude_max', 'Max'],
    ['claude_pro', 'Pro'],
    ['claude_free', 'Free'],
    ['future_claude_plan', 'future_claude_plan']
  ])('keeps Claude plan %s distinct without a known rate tier', (plan, label) => {
    expect(accountSubscriptionPlan('anthropic', ` ${plan} `, 'future_tier')).toBe(label)
    expect(accountSubscriptionPlan('anthropic', ' ', undefined, ` ${plan} `)).toBe(label)
  })

  it('does not invent a Max multiplier or a plan for missing metadata', () => {
    expect(accountSubscriptionPlan('anthropic', undefined, undefined, 'claude_max')).toBe('Max')
    expect(accountSubscriptionPlan('anthropic')).toBe('')
    expect(accountSubscriptionPlan('anthropic', undefined, 'future_tier')).toBe('')
    expect(accountSubscriptionPlan('openai')).toBe('')
  })

  it.each(['openai', 'antigravity', 'grok'])('does not apply Claude rate tiers to %s', platform => {
    expect(accountSubscriptionPlan(platform, ' pro ', 'default_claude_max_20x', 'free')).toBe('pro')
    expect(accountSubscriptionPlan(platform, ' ', 'default_claude_pro', ' team ')).toBe('team')
  })

  it.each([
    ['pro', 'Pro 200'],
    ['prolite', 'Pro 100'],
    ['promax', 'Pro 500'],
    ['team', 'Business'],
    ['self_serve_business_prolite', 'Business Premium'],
    ['business', 'Enterprise']
  ])('preserves the OpenAI %s SKU for its own display mapper', (plan, label) => {
    const sku = accountSubscriptionPlan('openai', plan, 'default_claude_pro')
    expect(sku).toBe(plan)
    expect(openAIPlanTypeLabel(sku)).toBe(label)
  })
})
