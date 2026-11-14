import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAvailableChannels, userChannelsAPI } from '../api/availableChannels'
import type { UserAvailableChannel } from '../api/types'
const get = vi.hoisted(() => vi.fn())
vi.mock('@/shared/http', () => ({ apiClient: { get } }))
beforeEach(() => get.mockReset())

describe('available channel HTTP contract', () => {
  it('uses the existing endpoint and preserves unknown, null and zero pricing fields', async () => {
    const data: UserAvailableChannel[] = [{ name: 'Channel', description: '', platforms: [{
      platform: 'unknown-provider', groups: [], supported_models: [{ name: 'model', platform: 'unknown-provider', pricing: {
        billing_mode: 'token', input_price: 0, output_price: null, cache_write_price: null,
        cache_write_1h_price: 0, cache_read_price: null, reasoning_effort_multipliers: { high: 0 },
        image_input_price: null, image_output_price: 0, per_request_price: null,
        intervals: [{ min_tokens: 0, max_tokens: null, tier_label: 'tier', input_price: null,
          output_price: 0, cache_write_price: null, cache_write_1h_price: 0, cache_read_price: null,
          input_multiplier: 0, output_multiplier: null, cache_write_multiplier: 2,
          cache_read_multiplier: 0, per_request_price: 0 }]
      } }]
    }] }]
    get.mockResolvedValue({ data })
    const controller = new AbortController()
    expect(await userChannelsAPI.getAvailable({ signal: controller.signal })).toBe(data)
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/channels/available', { signal: controller.signal })
    expect(getAvailableChannels).toBe(userChannelsAPI.getAvailable)
  })
  it('distinguishes an empty list from a primary API failure', async () => {
    get.mockResolvedValueOnce({ data: [] }).mockRejectedValueOnce(new Error('offline'))
    expect(await getAvailableChannels()).toEqual([])
    await expect(getAvailableChannels()).rejects.toThrow('offline')
  })
})
