import type { AccountPlatform } from '../api/platformTypes'

export interface BulkEditPermissions {
  readonly allowAuthorize: boolean
  readonly allowTransportEdit: boolean
  readonly allowRateEdit: boolean
  readonly supplierScope?: boolean
}

export interface BulkTargetEligibility {
  readonly managedModelReferences: boolean
  readonly mixedModelReferences: boolean
  readonly openAIPassthrough: boolean
  readonly openAIOAuth: boolean
  readonly openAIOAuthOnly: boolean
  readonly openAIAPIKey: boolean
  readonly billingProbe: boolean
  readonly headerOverride: boolean
  readonly anthropicOAuth: boolean
}

/** UI visibility and outgoing patches share the same conservative selection projection. */
export function bulkTargetEligibility(
  platforms: readonly AccountPlatform[],
  types: readonly string[],
  isHeaderOverrideCapable: (platform: AccountPlatform, type: string) => boolean
): BulkTargetEligibility {
  const allTypes = (allowed: readonly string[]) =>
    types.length > 0 && types.every(type => allowed.includes(type))
  const openAI = platforms.length === 1 && platforms[0] === 'openai'
  const managed = platforms.filter(platform => platform === 'anthropic' || platform === 'openai')
  return {
    managedModelReferences: platforms.length > 0 && managed.length === platforms.length,
    mixedModelReferences: managed.length > 0 && managed.length < platforms.length,
    openAIPassthrough: openAI && allTypes(['oauth', 'setup-token', 'apikey']),
    openAIOAuth: openAI && allTypes(['oauth', 'setup-token']),
    openAIOAuthOnly: openAI && allTypes(['oauth']),
    openAIAPIKey: openAI && allTypes(['apikey']),
    // Billing probes are supported across API-key platforms, including mixed selections.
    billingProbe: allTypes(['apikey']),
    // Selected accounts are a subset of this cross product; do not widen eligibility.
    headerOverride: platforms.length > 0 && types.length > 0 &&
      platforms.every(platform => types.every(type => isHeaderOverrideCapable(platform, type))),
    anthropicOAuth: platforms.length === 1 && platforms[0] === 'anthropic' &&
      allTypes(['oauth', 'setup-token'])
  }
}

export function bulkModelRestrictionDisabled(
  target: BulkTargetEligibility,
  passthrough: boolean | undefined
): boolean {
  return target.openAIPassthrough && passthrough === true
}
