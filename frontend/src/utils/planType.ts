/** OpenAI subscription SKU identity and Codex display names. Other providers use their own labels. */
export function normalizePlanType(value?: string | null): string {
  return (value || '').trim().toLowerCase().replace(/[\s_-]+/g, '')
}

/** Claude's rate tier distinguishes the two Max subscriptions. */
export function accountSubscriptionPlan(platform: string, plan?: string, tier?: string, subscription?: string): string {
  if (platform === 'anthropic') {
    const tiers: Record<string, string> = {
      default_claude_max_20x: 'Max 20x',
      default_claude_max_5x: 'Max 5x',
      default_claude_pro: 'Pro'
    }
    if (tier && tiers[tier]) return tiers[tier]
    const value = plan?.trim() || subscription?.trim() || ''
    return ({ claude_max: 'Max', claude_pro: 'Pro', claude_free: 'Free' } as Record<string, string>)[value] || value
  }
  return plan?.trim() || subscription?.trim() || ''
}

/** Canonical comparison key; labels may be shared by distinct SKUs. */
export function openAIPlanTypeKey(value?: string | null): string {
  const key = normalizePlanType(value)
  return key === 'chatgptpro' ? 'pro' : key
}

// Wire names from openai/codex b1e72963, protocol/src/account.rs.
export const openAIPlanTypes = [
  'free', 'go', 'plus', 'prolite', 'pro', 'promax', 'team',
  'self_serve_business_usage_based', 'self_serve_business_prolite', 'business',
  'enterprise', 'ent26', 'enterprise_cbp_usage_based', 'enterprise_cbp_automation',
  'edu', 'edu_plus', 'edu_pro', 'unknown'
] as const

/** Keep Status SKU names distinct from the account families used in analytics. */
export function openAIPlanTypeLabel(value?: string | null, display: 'status' | 'analytics' = 'status'): string {
  switch (openAIPlanTypeKey(value)) {
    case 'free': return 'Free'
    case 'go': return 'Go'
    case 'plus': return 'Plus'
    case 'prolite': return 'Pro 100'
    case 'pro': return 'Pro 200'
    case 'promax': return 'Pro 500'
    case 'team':
    case 'selfservebusinessusagebased': return 'Business'
    case 'business': return display === 'status' ? 'Enterprise' : 'Business'
    case 'selfservebusinessprolite': return display === 'status' ? 'Business Premium' : 'Business'
    case 'enterprisecbpautomation': return display === 'status' ? 'Enterprise (Automation)' : 'Enterprise'
    case 'enterprise':
    case 'ent26':
    case 'enterprisecbpusagebased': return 'Enterprise'
    case 'edu': return display === 'status' ? 'Edu' : 'Education'
    case 'eduplus': return display === 'status' ? 'Edu Plus' : 'Education'
    case 'edupro': return display === 'status' ? 'Edu Pro' : 'Education'
    case 'unknown': return display === 'status' ? 'Unknown' : 'Account'
    default: return ''
  }
}
