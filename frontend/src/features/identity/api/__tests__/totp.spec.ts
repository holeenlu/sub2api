import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { get } }))

import { totpAPI } from '../totp'
import type { TotpStatus } from '../securityTypes'

describe('TOTP status request cancellation', () => {
  beforeEach(() => get.mockReset())

  it('preserves the no-argument status query and server response', async () => {
    const status = { enabled: false, enabled_at: null, feature_enabled: true } satisfies TotpStatus
    get.mockResolvedValueOnce({ data: status })

    await expect(totpAPI.getStatus()).resolves.toEqual(status)
    expect(get).toHaveBeenCalledWith('/user/totp/status')
  })

  it('forwards the caller signal and preserves request cancellation', async () => {
    const controller = new AbortController()
    const canceled = new Error('Request canceled')
    get.mockImplementationOnce((_url, options) => new Promise((_resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(canceled), { once: true })
    }))

    const request = totpAPI.getStatus({ signal: controller.signal })
    expect(get).toHaveBeenCalledWith('/user/totp/status', { signal: controller.signal })
    controller.abort()

    await expect(request).rejects.toBe(canceled)
    expect(get).toHaveBeenCalledTimes(1)
  })
})
