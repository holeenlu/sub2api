import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { paymentsServicesKey } from '../services'
import { createPaymentsServices } from './services'
import { usePaymentConfig } from '../queries/usePaymentConfig'
enableAutoUnmount(afterEach)
function createQuery() {
  let query!: ReturnType<typeof usePaymentConfig>
  mount(defineComponent({ setup() { query = usePaymentConfig(); return () => null } }), { global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices() } } })
  return query
}
import type { PaymentConfig } from '@/features/payments/api/types';

const getConfig = vi.hoisted(() => vi.fn())
vi.mock('@/features/payments/api/payment', () => ({ paymentAPI: { getConfig } }))

const config = { stripe_publishable_key: 'pk_test_example', payment_enabled: true } as PaymentConfig
function deferred() {
  let resolve!: (value: { data: PaymentConfig }) => void
  let reject!: (error: Error) => void
  const promise = new Promise<{ data: PaymentConfig }>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

beforeEach(() => {
  getConfig.mockReset()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

describe('payment configuration requests', () => {
  it('waits for the same response for concurrent initial callers', async () => {
    const pending = deferred()
    getConfig.mockReturnValue(pending.promise)
    const store = createQuery()
    const first = store.load()
    let finished = false
    const second = store.load().then(value => { finished = true; return value })
    await Promise.resolve()
    await Promise.resolve()
    expect(finished).toBe(false)
    expect(store.loading.value).toBe(true)
    expect(getConfig).toHaveBeenCalledTimes(1)
    pending.resolve({ data: config })
    expect(await Promise.all([first, second])).toEqual([config, config])
    expect(store.loading.value).toBe(false)
  })

  it('shares a forced refresh instead of returning stale cached configuration', async () => {
    getConfig.mockResolvedValueOnce({ data: config })
    const store = createQuery()
    await store.load()
    const pending = deferred()
    getConfig.mockReturnValueOnce(pending.promise)
    const first = store.load(true)
    const second = store.load(true)
    const updated = { ...config, stripe_publishable_key: 'pk_test_updated' }
    pending.resolve({ data: updated })
    expect(await Promise.all([first, second])).toEqual([updated, updated])
    expect(getConfig).toHaveBeenCalledTimes(2)
  })

  it('releases the shared request after failure so a later call can retry', async () => {
    const pending = deferred()
    getConfig.mockReturnValueOnce(pending.promise)
    const store = createQuery()
    const first = store.load()
    const second = store.load()
    pending.reject(new Error('offline'))
    expect(await Promise.all([first, second])).toEqual([null, null])
    expect(store.loading.value).toBe(false)
    expect(store.config.value).toBeNull()
    getConfig.mockResolvedValueOnce({ data: config })
    expect(await store.load()).toEqual(config)
    expect(getConfig).toHaveBeenCalledTimes(2)
  })

  it('returns cached configuration without another request', async () => {
    getConfig.mockResolvedValue({ data: config })
    const store = createQuery()
    await store.load()
    expect(await store.load()).toEqual(config)
    expect(getConfig).toHaveBeenCalledTimes(1)
  })
})
