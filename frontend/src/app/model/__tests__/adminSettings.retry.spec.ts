import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia, disposePinia } from 'pinia'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { useAuthStore } from '@/features/identity'

const mocks = vi.hoisted(() => ({ getSettings: vi.fn(), getConfig: vi.fn() }))
vi.mock('@/features/system/api', () => ({ settingsAPI: { getSettings: mocks.getSettings } }))
vi.mock('@/features/payments', () => ({ adminPaymentAPI: { getConfig: mocks.getConfig } }))
let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().token = 'test-admin-token'
  useAuthStore().user = { id: 1, role: "super_admin" } as any
  mocks.getSettings.mockResolvedValue({ ops_monitoring_enabled: true, custom_menu_items: [{ id: 'custom', title: 'Custom' }] })
  mocks.getConfig.mockResolvedValue({ data: { enabled: true } })
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { disposePinia(pinia); vi.restoreAllMocks(); localStorage.clear() })

describe('admin settings fetch retry', () => {
  it.each(['settings', 'payment'])('retries a failed initial %s request without forcing a refresh', async (source) => {
    const request = source === 'settings' ? mocks.getSettings : mocks.getConfig
    request.mockRejectedValueOnce(new Error('Temporarily unavailable'))
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.loaded).toBe(false)
    expect(store.loading).toBe(false)
    await store.fetch()
    expect(mocks.getSettings).toHaveBeenCalledTimes(2)
    expect(mocks.getConfig).toHaveBeenCalledTimes(2)
    expect(store.loaded).toBe(true)
    expect(store.paymentEnabled).toBe(true)
    expect(store.customMenuItems).toEqual([{ id: 'custom', title: 'Custom' }])
  })

  it('keeps cached values visible when the initial request fails', async () => {
    localStorage.setItem('ops_monitoring_enabled_cached', 'false')
    localStorage.setItem('payment_enabled_cached', 'true')
    mocks.getSettings.mockRejectedValueOnce(new Error('Offline'))
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.opsMonitoringEnabled).toBe(false)
    expect(store.paymentEnabled).toBe(true)
    expect(localStorage.getItem('payment_enabled_cached')).toBe('true')
  })

  it('still reuses successfully loaded settings', async () => {
    const store = useAdminSettingsStore()
    await store.fetch()
    await store.fetch()
    expect(mocks.getSettings).toHaveBeenCalledTimes(1)
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
  })

  it.each(['settings', 'payment'])('reports a forced %s projection failure without replacing the last confirmed values', async source => {
    const store = useAdminSettingsStore()
    await store.fetch()
    const error = new Error('Projection unavailable')
    const request = source === 'settings' ? mocks.getSettings : mocks.getConfig
    request.mockRejectedValueOnce(error)
    await expect(store.fetch(true, { throwOnError: true })).rejects.toBe(error)
    expect(store.loading).toBe(false)
    expect(store.paymentEnabled).toBe(true)
    expect(store.customMenuItems).toEqual([{ id: 'custom', title: 'Custom' }])
    await expect(store.fetch(true, { throwOnError: true })).resolves.toBeUndefined()
    expect(request).toHaveBeenCalledTimes(3)
  })

  it('lets each caller choose error reporting when a background load is already pending', async () => {
    let reject = (_error: Error): void => { throw new Error('Deferred request is not initialized') }
    mocks.getSettings.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    const store = useAdminSettingsStore()
    const background = store.fetch()
    const explicit = store.fetch(false, { throwOnError: true })
    const error = new Error('Shared read failed')
    const assertion = expect(explicit).rejects.toBe(error)
    reject(error)
    await Promise.all([expect(background).resolves.toBeUndefined(), assertion])
    expect(mocks.getSettings).toHaveBeenCalledTimes(1)
    expect(store.loading).toBe(false)
    expect(store.loaded).toBe(false)
  })

  it('does not report a disposed owner failure even when explicitly requested', async () => {
    let reject = (_error: Error): void => { throw new Error('Deferred request is not initialized') }
    mocks.getSettings.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    const store = useAdminSettingsStore()
    const read = store.fetch(true, { throwOnError: true })
    store.setScope(null)
    reject(new Error('Late failure'))
    await expect(read).resolves.toBeUndefined()
    expect(console.error).not.toHaveBeenCalled()
  })
})


describe('administrator settings request ownership', () => {
  it('ignores a previous administrator response after a role and policy change', async () => {
    let resolve = (_settings: unknown): void => { throw new Error('Deferred request is not initialized') }
    mocks.getSettings.mockReturnValueOnce(new Promise(finish => { resolve = finish }))
    const store = useAdminSettingsStore()
    const stale = store.fetch()
    const signal = mocks.getSettings.mock.calls[0][0].signal as AbortSignal
    expect(mocks.getConfig.mock.calls[0][0].signal).toBe(signal)
    const auth = useAuthStore()
    const prior = auth.user
    if (!prior) throw new Error('Administrator fixture is missing')
    auth.user = { ...prior, role: 'admin', policy_version: 2,
      admin_features: { ops_monitoring_enabled: false, ops_realtime_monitoring_enabled: false } }
    await store.fetch()
    expect(signal.aborted).toBe(true)
    resolve({ ops_monitoring_enabled: true, custom_menu_items: [{ id: 'private', title: 'Prior admin' }] })
    await stale
    expect(store.opsMonitoringEnabled).toBe(false)
    expect(store.customMenuItems).toEqual([])
    expect(store.loading).toBe(false)
    expect(localStorage.getItem('ops_monitoring_enabled_cached')).toBeNull()
  })

  it('cancels a pending request on sign-out even when its transport resolves later', async () => {
    let resolve = (_settings: unknown): void => { throw new Error('Deferred request is not initialized') }
    mocks.getSettings.mockReturnValueOnce(new Promise(finish => { resolve = finish }))
    const store = useAdminSettingsStore()
    const stale = store.fetch()
    useAuthStore().token = null
    useAuthStore().user = null
    store.setScope(null)
    resolve({ ops_monitoring_enabled: true, custom_menu_items: [{ id: 'secret' }] })
    await stale
    expect(store.loaded).toBe(false)
    expect(store.loading).toBe(false)
    expect(store.customMenuItems).toEqual([])
  })

  it('shares a same-scope load and does not commit an explicitly cancelled result', async () => {
    let resolve = (_settings: unknown): void => { throw new Error('Deferred request is not initialized') }
    mocks.getSettings.mockReturnValueOnce(new Promise(finish => { resolve = finish }))
    const controller = new AbortController()
    const store = useAdminSettingsStore()
    const first = store.fetch(false, { signal: controller.signal })
    const second = store.fetch()
    expect(mocks.getSettings).toHaveBeenCalledTimes(1)
    controller.abort()
    resolve({ ops_monitoring_enabled: true })
    await Promise.all([first, second])
    expect(store.loaded).toBe(false)
    expect(store.loading).toBe(false)
    expect(localStorage.getItem('ops_monitoring_enabled_cached')).toBeNull()
  })
})
