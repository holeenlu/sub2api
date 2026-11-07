import { beforeEach, describe, expect, it, vi } from 'vitest'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))
import { adminPaymentAPI } from '../api/adminPayment'
describe('provider API request ownership', () => {
  beforeEach(() => { Object.values(http).forEach(method => method.mockReset().mockResolvedValue({ data: {} })) })
  it('preserves provider endpoints and explicit payload values while forwarding cancellation', async () => {
    const options = { signal: new AbortController().signal }
    const payload = { enabled: false, supported_types: [], sort_order: 0, config: { privateKey: '' } }
    await adminPaymentAPI.getProviders(options)
    await adminPaymentAPI.createProvider(payload, options)
    await adminPaymentAPI.updateProvider(7, payload, options)
    await adminPaymentAPI.deleteProvider(7, options)
    expect(http.get).toHaveBeenCalledWith('/admin/payment/providers', options)
    expect(http.post).toHaveBeenCalledWith('/admin/payment/providers', payload, options)
    expect(http.put).toHaveBeenCalledWith('/admin/payment/providers/7', payload, options)
    expect(http.delete).toHaveBeenCalledWith('/admin/payment/providers/7', options)
  })
  it.each(['aborted', 'stale'] as const)('does not send providers requests from a %s scope', state => {
    const controller = new AbortController(); if (state === 'aborted') controller.abort()
    const options = { signal: controller.signal, isCurrent: () => state !== 'stale' }
    expect(() => adminPaymentAPI.getProviders(options)).toThrow()
    expect(() => adminPaymentAPI.createProvider({}, options)).toThrow()
    expect(() => adminPaymentAPI.updateProvider(7, {}, options)).toThrow()
    expect(() => adminPaymentAPI.deleteProvider(7, options)).toThrow()
    Object.values(http).forEach(method => expect(method).not.toHaveBeenCalled())
  })
})
