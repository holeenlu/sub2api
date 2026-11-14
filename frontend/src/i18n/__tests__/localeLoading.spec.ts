import { beforeEach, describe, expect, it, vi } from 'vitest'

const { loaders } = vi.hoisted(() => ({
  loaders: { en: vi.fn(), zh: vi.fn(), ja: vi.fn(), 'zh-TW': vi.fn() },
}))

function deferred() {
  let resolve!: (value: { default: Record<string, unknown> }) => void
  let reject!: (error: Error) => void
  const promise = new Promise<{ default: Record<string, unknown> }>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('language loading and selection', () => {
  beforeEach(async () => {
    vi.resetModules()
    vi.clearAllMocks()
    localStorage.clear()
    localStorage.setItem('sub2api_locale', 'en')
    for (const loader of Object.values(loaders)) loader.mockResolvedValue({ default: { common: { fallback: 'English' } } })
    const { configureLocaleMessageLoader } = await import('..')
    configureLocaleMessageLoader(async locale => (await loaders[locale]()).default)
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
  })

  it('does not persist a failed selection and retries the failed load', async () => {
    loaders.ja.mockRejectedValueOnce(new Error('offline'))
    const { i18n, setLocale } = await import('..')
    await expect(setLocale('ja')).rejects.toThrow()
    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('sub2api_locale')).toBe('en')
    expect(i18n.global.availableLocales).not.toContain('ja')
    // A rejected injected chunk loader is retried without replacing the engine.
    expect(await setLocale('ja')).toBe(true)
    expect(loaders.ja).toHaveBeenCalledTimes(2)
  })

  it('ignores an older failed request after a newer language is committed', async () => {
    const pending = deferred()
    loaders.ja.mockReturnValue(pending.promise)
    const { i18n, setLocale } = await import('..')
    const older = setLocale('ja')
    expect(await setLocale('zh')).toBe(true)
    pending.reject(new Error('old chunk failed'))
    await expect(older).resolves.toBe(false)
    expect(i18n.global.locale.value).toBe('zh')
    expect(localStorage.getItem('sub2api_locale')).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('requires an explicit loader and can recover after a missing installation', async () => {
    vi.resetModules()
    const { configureLocaleMessageLoader, loadLocaleMessages, i18n } = await import('..')
    await expect(loadLocaleMessages('en')).rejects.toThrow('Locale message loader is not configured')
    const load = vi.fn().mockResolvedValue({ common: { ready: 'Ready' } })
    configureLocaleMessageLoader(load)
    await loadLocaleMessages('en')
    expect(i18n.global.t('common.ready')).toBe('Ready')
    expect(() => configureLocaleMessageLoader(load)).not.toThrow()
    expect(() => configureLocaleMessageLoader(async () => ({}))).toThrow('Cannot replace the locale loader')
    expect(load).toHaveBeenCalledOnce()
  })
})
