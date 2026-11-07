import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAppStore } from '@/app/model/app'
import { getPublicSettings } from '@/features/site'
import { getVersion, type VersionInfo } from '@/features/system'
import { i18n, loadLocaleMessages, configureLocaleMessageLoader } from '@/i18n'
import { loadApplicationLocale } from '@/app/i18n'
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';

configureLocaleMessageLoader(loadApplicationLocale)

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })

  return { promise, resolve, reject }
}

function createPublicSettings(overrides: Partial<PublicSettings> = {}): PublicSettings {
  return {
    registration_enabled: false,
    email_verify_enabled: false,
    force_email_on_third_party_signup: false,
    registration_email_suffix_whitelist: [],
    password_reset_enabled: false,
    invitation_code_enabled: false,
    turnstile_enabled: false,
    turnstile_site_key: '',
    site_name: 'Test Site',
    site_logo: '',
    site_subtitle: '',
    api_base_url: '',
    contact_info: '',
    doc_url: '',
    home_content: '',
    compact_home_enabled: false,
    payment_enabled: false,
    risk_control_enabled: false,
    table_default_page_size: 20,
    table_page_size_options: [10, 20, 50, 100],
    custom_menu_items: [],
    custom_endpoints: [],
    linuxdo_oauth_enabled: false,
    wechat_oauth_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    backend_mode_enabled: false,
    version: '1.0.0',
    balance_low_notify_enabled: false,
    account_quota_notify_enabled: false,
    balance_low_notify_threshold: 0,
    channel_monitor_enabled: true,
    channel_monitor_default_interval_seconds: 60,
    available_channels_enabled: false,
    subscription_enabled: true,
    payment_balance_disabled: false,
    model_plaza_enabled: false,
    model_plaza_require_auth: false,
    plugin_management_enabled: false,
    service_quota_enabled: false,
    ...overrides,
  }
}

// Mock API 模块
vi.mock('@/features/system', () => ({
  getVersion: vi.fn(),
}))

vi.mock('@/features/site', () => ({
  getPublicSettings: vi.fn(),
}))

