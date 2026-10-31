import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  getUserBalanceHistory,
  updateBalance,
  type AdminBalanceHistoryResponse,
  type AdminBalanceUpdateResult
} from '../userBalances'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

beforeEach(() => vi.resetAllMocks())

function history(): AdminBalanceHistoryResponse {
  return {
    items: [{
      id: 71, code: 'historical-code', type: 'admin_balance', value: -0.000001,
      status: 'used', used_by: null, used_at: null, created_at: '2026-10-25T01:00:00Z',
      group_id: null, validity_days: 0, notes: '', user: null, group: null
    }],
    total: 41, page: 3, page_size: 20, pages: 3, total_recharged: 0.123456789,
    has_more: false, total_exact: true
  }
}

describe('administrator balance commands', () => {
  it('preserves the default set operation, empty notes and id-only acknowledgement', async () => {
    const acknowledgement: AdminBalanceUpdateResult = { id: 17 }
    http.post.mockResolvedValue({ data: acknowledgement })
    expect(await updateBalance(17, 0.000000123)).toBe(acknowledgement)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/17/balance', {
      balance: 0.000000123, operation: 'set', notes: ''
    })
    expect(acknowledgement).not.toHaveProperty('balance')
  })

  it.each(['set', 'add', 'subtract'] as const)('preserves %s and full server data without rounding', async operation => {
    const data = { id: 17, balance: 1234.567891, email: 'historical@example.test' }
    http.post.mockResolvedValue({ data })
    expect(await updateBalance(17, 0.123456789, operation, '  audit note  ')).toBe(data)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/17/balance', {
      balance: 0.123456789, operation, notes: '  audit note  '
    })
  })

  it('does not replay a balance mutation after an unknown transport outcome', async () => {
    const error = new Error('connection closed after sending')
    http.post.mockRejectedValue(error)
    await expect(updateBalance(17, 5, 'subtract')).rejects.toBe(error)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.get).not.toHaveBeenCalled()
  })
})

describe('administrator balance history reads', () => {
  it('preserves the original default page and omitted filter', async () => {
    const data = history()
    http.get.mockResolvedValue({ data })
    expect(await getUserBalanceHistory(17)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/17/balance-history', {
      params: { page: 1, page_size: 20 }
    })
    expect(data.items[0]).toMatchObject({ used_by: null, used_at: null, group_id: null, user: null, group: null })
    expect(data.total_recharged).toBe(0.123456789)
  })

  it.each(['balance', 'affiliate_balance', 'admin_balance', 'concurrency', 'admin_concurrency', 'subscription'])('retains the %s history type and pagination', async type => {
    const data = history()
    http.get.mockResolvedValue({ data })
    expect(await getUserBalanceHistory(17, 3, 75, type)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/17/balance-history', {
      params: { page: 3, page_size: 75, type }
    })
  })

  it('continues omitting an empty type and passes the caller cancellation signal', async () => {
    const controller = new AbortController()
    http.get.mockResolvedValue({ data: history() })
    await getUserBalanceHistory(17, 2, 10, '', { signal: controller.signal })
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/17/balance-history', {
      params: { page: 2, page_size: 10 }, signal: controller.signal
    })
    controller.abort()
    expect(http.get.mock.calls[0][1].signal.aborted).toBe(true)
  })

  it('does not turn a denied or missing historical user into an empty ledger', async () => {
    const error = { response: { status: 404 }, message: 'User not found' }
    http.get.mockRejectedValue(error)
    await expect(getUserBalanceHistory(17)).rejects.toBe(error)
    expect(http.get).toHaveBeenCalledTimes(1)
  })
})
