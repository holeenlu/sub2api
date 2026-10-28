import { beforeEach, describe, expect, it, vi } from 'vitest'

const { loaders, title } = vi.hoisted(() => ({
  loaders: { en: vi.fn(), zh: vi.fn(), ja: vi.fn(), 'zh-TW': vi.fn() },
  title: vi.fn(() => 'Localized title'),
}))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: title, applyRouteMetaDescription: vi.fn() }))
vi.mock('@/router', () => ({ default: { currentRoute: { value: {} } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ siteName: 'Test', cachedPublicSettings: null }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))

function deferred() {
  let resolve!: (value: { default: Record<string, unknown> }) => void
  let reject!: (error: Error) => void
  const promise = new Promise<{ default: Record<string, unknown> }>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('language loading and selection', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    localStorage.clear()
    localStorage.setItem('sub2api_locale', 'en')
    for (const loader of Object.values(loaders)) loader.mockResolvedValue({ default: { common: { fallback: 'English' } } })
    vi.doMock('../locales/en', () => loaders.en())
    vi.doMock('../locales/zh', () => loaders.zh())
    vi.doMock('../locales/ja', () => loaders.ja())
    vi.doMock('../locales/zh-TW', () => loaders['zh-TW']())
  })

  it('shares in-flight fallback loads and preserves the latest site name', async () => {
    const pending = deferred()
    loaders.en.mockReturnValue(pending.promise)
    const { i18n, loadLocaleMessages, setSiteName } = await import('..')
    const first = loadLocaleMessages('zh-TW')
    const second = loadLocaleMessages('en')
    setSiteName('Changed @ Site')
    pending.resolve({ default: { common: { fallback: 'English' } } })
    await Promise.all([first, second])
    expect(loaders.en).toHaveBeenCalledTimes(1)
    expect(loaders.zh).toHaveBeenCalledTimes(1)
    i18n.global.locale.value = 'zh-TW'
    expect(i18n.global.t('common.siteName')).toBe('Changed @ Site')
    expect(i18n.global.t('common.fallback')).toBe('English')
  })

  it('keeps the latest choice when an older load completes afterward', async () => {
    const pending = deferred()
    loaders.ja.mockReturnValue(pending.promise)
    const { i18n, setLocale } = await import('..')
    const older = setLocale('ja')
    expect(await setLocale('zh')).toBe(true)
    pending.resolve({ default: {} })
    expect(await older).toBe(false)
    expect(i18n.global.locale.value).toBe('zh')
    expect(localStorage.getItem('sub2api_locale')).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
    expect(title).toHaveBeenCalledTimes(1)
  })

  it('does not persist a failed selection and retries the failed load', async () => {
    loaders.ja.mockRejectedValueOnce(new Error('offline'))
    const { i18n, setLocale } = await import('..')
    await expect(setLocale('ja')).rejects.toThrow()
    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('sub2api_locale')).toBe('en')
    // Vitest caches rejected module factories; model a retryable chunk fetch.
    vi.doMock('../locales/ja', () => loaders.ja())
    expect(await setLocale('ja')).toBe(true)
    expect(loaders.ja).toHaveBeenCalledTimes(2)
  })
})
