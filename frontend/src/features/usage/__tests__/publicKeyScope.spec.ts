import { defineComponent, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { publicKeyUsageServicesKey } from '../publicKeyServices'
import { usePublicKeyUsage } from '../queries/usePublicKeyUsage'
import type { PublicKeyUsageResponse } from '../api/publicKeyUsage'
const getPublicKeyUsage = vi.hoisted(() => vi.fn())
vi.mock('../api/publicKeyUsage', () => ({ getPublicKeyUsage }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => getPublicKeyUsage.mockReset())
function pending() { let resolve!: (value: PublicKeyUsageResponse) => void; const promise = new Promise<PublicKeyUsageResponse>(done => { resolve = done }); return { promise, resolve } }
function open() {
  let query!: ReturnType<typeof usePublicKeyUsage>
  const notifySuccess = vi.fn(), notifyError = vi.fn()
  const wrapper = mount(defineComponent({ setup() { query = usePublicKeyUsage(); return () => null } }), { global: { provide: { [publicKeyUsageServicesKey as symbol]: {
    appearance: ref({ siteName: '', siteLogo: '', docUrl: '', subscriptionEnabled: true }), ensureSettings: vi.fn(), notifyInfo: vi.fn(), notifySuccess, notifyError,
  } } } })
  return { query, wrapper, notifySuccess, notifyError }
}
describe('public key query ownership', () => {
  it('aborts the previous key and never exposes its late response or notice', async () => {
    const old = pending(); getPublicKeyUsage.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ mode: 'unrestricted', balance: 2 })
    const { query, notifySuccess, notifyError } = open()
    query.apiKey.value = 'key-a'; const first = query.queryKey(), signal = getPublicKeyUsage.mock.calls[0][2].signal
    query.apiKey.value = 'key-b'; await query.queryKey()
    old.resolve({ mode: 'unrestricted', balance: 100 }); await first
    expect(signal.aborted).toBe(true); expect(query.resultData.value?.balance).toBe(2)
    expect(notifySuccess).toHaveBeenCalledOnce(); expect(notifyError).not.toHaveBeenCalled()
  })
  it('keeps the newest daily range when users click again during a pending refresh', async () => {
    const old = pending(); getPublicKeyUsage.mockResolvedValueOnce({ mode: 'unrestricted', balance: 30 }).mockReturnValueOnce(old.promise).mockResolvedValueOnce({ mode: 'unrestricted', balance: 90 })
    const { query, wrapper } = open(); query.apiKey.value = 'key-a'; await query.queryKey()
    query.setDailyUsageDays(7); const signal = getPublicKeyUsage.mock.calls[1][2].signal
    query.setDailyUsageDays(90); await flushPromises(); old.resolve({ mode: 'unrestricted', balance: 7 }); await flushPromises()
    expect(getPublicKeyUsage).toHaveBeenCalledTimes(3); expect(signal.aborted).toBe(true)
    expect(getPublicKeyUsage.mock.calls[2][1].days).toBe(90); expect(query.resultData.value?.balance).toBe(90)
    wrapper.unmount(); expect(getPublicKeyUsage.mock.calls[2][2].signal.aborted).toBe(true)
  })
})
