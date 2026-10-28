import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getUserApiKeys } from '../userKeys'

const http = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

beforeEach(() => vi.resetAllMocks())

describe('administrator user Key query', () => {
  it('preserves endpoint, pagination metadata and nullable Key fields without invented params', async () => {
    const data = {
      items: [{ id: 19, user_id: 7, name: 'existing', key: 'fixture-only', group_id: null, expires_at: null, current_concurrency: null }],
      total: 61, page: 1, page_size: 20, pages: 4,
      next_cursor: 'opaque-cursor', has_more: true, total_exact: false
    }
    http.get.mockResolvedValue({ data })
    expect(await getUserApiKeys(7)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/api-keys')
    expect(data.items[0].group_id).toBeNull()
    expect(data.items[0].current_concurrency).toBeNull()
  })

  it('passes an explicit query signal through the same authenticated client', async () => {
    const controller = new AbortController()
    http.get.mockResolvedValue({ data: { items: [] } })
    await getUserApiKeys(7, { signal: controller.signal })
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/api-keys', { signal: controller.signal })
    controller.abort()
    expect(http.get.mock.calls[0][1].signal.aborted).toBe(true)
  })

  it('keeps denied access as an error instead of returning an empty list', async () => {
    const error = { response: { status: 403 } }
    http.get.mockRejectedValue(error)
    await expect(getUserApiKeys(7)).rejects.toBe(error)
    expect(http.get).toHaveBeenCalledTimes(1)
  })
})
