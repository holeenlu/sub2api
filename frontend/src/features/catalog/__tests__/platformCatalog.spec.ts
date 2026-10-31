import { afterEach, describe, expect, it } from 'vitest'
import { BUILTIN_PLATFORM_CATALOG, getPlatformSpec, platformDisplayName, resetPlatformCatalog, setPlatformCatalog, supportsPricingModelSync, supportsUpstreamModelSync } from '../model/platformCatalog'
afterEach(resetPlatformCatalog)
describe('platform directory facts', () => {
 it('preserves unknown display IDs without declaring capability support', () => {
  expect(getPlatformSpec('future-provider')).toBeUndefined()
  expect(platformDisplayName('future-provider')).toBe('future-provider')
  expect(supportsPricingModelSync('future-provider')).toBe(false)
  expect(supportsUpstreamModelSync('future-provider')).toBe(false)
 })
 it('requires an actual pricing catalog declaration for sync', () => {
  const provider = { id: 'future-provider', display_name: 'Future', gateway: 'openai' as const, cn_provider: false }
  setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, provider] })
  expect(supportsPricingModelSync(provider.id)).toBe(false)
  setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, { ...provider, litellm_provider: 'future' }] })
  expect(supportsPricingModelSync(provider.id)).toBe(true)
 })
})
