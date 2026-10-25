import { beforeEach, describe, expect, it, vi } from 'vitest'

const harness = vi.hoisted(() => ({
  calls: [] as string[],
  app: { use: vi.fn(), mount: vi.fn() },
  pinia: { name: 'pinia' },
  router: { isReady: vi.fn() },
  i18n: { name: 'i18n' },
  initI18n: vi.fn(),
  store: { siteName: 'Configured Site', initFromInjectedConfig: vi.fn() },
}))

vi.mock('vue', () => ({ createApp: () => {
  harness.calls.push('create')
  return harness.app
} }))
vi.mock('pinia', () => ({ createPinia: () => harness.pinia }))
vi.mock('../App.vue', () => ({ default: { name: 'ApplicationRoot' } }))
vi.mock('@/router', () => ({ default: harness.router }))
vi.mock('@/i18n', () => ({ default: harness.i18n, initI18n: harness.initI18n }))
vi.mock('@/stores/app', () => ({ useAppStore: () => harness.store }))
vi.mock('@/utils/device', () => ({ isIOSDevice: () => false }))

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
  })
})
