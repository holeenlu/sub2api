import { describe, expect, it } from 'vitest'
import { filterAvailableChannels } from '../model/availableChannels'
import type { UserAvailableChannel } from '../api/types'
const source: UserAvailableChannel[] = [{ name: 'Direct', description: 'Regional access', platforms: [
  { platform: 'openai', groups: [{ id: 1, name: 'Exclusive', platform: 'openai', subscription_type: 'standard', rate_multiplier: 0, is_exclusive: true, peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1 }], supported_models: [] },
  { platform: 'unknown', groups: [], supported_models: [{ name: 'Experimental', platform: 'unknown', pricing: null }] }
] }]
describe('available channel search presentation', () => {
  it.each(['', '  DIRECT ', 'regional'])('keeps complete channels matching metadata: %s', keyword => {
    expect(filterAvailableChannels(source, keyword)).toEqual(source)
  })
  it.each([['OpenAI', 0], ['exclusive', 0], ['experimental', 1], ['unknown', 1]] as const)('filters matching sections for %s without mutating the source', (keyword, index) => {
    const result = filterAvailableChannels(source, keyword)
    expect(result[0].platforms).toEqual([source[0].platforms[index]])
    expect(source[0].platforms).toHaveLength(2)
  })
  it('returns an empty search result without inventing a supported platform or model', () => {
    expect(filterAvailableChannels(source, 'missing')).toEqual([])
  })
})