describe('useAppStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    localStorage.clear()
    vi.mocked(getPublicSettings).mockReset()
    vi.mocked(getVersion).mockReset()
    // 清除 window.__APP_CONFIG__
    delete window.__APP_CONFIG__
  })

  afterEach(() => {
    vi.useRealTimers()
    localStorage.clear()
  })

  it('applies backend branding on static portal settings load and subsequent refresh', async () => {
    document.head.innerHTML = '<link rel="icon" type="image/svg+xml" href="/logo.svg">'
    const store = useAppStore()
    vi.mocked(getPublicSettings).mockResolvedValueOnce(createPublicSettings({ site_logo: 'data:image/png;base64,aGVsbG8=' }))
    await store.fetchPublicSettings()
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')!
    expect(icon.href).toBe('data:image/png;base64,aGVsbG8=')
    expect(icon.hasAttribute('type')).toBe(false)
    vi.mocked(getPublicSettings).mockResolvedValueOnce(createPublicSettings({ site_logo: '' }))
    await store.fetchPublicSettings(true)
    expect(icon.getAttribute('href')).toBe('/logo.svg')
    store.clearPublicSettingsCache()
    window.__APP_CONFIG__ = createPublicSettings({ site_logo: '/injected-logo.svg' })
    store.initFromInjectedConfig()
    expect(icon.getAttribute('href')).toBe('/injected-logo.svg')
  })

  // --- Toast 消息管理 ---

  describe('Toast 消息管理', () => {
    it('showSuccess 创建 success 类型 toast', () => {
      const store = useAppStore()
      const id = store.showSuccess('操作成功')

      expect(id).toMatch(/^toast-/)
      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('success')
      expect(store.toasts[0].message).toBe('操作成功')
    })

    it('showError 创建 error 类型 toast', () => {
      const store = useAppStore()
      store.showError('出错了')

      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('error')
      expect(store.toasts[0].message).toBe('出错了')
    })

    it('showWarning 创建 warning 类型 toast', () => {
      const store = useAppStore()
      store.showWarning('警告信息')

      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('warning')
    })

    it('showInfo 创建 info 类型 toast', () => {
      const store = useAppStore()
      store.showInfo('提示信息')

      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('info')
    })

    it('toast 在指定 duration 后自动消失', () => {
      const store = useAppStore()
      store.showSuccess('临时消息', 3000)

      expect(store.toasts).toHaveLength(1)

      vi.advanceTimersByTime(3000)

      expect(store.toasts).toHaveLength(0)
    })

    it('hideToast 移除指定 toast', () => {
      const store = useAppStore()
      const id = store.showSuccess('消息1')
      store.showError('消息2')

      expect(store.toasts).toHaveLength(2)

      store.hideToast(id)

      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('error')
    })

    it('clearAllToasts 清除所有 toast', () => {
      const store = useAppStore()
      store.showSuccess('消息1')
      store.showError('消息2')
      store.showWarning('消息3')

      expect(store.toasts).toHaveLength(3)

      store.clearAllToasts()

      expect(store.toasts).toHaveLength(0)
    })

    it('hasActiveToasts 正确反映 toast 状态', () => {
      const store = useAppStore()
      expect(store.hasActiveToasts).toBe(false)

      store.showSuccess('消息')
      expect(store.hasActiveToasts).toBe(true)

      store.clearAllToasts()
      expect(store.hasActiveToasts).toBe(false)
    })

    it('多个 toast 的 ID 唯一', () => {
      const store = useAppStore()
      const id1 = store.showSuccess('消息1')
      const id2 = store.showSuccess('消息2')
      const id3 = store.showSuccess('消息3')

      expect(id1).not.toBe(id2)
      expect(id2).not.toBe(id3)
    })
  })

  // --- 侧边栏 ---

  describe('侧边栏管理', () => {
    it('toggleSidebar 切换折叠状态', () => {
      const store = useAppStore()
      expect(store.sidebarCollapsed).toBe(false)

      store.toggleSidebar()
      expect(store.sidebarCollapsed).toBe(true)

      store.toggleSidebar()
      expect(store.sidebarCollapsed).toBe(false)
    })

    it('setSidebarCollapsed 直接设置状态', () => {
      const store = useAppStore()

      store.setSidebarCollapsed(true)
      expect(store.sidebarCollapsed).toBe(true)

      store.setSidebarCollapsed(false)
      expect(store.sidebarCollapsed).toBe(false)
    })

    it('sidebarScrollTop 默认为 0 且可读写', () => {
      const store = useAppStore()
      expect(store.sidebarScrollTop).toBe(0)

      store.sidebarScrollTop = 256
      expect(store.sidebarScrollTop).toBe(256)

      store.sidebarScrollTop = 0
      expect(store.sidebarScrollTop).toBe(0)
    })

    it('toggleMobileSidebar 切换移动端状态', () => {
      const store = useAppStore()
      expect(store.mobileOpen).toBe(false)

      store.toggleMobileSidebar()
      expect(store.mobileOpen).toBe(true)

      store.toggleMobileSidebar()
      expect(store.mobileOpen).toBe(false)
    })
  })

  // --- Loading 状态 ---

  describe('Loading 状态管理', () => {
    it('setLoading 管理引用计数', () => {
      const store = useAppStore()
      expect(store.loading).toBe(false)

      store.setLoading(true)
      expect(store.loading).toBe(true)

      store.setLoading(true) // 两次 true
      expect(store.loading).toBe(true)

      store.setLoading(false) // 第一次 false，计数还是 1
      expect(store.loading).toBe(true)

      store.setLoading(false) // 第二次 false，计数为 0
      expect(store.loading).toBe(false)
    })

    it('setLoading(false) 不会使计数为负', () => {
      const store = useAppStore()

      store.setLoading(false)
      store.setLoading(false)
      expect(store.loading).toBe(false)

      store.setLoading(true)
      expect(store.loading).toBe(true)

      store.setLoading(false)
      expect(store.loading).toBe(false)
    })

    it('withLoading 自动管理 loading 状态', async () => {
      const store = useAppStore()

      const result = await store.withLoading(async () => {
        expect(store.loading).toBe(true)
        return 'done'
      })

      expect(result).toBe('done')
      expect(store.loading).toBe(false)
    })

    it('withLoading 错误时也恢复 loading 状态', async () => {
      const store = useAppStore()

      await expect(
        store.withLoading(async () => {
          throw new Error('操作失败')
        })
      ).rejects.toThrow('操作失败')

      expect(store.loading).toBe(false)
    })

    it('withLoadingAndError 错误时显示 toast 并返回 null', async () => {
      const store = useAppStore()

      const result = await store.withLoadingAndError(async () => {
        throw new Error('网络错误')
      })

      expect(result).toBeNull()
      expect(store.loading).toBe(false)
      expect(store.toasts).toHaveLength(1)
      expect(store.toasts[0].type).toBe('error')
    })
  })

  // --- reset ---

  describe('reset', () => {
    it('重置所有 UI 状态', () => {
      const store = useAppStore()

      store.setSidebarCollapsed(true)
      store.setLoading(true)
      store.showSuccess('消息')

      store.reset()

      expect(store.sidebarCollapsed).toBe(false)
      expect(store.loading).toBe(false)
      expect(store.toasts).toHaveLength(0)
    })
  })

  describe('只读版本信息', () => {
    it('仅缓存当前构建字段，force 只重新读取版本且清除缺失元数据', async () => {
      const initial: VersionInfo = { version: '1.0.0', upstream_version: 'v0.2.0', build_commit: 'abc123' }
      const updated: VersionInfo = { version: '1.0.1' }
      vi.mocked(getVersion).mockResolvedValueOnce(initial).mockResolvedValueOnce(updated)
      const store = useAppStore()

      await expect(store.fetchVersion()).resolves.toEqual(initial)
      await expect(store.fetchVersion()).resolves.toEqual(initial)
      expect(getVersion).toHaveBeenCalledTimes(1)
      expect(store.currentVersion).toBe('1.0.0')
      expect(store.upstreamVersion).toBe('v0.2.0')
      expect(store.buildCommit).toBe('abc123')
      expect(store.versionLoaded).toBe(true)
      expect(store.versionLoading).toBe(false)
      for (const removed of [
        'latestVersion', 'hasUpdate', 'buildType', 'updateDisabled', 'updateMethod',
        'releaseChannel', 'updateWarning', 'containerUpdate', 'releaseInfo'
      ]) {
        expect(store).not.toHaveProperty(removed)
      }

      await expect(store.fetchVersion(true)).resolves.toEqual(updated)
      expect(getVersion).toHaveBeenCalledTimes(2)
      expect(getVersion).toHaveBeenLastCalledWith({ signal: expect.any(AbortSignal) })
      expect(store.currentVersion).toBe('1.0.1')
      expect(store.upstreamVersion).toBe('')
      expect(store.buildCommit).toBe('')
      await expect(store.fetchVersion()).resolves.toEqual({
        version: '1.0.1', upstream_version: undefined, build_commit: undefined
      })
      expect(getVersion).toHaveBeenCalledTimes(2)
    })

    it('有缓存时普通调用与 force 仍等待同一个活动刷新', async () => {
      const initial: VersionInfo = { version: '1.0.0' }
      const updated: VersionInfo = { version: '1.0.1', build_commit: 'new123' }
      const pending = createDeferred<VersionInfo>()
      vi.mocked(getVersion).mockResolvedValueOnce(initial).mockReturnValueOnce(pending.promise)
      const store = useAppStore()
      await store.fetchVersion()

      const forced = store.fetchVersion(true)
      const ordinary = store.fetchVersion()
      const anotherForced = store.fetchVersion(true)
      expect(getVersion).toHaveBeenCalledTimes(2)
      expect(store.versionLoading).toBe(true)
      expect(store.currentVersion).toBe('1.0.0')

      pending.resolve(updated)
      await expect(Promise.all([forced, ordinary, anotherForced])).resolves.toEqual([
        updated, updated, updated
      ])
      expect(store.currentVersion).toBe('1.0.1')
      expect(store.versionLoading).toBe(false)
    })

    it.each(['throw', 'reject'] as const)('%s 失败保留已读版本并允许重试', async (failureMode) => {
      const initial: VersionInfo = { version: '1.0.0', build_commit: 'abc123' }
      const failure = new Error('Version unavailable')
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
      try {
        vi.mocked(getVersion).mockResolvedValueOnce(initial)
        const store = useAppStore()
        await store.fetchVersion()
        if (failureMode === 'throw') {
          vi.mocked(getVersion).mockImplementationOnce(() => { throw failure })
        } else {
          vi.mocked(getVersion).mockRejectedValueOnce(failure)
        }

        await expect(store.fetchVersion(true)).resolves.toBeNull()
        expect(store.versionLoaded).toBe(true)
        expect(store.versionLoading).toBe(false)
        expect(store.currentVersion).toBe('1.0.0')
        expect(store.buildCommit).toBe('abc123')
        await expect(store.fetchVersion()).resolves.toEqual(initial)
        expect(getVersion).toHaveBeenCalledTimes(2)
        expect(consoleError).toHaveBeenCalledTimes(1)

        vi.mocked(getVersion).mockResolvedValueOnce({ version: '1.0.1' })
        await expect(store.fetchVersion(true)).resolves.toEqual({ version: '1.0.1' })
        expect(getVersion).toHaveBeenCalledTimes(3)
      } finally {
        consoleError.mockRestore()
      }
    })

    it.each(['resolve', 'reject'] as const)('清除后旧请求 %s 不得回写或释放新请求', async (settlement) => {
      const initial: VersionInfo = { version: '1.0.0', upstream_version: 'v0.2.0', build_commit: 'abc123' }
      const old = createDeferred<VersionInfo>()
      const current = createDeferred<VersionInfo>()
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
      try {
        vi.mocked(getVersion)
          .mockResolvedValueOnce(initial)
          .mockReturnValueOnce(old.promise)
          .mockReturnValueOnce(current.promise)
        const store = useAppStore()
        await store.fetchVersion()
        const invalidated = store.fetchVersion(true)
        const oldSignal = vi.mocked(getVersion).mock.calls.at(-1)?.[0]?.signal
        store.clearVersionCache()
        expect(oldSignal?.aborted).toBe(true)
        expect(store.versionLoaded).toBe(false)
        expect(store.versionLoading).toBe(false)
        expect(store.currentVersion).toBe('')
        expect(store.upstreamVersion).toBe('')
        expect(store.buildCommit).toBe('')
        const active = store.fetchVersion()

        if (settlement === 'resolve') old.resolve({ version: 'stale' })
        else old.reject(new Error('Invalidated request failed'))
        await expect(invalidated).resolves.toBeNull()
        expect(store.versionLoaded).toBe(false)
        expect(store.currentVersion).toBe('')
        expect(store.versionLoading).toBe(true)
        expect(consoleError).not.toHaveBeenCalled()
        const joined = store.fetchVersion(true)
        expect(getVersion).toHaveBeenCalledTimes(3)

        const latest: VersionInfo = { version: '1.0.2', build_commit: 'new456' }
        current.resolve(latest)
        await expect(Promise.all([active, joined])).resolves.toEqual([latest, latest])
        expect(store.versionLoaded).toBe(true)
        expect(store.versionLoading).toBe(false)
        expect(store.currentVersion).toBe('1.0.2')
        expect(store.buildCommit).toBe('new456')
      } finally {
        consoleError.mockRestore()
      }
    })
  })

  // --- 公开设置 ---

  describe('公开设置加载', () => {
    it('并发调用复用并等待同一个请求，包括 force 调用', async () => {
      const deferred = createDeferred<PublicSettings>()
      vi.mocked(getPublicSettings).mockReturnValue(deferred.promise)
      const settings = createPublicSettings({ payment_enabled: true })
      const store = useAppStore()

      const first = store.fetchPublicSettings()
      const second = store.fetchPublicSettings()
      const forced = store.fetchPublicSettings(true)

      expect(getPublicSettings).toHaveBeenCalledTimes(1)

      const settled = vi.fn()
      void first.then(settled)
      void second.then(settled)
      void forced.then(settled)
      await Promise.resolve()
      expect(settled).not.toHaveBeenCalled()

      deferred.resolve(settings)
      await expect(Promise.all([first, second, forced])).resolves.toEqual([
        settings,
        settings,
        settings,
      ])
      expect(store.publicSettingsLoaded).toBe(true)
      expect(store.cachedPublicSettings?.payment_enabled).toBe(true)
    })

    it('force 在无活动请求时绕过缓存，刷新期间的普通调用等待刷新结果', async () => {
      const initial = createPublicSettings({ site_name: 'Initial Site' })
      vi.mocked(getPublicSettings).mockResolvedValueOnce(initial)
      const store = useAppStore()
      await store.fetchPublicSettings()

      const deferred = createDeferred<PublicSettings>()
      const updated = createPublicSettings({ site_name: 'Updated Site' })
      vi.mocked(getPublicSettings).mockReturnValueOnce(deferred.promise)

      const refresh = store.fetchPublicSettings(true)
      const duringRefresh = store.fetchPublicSettings()

      expect(getPublicSettings).toHaveBeenCalledTimes(2)

      deferred.resolve(updated)
      await expect(Promise.all([refresh, duringRefresh])).resolves.toEqual([updated, updated])
      expect(store.siteName).toBe('Updated Site')

      await expect(store.fetchPublicSettings()).resolves.toEqual(updated)
      expect(getPublicSettings).toHaveBeenCalledTimes(2)
    })

    it('并发请求失败时所有调用得到 null，且不会标记设置已加载', async () => {
      const deferred = createDeferred<PublicSettings>()
      vi.mocked(getPublicSettings).mockReturnValue(deferred.promise)
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
      const store = useAppStore()

      const first = store.fetchPublicSettings()
      const second = store.fetchPublicSettings()
      deferred.reject(new Error('network unavailable'))

      await expect(Promise.all([first, second])).resolves.toEqual([null, null])
      expect(getPublicSettings).toHaveBeenCalledTimes(1)
      expect(store.publicSettingsLoaded).toBe(false)
      expect(store.cachedPublicSettings).toBeNull()
      consoleError.mockRestore()
    })

    it('从 window.__APP_CONFIG__ 初始化', () => {
      window.__APP_CONFIG__ = createPublicSettings({
        site_name: 'TestSite',
        site_logo: '/logo.png',
        version: '1.0.0',
        contact_info: 'test@test.com',
        api_base_url: 'https://api.test.com',
        doc_url: 'https://docs.test.com',
      })

      const store = useAppStore()
      const result = store.initFromInjectedConfig()

      expect(result).toBe(true)
      expect(store.siteName).toBe('TestSite')
      expect(store.siteLogo).toBe('/logo.png')
      expect(store.siteVersion).toBe('1.0.0')
      expect(store.publicSettingsLoaded).toBe(true)
    })

    it('无注入配置时返回 false', () => {
      const store = useAppStore()
      const result = store.initFromInjectedConfig()

      expect(result).toBe(false)
      expect(store.publicSettingsLoaded).toBe(false)
    })

    it('clearPublicSettingsCache 清除缓存', () => {
      window.__APP_CONFIG__ = createPublicSettings({ site_name: 'Test' })
      const store = useAppStore()
      store.initFromInjectedConfig()

      expect(store.publicSettingsLoaded).toBe(true)

      store.clearPublicSettingsCache()

      expect(store.publicSettingsLoaded).toBe(false)
      expect(store.cachedPublicSettings).toBeNull()
    })

    it('清除使旧请求失效，迟到结果不能覆盖新配置、favicon 或站点文案', async () => {
      await loadLocaleMessages('en')
      document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
      window.__APP_CONFIG__ = createPublicSettings({ site_name: 'Initial site' })
      const store = useAppStore()
      store.initFromInjectedConfig()
      const stale = createDeferred<PublicSettings>()
      const fresh = createDeferred<PublicSettings>()
      vi.mocked(getPublicSettings)
        .mockReturnValueOnce(stale.promise)
        .mockReturnValueOnce(fresh.promise)
      const invalidated = store.fetchPublicSettings(true)

      store.clearPublicSettingsCache()
      expect(store.cachedPublicSettings).toBeNull()
      expect(store.publicSettingsLoaded).toBe(false)
      expect(window.__APP_CONFIG__).toBeUndefined()
      expect(store.siteName).toBe('Initial site')
      const active = store.fetchPublicSettings()
      expect(getPublicSettings).toHaveBeenCalledTimes(2)

      const latest = createPublicSettings({
        site_name: 'Current site',
        site_logo: '/current-logo.svg',
        table_default_page_size: 100,
      })
      fresh.resolve(latest)
      await expect(active).resolves.toEqual(latest)
      stale.resolve(createPublicSettings({ site_name: 'Stale site', site_logo: '/stale.svg' }))
      await expect(invalidated).resolves.toBeNull()

      expect(store.cachedPublicSettings).toEqual(latest)
      expect(window.__APP_CONFIG__).toEqual(latest)
      expect(store.publicSettingsLoaded).toBe(true)
      expect(store.publicSettingsLoading).toBe(false)
      expect(store.siteName).toBe('Current site')
      expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/current-logo.svg')
      expect(i18n.global.t('common.siteName', {}, { locale: 'en' })).toBe('Current site')
    })

    it('fetchPublicSettings(force) 会同步更新运行时注入配置', async () => {
      vi.mocked(getPublicSettings).mockResolvedValue(createPublicSettings({
        site_name: 'Updated Site',
        table_default_page_size: 1000,
        table_page_size_options: [20, 100, 1000],
      }))

      const store = useAppStore()
      await store.fetchPublicSettings(true)

      expect(window.__APP_CONFIG__?.table_default_page_size).toBe(1000)
      expect(window.__APP_CONFIG__?.table_page_size_options).toEqual([20, 100, 1000])
      expect(localStorage.getItem('table-page-size')).toBeNull()
      expect(localStorage.getItem('table-page-size-source')).toBeNull()
    })
  })
})
