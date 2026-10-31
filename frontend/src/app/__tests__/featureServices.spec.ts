import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, inject } from 'vue'
import { createPinia, disposePinia } from 'pinia'
import { identitySettingsKey, identityClipboardKey } from '@/features/identity'
import { installFeatureServices } from '../compositions/featureServices'

const harness = vi.hoisted(() => ({
  current: { tencent_captcha_enabled: false, login_agreement_revision: 'v1' },
  fetch: vi.fn(),
  notify: vi.fn()
}))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({
  get cachedPublicSettings() { return harness.current },
  publicSettingsLoaded: true,
  fetchPublicSettings: harness.fetch,
  showSuccess: harness.notify, showError: harness.notify,
  showWarning: harness.notify, showInfo: harness.notify
}) }))

afterEach(() => vi.clearAllMocks())

describe('identity application services', () => {
  it('refreshes security settings when an identity page re-enters an already loaded app', async () => {
    const pinia = createPinia()
    const app = createApp({ render: () => null })
    app.use(pinia)
    harness.current = { tencent_captcha_enabled: false, login_agreement_revision: 'v1' }
    harness.fetch.mockImplementation(async (force: boolean) => {
      if (force) harness.current = { tencent_captcha_enabled: true, login_agreement_revision: 'v2' }
      return harness.current
    })
    let disposeServices: (() => void) | undefined
    try {
      disposeServices = installFeatureServices(app)
      const settings = app.runWithContext(() => inject(identitySettingsKey))
      expect(settings?.current?.login_agreement_revision).toBe('v1')
      const loaded = await settings?.load()
      expect(loaded?.login_agreement_revision).toBe('v2')
      expect(loaded?.tencent_captcha_enabled).toBe(true)
      expect(settings?.current).toBe(loaded)
      expect(app.runWithContext(() => inject(identityClipboardKey))).toBeTypeOf('function')
    } finally {
      disposeServices?.()
      disposePinia(pinia)
    }
  })
})
