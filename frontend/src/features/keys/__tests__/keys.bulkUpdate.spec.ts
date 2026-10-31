import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { bulkUpdate } from '../api/keys'

const { put } = vi.hoisted(() => ({ put: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { put } }))

describe('API key bulk updates', () => {
  beforeEach(() => {
    put.mockReset()
  })

  it('updates each selected key once and preserves partial failures', async () => {
    const failure = { response: { status: 403, data: { detail: 'Forbidden' } } }
    put.mockImplementation((url: string) => url === '/keys/2'
      ? Promise.reject(failure)
      : Promise.resolve({ data: { id: Number(url.split('/').pop()) } }))

    const updates = { group_id: 12, quota: 0, expires_at: '', ip_whitelist: [] }
    const result = await bulkUpdate([1, 2, 1, 3], updates)

    expect(put.mock.calls).toEqual([
      ['/keys/1', updates, { signal: undefined }], ['/keys/2', updates, { signal: undefined }], ['/keys/3', updates, { signal: undefined }]
    ])
    expect(result).toEqual({ succeededIds: [1, 3], failures: [{ id: 2, error: failure }] })
  })

  it('limits in-flight requests and continues after a failed batch member', async () => {
    const settle: Array<() => void> = []
    let inFlight = 0
    let peak = 0
    put.mockImplementation(() => new Promise((resolve, reject) => {
      inFlight++
      peak = Math.max(peak, inFlight)
      const index = settle.length
      settle.push(() => {
        inFlight--
        if (index === 0) reject(new Error('network error'))
        else resolve({ data: {} })
      })
    }))

    const pending = bulkUpdate([1, 2, 3, 4, 5, 6, 7], { status: 'inactive' })
    expect(put).toHaveBeenCalledTimes(5)
    settle.slice(0, 4).forEach((finish) => finish())
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(5)
    settle[4]()
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(7)
    settle.slice(5).forEach((finish) => finish())
    const result = await pending

    expect(peak).toBe(5)
    expect(result.succeededIds).toEqual([2, 3, 4, 5, 6, 7])
    expect(result.failures.map(({ id }) => id)).toEqual([1])
  })

  it('does not send requests when no keys are selected', async () => {
    expect(await bulkUpdate([], { quota: 0 })).toEqual({ succeededIds: [], failures: [] })
    expect(put).not.toHaveBeenCalled()
  })
  it('stops before sending another batch after session invalidation', async () => {
    let current = true
    const settle: Array<() => void> = []
    put.mockImplementation(() => new Promise(resolve => settle.push(() => resolve({ data: {} }))))
    const pending = bulkUpdate([1, 2, 3, 4, 5, 6], { status: 'inactive' }, { isCurrent: () => current })
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    expect(put).toHaveBeenCalledTimes(5)
    current = false; settle.forEach(resolve => resolve())
    await rejected
    expect(put).toHaveBeenCalledTimes(5)
  })

  it('does not accept final-batch results after the captured scope is revoked', async () => {
    let current = true
    let finish: () => void = () => undefined
    put.mockImplementation(() => new Promise(resolve => { finish = () => resolve({ data: {} }) }))
    const pending = bulkUpdate([1], { quota: 0 }, { isCurrent: () => current })
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    current = false; finish(); await rejected
    expect(put).toHaveBeenCalledTimes(1)
  })
  it('keeps the original field values and IP arrays for every batch', async () => {
    const settle: Array<() => void> = []
    put.mockImplementation(() => new Promise(resolve => settle.push(() => resolve({ data: {} }))))
    const updates = { quota: 0, ip_whitelist: ['10.0.0.1'], ip_blacklist: [] as string[] }
    const pending = bulkUpdate([1, 2, 3, 4, 5, 6], updates)
    updates.quota = 100; updates.ip_whitelist.push('10.0.0.2'); updates.ip_blacklist.push('10.0.0.3')
    settle.slice(0, 5).forEach(resolve => resolve()); await flushPromises()
    expect(put.mock.calls[5][1]).toEqual({ quota: 0, ip_whitelist: ['10.0.0.1'], ip_blacklist: [] })
    settle[5](); await pending
  })

})
