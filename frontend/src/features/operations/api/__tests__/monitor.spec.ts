import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { getMatrix, getConfig, updateConfig, repeatedArrayParamsSerializer } from '../monitor'

afterEach(() => vi.restoreAllMocks())

describe('traffic monitor query serialization', () => {
  it('uses repeated keys without bracket suffixes for array filters', () => {
    const query = repeatedArrayParamsSerializer({
      range: '90m',
      platform: ['openai', 'grok'],
      group_id: [1, 2],
      model: undefined,
      group_by: 'platform_group_model',
    })

    expect(query).toBe('range=90m&platform=openai&platform=grok&group_id=1&group_id=2&group_by=platform_group_model')
    expect(query).not.toContain('%5B%5D')
  })

  it('sends the matrix grouping with the shared filters', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      data: { coverage: {}, group_by: 'platform_group', items: [] },
    })

    await getMatrix({ range: '24h', platforms: ['openai'], groupIds: [7], models: [] }, 'platform_group', true)

    expect(get).toHaveBeenCalledWith('/admin/channel-monitor-v2/matrix', expect.objectContaining({
      params: {
        range: '24h',
        platform: ['openai'],
        group_id: [7],
        model: undefined,
        group_by: 'platform_group',
      },
    }))
  })
})


describe('monitor configuration request lifetime', () => {
  it('passes cancellation into the same shared GET/PUT endpoints and preserves the server projection', async () => {
    const controller = new AbortController()
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: { enabled: false, platforms: [], thresholds: {} } })
    const config = await getConfig({ signal: controller.signal })
    expect(get).toHaveBeenCalledWith('/admin/channel-monitor-v2/config', { signal: controller.signal })
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: config })
    expect(await updateConfig(config, { signal: controller.signal })).toBe(config)
    expect(put).toHaveBeenCalledWith('/admin/channel-monitor-v2/config', config, { signal: controller.signal })
    controller.abort()
    expect(get.mock.calls[0][1]?.signal?.aborted).toBe(true)
    expect(put.mock.calls[0][2]?.signal?.aborted).toBe(true)
  })
})
