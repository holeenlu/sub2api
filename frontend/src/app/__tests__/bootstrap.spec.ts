import { beforeEach, describe, expect, it, vi } from 'vitest'

const harness = vi.hoisted(() => ({
  calls: [] as string[],
  app: { use: vi.fn(), mount: vi.fn(), onUnmount: vi.fn(), unmount: vi.fn() },
  disposeFeatures: vi.fn(),
  disposePrefetch: vi.fn(),
  disposeHttp: vi.fn(),
  disposeState: vi.fn(),
  requireCompliance: vi.fn(),
  httpEffects: null as { requireAdminCompliance(status: unknown): void } | null,
  pinia: { name: 'pinia' },
  router: { isReady: vi.fn() },
  i18n: { name: 'i18n' },
  initI18n: vi.fn(),
  configureLocaleMessageLoader: vi.fn(),
  store: { siteName: 'Configured Site', initFromInjectedConfig: vi.fn() },
}))

vi.mock('vue', () => ({ createApp: () => {
  harness.calls.push('create')
  return harness.app
} }))
vi.mock('pinia', () => ({ createPinia: () => harness.pinia, disposePinia: harness.disposeState }))
vi.mock('../App.vue', () => ({ default: { name: 'ApplicationRoot' } }))
vi.mock('../compositions/featureServices', () => ({ installFeatureServices: () => harness.disposeFeatures }))
vi.mock('../http/install', () => ({ installApplicationHttp: (effects: NonNullable<typeof harness.httpEffects>) => { harness.httpEffects = effects; return harness.disposeHttp } }))
vi.mock('@/features/identity', () => ({ useAdminComplianceStore: () => ({ requireAcknowledgement: harness.requireCompliance }) }))
vi.mock('../router/useRoutePrefetch', () => ({ installRoutePrefetch: () => harness.disposePrefetch }))
vi.mock('@/app/router', () => ({ default: harness.router }))
vi.mock('@/i18n', () => ({ default: harness.i18n, initI18n: harness.initI18n, configureLocaleMessageLoader: harness.configureLocaleMessageLoader }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => harness.store }))
vi.mock('@/shared/lib/device', () => ({ isIOSDevice: () => false }))

import { bootstrap } from '../bootstrap'

describe('application bootstrap', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    harness.calls.length = 0
    harness.store.siteName = 'Configured Site'
    harness.app.use.mockImplementation((plugin: unknown) => {
      harness.calls.push(plugin === harness.router ? 'router' : plugin === harness.pinia ? 'pinia' : 'i18n')
      return harness.app
    })
    harness.store.initFromInjectedConfig.mockImplementation(() => {
      harness.calls.push('settings')
      return true
    })
    harness.initI18n.mockImplementation(async () => { harness.calls.push('locale') })
    harness.router.isReady.mockImplementation(async () => { harness.calls.push('ready') })
    harness.app.mount.mockImplementation(() => { harness.calls.push('mount') })
    document.title = 'Initial document'
  })

  it('applies injected branding before locale and router initialization', async () => {
    harness.initI18n.mockImplementation(async () => {
      expect(document.title).toBe('Configured Site - AI API Gateway')
      harness.calls.push('locale')
    })
    await bootstrap()
    expect(harness.calls).toEqual(['create', 'pinia', 'settings', 'locale', 'router', 'i18n', 'ready', 'mount'])
    expect(harness.app.mount).toHaveBeenCalledWith('#app')
  })

  it('uses the neutral project title when site settings are absent', async () => {
    harness.store.siteName = ''
    await bootstrap()
    expect(document.title).toBe('sub2api - AI API Gateway')
  })

  it('waits for initial navigation before mounting the application', async () => {
    let finishNavigation: () => void = () => undefined
    harness.router.isReady.mockImplementation(() => new Promise<void>((resolve) => { finishNavigation = resolve }))
    const pending = bootstrap()
    await vi.waitFor(() => expect(harness.router.isReady).toHaveBeenCalledOnce())
    expect(harness.app.mount).not.toHaveBeenCalled()
    finishNavigation()
    await pending
    expect(harness.app.mount).toHaveBeenCalledOnce()
  })

  it('leaves failed initialization unmounted and reports it to the entrypoint', async () => {
    harness.initI18n.mockRejectedValue(new Error('locale unavailable'))
    await expect(bootstrap()).rejects.toThrow('locale unavailable')
    expect(harness.app.mount).not.toHaveBeenCalled()
    expect(harness.router.isReady).not.toHaveBeenCalled()
    expect(harness.disposeFeatures).toHaveBeenCalledOnce()
    expect(harness.disposeHttp).toHaveBeenCalledOnce()
    expect(harness.disposeState).toHaveBeenCalledWith(harness.pinia)
  })

  it('disposes installed warming and services when initial navigation fails', async () => {
    harness.router.isReady.mockRejectedValue(new Error('navigation failed'))
    await expect(bootstrap()).rejects.toThrow('navigation failed')
    expect(harness.disposePrefetch).toHaveBeenCalledOnce()
    expect(harness.disposeFeatures).toHaveBeenCalledOnce()
    expect(harness.app.unmount).toHaveBeenCalledOnce()
    expect(harness.app.mount).not.toHaveBeenCalled()
  })

  it('registers warming cleanup with the application host', async () => {
    await bootstrap()
    expect(harness.app.onUnmount).toHaveBeenCalledWith(harness.disposePrefetch)
  })

  it('connects compliance directly to the application identity owner', async () => {
    await bootstrap()
    const status = { version: 'fixture-v2' }
    harness.httpEffects!.requireAdminCompliance(status)
    expect(harness.requireCompliance).toHaveBeenCalledWith(status)
    expect(harness.app.onUnmount).toHaveBeenCalledWith(harness.disposeHttp)
  })

})
