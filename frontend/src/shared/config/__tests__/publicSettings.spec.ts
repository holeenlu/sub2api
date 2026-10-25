import { describe, expect, it, vi } from 'vitest'
import { createPublicSettings } from '../publicSettings'
import type { PublicSettingsFields } from '../publicSettings'

interface TestSettings extends PublicSettingsFields {
  feature_enabled: boolean
  page_size: number
  endpoints: Array<{ name: string; url: string }>
}

function settings(overrides: Partial<TestSettings> = {}): TestSettings {
  return {
    site_name: 'Saved site',
    site_logo: '/saved-logo.svg',
    version: '1.0.0',
    contact_info: 'help@example.test',
    api_base_url: 'https://api.example.test',
    doc_url: 'https://docs.example.test',
    backend_mode_enabled: false,
    feature_enabled: false,
    page_size: 0,
    endpoints: [{ name: 'Gateway', url: 'https://api.example.test/v1' }],
    ...overrides
  }
}

function deferred<T>(): {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (error: unknown) => void
} {
  let resolve: ((value: T) => void) | undefined
  let reject: ((error: unknown) => void) | undefined
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return {
    promise,
    resolve: (value) => {
      if (!resolve) throw new Error('Deferred resolver was not initialized')
      resolve(value)
    },
    reject: (error) => {
      if (!reject) throw new Error('Deferred rejecter was not initialized')
      reject(error)
    }
  }
}

function owner(initial?: TestSettings) {
  let injected = initial
  const load = vi.fn<() => Promise<TestSettings>>()
  const readInjected = vi.fn(() => injected)
  const onApplied = vi.fn()
  const onError = vi.fn()
  const onCleared = vi.fn(() => { injected = undefined })
  const state = createPublicSettings<TestSettings>({
    defaultSiteName: 'sub2api',
    load,
    readInjected,
    onApplied,
    onCleared,
    onError
  })
  return { state, load, readInjected, onApplied, onCleared, onError }
}

