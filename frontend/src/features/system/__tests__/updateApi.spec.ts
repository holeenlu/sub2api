import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { checkOnlineUpdate, getOnlineUpdate, startOnlineUpdate } from '../api/update'

vi.mock('@/shared/http', () => ({ apiClient: { get: vi.fn(), post: vi.fn() } }))

beforeEach(() => { vi.clearAllMocks() })
describe('complete release online update API', () => {
  it('forwards cancellation to status and release checks', async () => {
    const status = { enabled: false }
    vi.mocked(apiClient.get).mockResolvedValue({ data: status })
    vi.mocked(apiClient.post).mockResolvedValue({ data: status })
    const options = { signal: new AbortController().signal }
    expect(await getOnlineUpdate(options)).toEqual(status)
    expect(await checkOnlineUpdate(options)).toEqual(status)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/system/update', options)
    expect(apiClient.post).toHaveBeenCalledWith('/admin/system/update/check', {}, { ...options, timeout: 130000 })
  })

  it('submits only the reviewed commit and stable idempotency key', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ data: { enabled: true } })
    const signal = new AbortController().signal
    await startOnlineUpdate('a'.repeat(40), 'operation-1', { signal })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/system/update', { target_commit: 'a'.repeat(40) }, {
      signal, timeout: 130000, headers: { 'Idempotency-Key': 'operation-1' }
    })
  })
})
