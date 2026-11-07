import { describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { getPublicSettings } from '../api'
vi.mock('@/shared/http', () => ({ apiClient: { get: vi.fn() } }))
describe('site public settings loader', () => {
  it('uses the explicit anonymous transport policy on the same existing endpoint', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { site_name: 'sub2api' } })
    expect(await getPublicSettings()).toEqual({ site_name: 'sub2api' })
    expect(apiClient.get).toHaveBeenCalledWith('/settings/public', { sessionPolicy: 'anonymous' })
  })
  it('preserves caller cancellation without making the response identity-dependent', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: {} })
    const signal = new AbortController().signal
    await getPublicSettings({ signal })
    expect(apiClient.get).toHaveBeenCalledWith('/settings/public', { signal, sessionPolicy: 'anonymous' })
  })
})