describe('public settings owner', () => {
  it('uses injection before loading and preserves the full caller DTO and saved values', async () => {
    const saved = settings()
    const { state, load, onApplied } = owner(saved)

    await expect(state.fetchPublicSettings()).resolves.toEqual(saved)

    expect(load).not.toHaveBeenCalled()
    expect(state.cachedPublicSettings.value).toEqual(saved)
    expect(state.publicSettingsLoaded.value).toBe(true)
    expect(state.publicSettingsLoading.value).toBe(false)
    expect(state.siteName.value).toBe('Saved site')
    expect(state.apiBaseUrl.value).toBe('https://api.example.test')
    expect(onApplied).toHaveBeenCalledTimes(1)
    expect(onApplied).toHaveBeenCalledWith(saved, {
      siteName: 'Saved site', siteLogo: '/saved-logo.svg'
    })
    const cached = await state.fetchPublicSettings()
    expect(cached).toEqual(saved)
    expect(cached).not.toBe(saved)
    expect(onApplied).toHaveBeenCalledTimes(1)
  })

  it('does not let stale injection overwrite a successfully refreshed snapshot', async () => {
    const injected = settings({ site_name: 'Injected site' })
    const refreshed = settings({ site_name: 'Refreshed site' })
    const { state, load, readInjected } = owner(injected)
    load.mockResolvedValue(refreshed)

    await expect(state.fetchPublicSettings(true)).resolves.toEqual(refreshed)
    expect(readInjected).not.toHaveBeenCalled()
    expect(state.initFromInjectedConfig()).toBe(false)
    await expect(state.fetchPublicSettings()).resolves.toEqual(refreshed)
    expect(state.siteName.value).toBe('Refreshed site')
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('joins one active refresh for forced and ordinary calls before considering cache or injection', async () => {
    const { state, load, onApplied } = owner(settings())
    const refreshed = settings({ site_name: 'Refreshed site' })
    const pending = deferred<TestSettings>()
    state.initFromInjectedConfig()
    load.mockReturnValue(pending.promise)

    const first = state.fetchPublicSettings(true)
    expect(state.fetchPublicSettings()).toBe(first)
    expect(state.fetchPublicSettings(true)).toBe(first)
    expect(state.initFromInjectedConfig()).toBe(false)
    expect(load).toHaveBeenCalledTimes(1)
    expect(state.publicSettingsLoading.value).toBe(true)
    expect(state.siteName.value).toBe('Saved site')

    pending.resolve(refreshed)
    await expect(first).resolves.toEqual(refreshed)
    expect(state.publicSettingsLoading.value).toBe(false)
    expect(state.siteName.value).toBe('Refreshed site')
    expect(onApplied).toHaveBeenCalledTimes(2)
  })

  it('keeps the last good snapshot on refresh failure and permits a later retry', async () => {
    const saved = settings({ backend_mode_enabled: true })
    const { state, load, onError } = owner(saved)
    const failure = new Error('Network unavailable')
    state.initFromInjectedConfig()
    load.mockRejectedValueOnce(failure).mockResolvedValueOnce(settings())

    const failed = state.fetchPublicSettings(true)
    expect(state.fetchPublicSettings()).toBe(failed)
    await expect(failed).resolves.toBeNull()
    expect(state.cachedPublicSettings.value).toEqual(saved)
    expect(state.publicSettingsLoaded.value).toBe(true)
    expect(state.backendModeEnabled.value).toBe(true)
    expect(state.publicSettingsLoading.value).toBe(false)
    expect(onError).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenCalledWith(failure)

    await expect(state.fetchPublicSettings(true)).resolves.toEqual(settings())
    expect(load).toHaveBeenCalledTimes(2)
    expect(state.backendModeEnabled.value).toBe(false)
  })

  it('releases loading after a synchronous loader failure without inventing fallback settings', async () => {
    const { state, load, onError } = owner()
    const failure = new Error('Loader setup failed')
    load.mockImplementationOnce(() => { throw failure }).mockResolvedValueOnce(settings())

    await expect(state.fetchPublicSettings()).resolves.toBeNull()
    expect(state.publicSettingsLoaded.value).toBe(false)
    expect(state.cachedPublicSettings.value).toBeNull()
    expect(state.publicSettingsLoading.value).toBe(false)
    expect(onError).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenCalledWith(failure)
    await expect(state.fetchPublicSettings()).resolves.toEqual(settings())
  })

  it('invalidates both cache and injection while retaining display values until reload', async () => {
    const { state, load, onCleared } = owner(settings({ backend_mode_enabled: true }))
    state.initFromInjectedConfig()
    state.clearPublicSettingsCache()

    expect(state.publicSettingsLoaded.value).toBe(false)
    expect(state.cachedPublicSettings.value).toBeNull()
    expect(state.backendModeEnabled.value).toBe(false)
    expect(state.siteName.value).toBe('Saved site')
    expect(onCleared).toHaveBeenCalledTimes(1)
    expect(state.initFromInjectedConfig()).toBe(false)

    const refreshed = settings({ site_name: 'Reloaded site' })
    load.mockResolvedValue(refreshed)
    await expect(state.fetchPublicSettings()).resolves.toEqual(refreshed)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('discards a late success after clear without applying callbacks or restoring the cache', async () => {
    const { state, load, onApplied } = owner()
    const pending = deferred<TestSettings>()
    load.mockReturnValue(pending.promise)
    const invalidated = state.fetchPublicSettings()

    state.clearPublicSettingsCache()
    pending.resolve(settings())

    await expect(invalidated).resolves.toBeNull()
    expect(state.cachedPublicSettings.value).toBeNull()
    expect(state.publicSettingsLoaded.value).toBe(false)
    expect(state.publicSettingsLoading.value).toBe(false)
    expect(state.siteName.value).toBe('sub2api')
    expect(onApplied).not.toHaveBeenCalled()
  })

  it('does not release a newer request or report an error when an invalidated request rejects', async () => {
    const { state, load, onError, onApplied } = owner()
    const old = deferred<TestSettings>()
    const current = deferred<TestSettings>()
    load.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const invalidated = state.fetchPublicSettings()
    state.clearPublicSettingsCache()
    const active = state.fetchPublicSettings()

    old.reject(new Error('Old generation failed'))
    await expect(invalidated).resolves.toBeNull()
    expect(onError).not.toHaveBeenCalled()
    expect(state.publicSettingsLoading.value).toBe(true)
    expect(state.fetchPublicSettings(true)).toBe(active)
    expect(load).toHaveBeenCalledTimes(2)

    current.resolve(settings({ site_name: 'Current site' }))
    await expect(active).resolves.toEqual(settings({ site_name: 'Current site' }))
    expect(onApplied).toHaveBeenCalledTimes(1)
    expect(state.publicSettingsLoading.value).toBe(false)
  })

  it('does not overwrite a completed newer generation with an older success', async () => {
    const { state, load, onApplied } = owner()
    const old = deferred<TestSettings>()
    const current = settings({ site_name: 'Current site' })
    load.mockReturnValueOnce(old.promise).mockResolvedValueOnce(current)
    const invalidated = state.fetchPublicSettings()
    state.clearPublicSettingsCache()

    await expect(state.fetchPublicSettings()).resolves.toEqual(current)
    old.resolve(settings({ site_name: 'Stale site' }))
    await expect(invalidated).resolves.toBeNull()
    expect(state.cachedPublicSettings.value).toEqual(current)
    expect(state.siteName.value).toBe('Current site')
    expect(onApplied).toHaveBeenCalledTimes(1)
  })

  it('keeps settings and in-flight requests isolated between application instances', async () => {
    const first = owner()
    const second = owner()
    const pending = deferred<TestSettings>()
    first.load.mockReturnValue(pending.promise)
    second.load.mockResolvedValue(settings({ site_name: 'Second instance' }))
    const firstLoad = first.state.fetchPublicSettings()

    await second.state.fetchPublicSettings()
    first.state.clearPublicSettingsCache()
    pending.resolve(settings({ site_name: 'First instance' }))
    await expect(firstLoad).resolves.toBeNull()
    expect(second.state.siteName.value).toBe('Second instance')
    expect(second.state.publicSettingsLoaded.value).toBe(true)
    expect(second.onCleared).not.toHaveBeenCalled()
  })
})
