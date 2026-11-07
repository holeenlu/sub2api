import { describe, expect, it, vi } from 'vitest'
import { getVersion, systemAPI } from '../api/version'
import { apiClient } from '@/shared/http'

vi.mock('@/shared/http', () => ({ apiClient: { get: vi.fn() } }))

describe('system version', () => {
  it('forwards cancellation through the public version-only surface', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { version: 'test-build' } })
    const controller = new AbortController()
    expect(await getVersion({ signal: controller.signal })).toEqual({ version: 'test-build' })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/system/version', { signal: controller.signal })
    expect(Object.keys(systemAPI)).toEqual(['getVersion'])
  })
})
