import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, defineComponent, h, type App } from 'vue'
import { createPinia, disposePinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import type { InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/shared/http'
import { installApplicationHttp } from '@/app/http/install'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import { globalAdminStepUp, useAuthStore, identityNotificationsKey, type AdminRolePermissionsResponse, type User } from '@/features/identity'
import type { SystemSettings } from '@/features/system/api/settings'
import SettingsWorkspace from '../SettingsWorkspace.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import DelegatedSettings from '../DelegatedSettings.vue'
import { settingsWorkspaceServicesKey, useSettingsWorkspaceServices, type SettingsWorkspaceServices } from '../ports'
import { installSettingsServices } from '../installServices'
import { settingsFixture } from './fixture'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en', setSiteName: vi.fn(), i18n: { global: { t: (key: string) => key } } }))
// Resolve the real site editor eagerly so shallow mounting can isolate unrelated panels.
vi.mock('@/features/site/settings', async original => ({
  ...await original<typeof import('@/features/site/settings')>(),
  AppearanceSettingsPanel: (await import('@/features/site/settings/AppearanceSettingsPanel.vue')).default
}))

const roleLeave = vi.hoisted(() => vi.fn<() => Promise<boolean>>())
const roleEditor = defineComponent({
  setup(_, { expose }) {
    expose({ confirmLeave: roleLeave })
    return () => h('div', { 'data-role-editor': '' })
  }
})

const revisionA = 'a'.repeat(64), revisionB = 'b'.repeat(64)
const snapshot = (): SystemSettings => ({ ...structuredClone(settingsFixture), site_name: 'Original', login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'Original terms' }] })
const allPermissions = ['settings.general.read', 'settings.general.manage', 'settings.content.read', 'settings.content.manage']
const emptyPanel = defineComponent({ render: () => null })
const cleanup: (() => void)[] = []
const originalAdapter = apiClient.defaults.adapter
const originalBaseURL = apiClient.defaults.baseURL
const originalTimeout = apiClient.defaults.timeout
const originalInjected = window.__APP_CONFIG__
// Synthetic public response: only the fields consumed by these projection tests.
const publicSnapshot = (siteName: string) => ({
  site_name: siteName, site_logo: `/${siteName.toLowerCase()}-logo.svg`, version: 'test',
  contact_info: '', api_base_url: '', doc_url: '', backend_mode_enabled: false, payment_enabled: true
})

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Promise was not initialized') }
  let reject: (reason: unknown) => void = () => { throw new Error('Promise was not initialized') }
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function token(sid: string, role: User['role'], iat = 0): string {
  return `e30.${btoa(JSON.stringify({ user_id: 7, sid, role, token_version: 0, iat }))}.signature`
}
function fail(config: InternalAxiosRequestConfig, status: number, code: string): never {
  throw { config, response: { status, data: { code, message: code } } }
}
async function setup(role: 'super_admin' | 'admin' = 'super_admin', permissions = allPermissions, realRefresh = false, realRoles = false) {
  const pinia = createPinia()
  cleanup.push(() => disposePinia(pinia))
  const auth = useAuthStore(pinia)
  auth.token = token('same-session', role)
  auth.user = {
    id: 7, role, permissions: [...permissions], policy_version: 1, username: 'test', email: 'test@example.test',
    balance: 0, concurrency: 1, status: 'active', allowed_groups: null, balance_notify_enabled: false,
    balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '', updated_at: ''
  }
  localStorage.setItem('auth_token', auth.token)
  localStorage.setItem('auth_user', JSON.stringify(auth.user))
  let server = snapshot()
  const get = vi.fn(async () => structuredClone(server))
  const publicGet = vi.fn(async () => publicSnapshot(server.site_name))
  const paymentGet = vi.fn(async () => ({ enabled: true }))
  let rolePolicy: AdminRolePermissionsResponse = {
    policy: { version: 7, permissions: [], updated_by: 1, updated_at: '' },
    catalogue: [{ key: 'accounts.read', module: 'accounts', group: 'resources', sensitive: false, dependencies: [], label: '', aliases: [] }],
    affected_admin_count: 3, step_up_enabled: true
  }
  const roleGet = vi.fn(async () => structuredClone(rolePolicy))
  const rolePut = vi.fn(async (config: InternalAxiosRequestConfig) => {
    const submitted = JSON.parse(config.data)
    rolePolicy = { ...rolePolicy, policy: { ...rolePolicy.policy, version: 8, permissions: submitted.permissions } }
    return structuredClone(rolePolicy)
  })
  apiClient.defaults.baseURL = 'http://localhost/api/v1'
  // Requests settle at explicit deferred barriers; avoid a jsdom timeout signal in Node's Request.
  apiClient.defaults.timeout = 0
  // Anonymous settings deliberately select Axios's fetch adapter, bypassing defaults.adapter.
  vi.stubGlobal('fetch', vi.fn(async (input: Request | string | URL) => {
    if (!(input instanceof Request) || new URL(input.url).pathname !== '/api/v1/settings/public') {
      throw new Error('Unexpected public request')
    }
    expect(input.credentials).toBe('omit')
    expect(input.headers.get('Authorization')).toBeNull()
    const data = await publicGet()
    return new Response(JSON.stringify({ code: 0, data }), {
      status: 200, headers: { 'Content-Type': 'application/json' }
    })
  }))
  const put = vi.fn(async (config: InternalAxiosRequestConfig): Promise<SystemSettings> => {
    server = { ...server, ...JSON.parse(config.data), revision: revisionB }
    return structuredClone(server)
  })
  apiClient.defaults.adapter = async config => {
    let data: unknown
    if (config.url === '/auth/me') data = auth.user
    else if (config.url === '/admin/roles/admin/permissions') data = await (config.method === 'put' ? rolePut(config) : roleGet())
    else if (config.url === '/admin/payment/config') data = await paymentGet()
    else if (config.url === '/admin/settings') {
      data = await (config.method === 'put' ? put(config).then(updated => {
        server = structuredClone(updated)
        return updated
      }) : get())
    } else throw new Error(`Unexpected request: ${config.url}`)
    return { config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} }
  }
  cleanup.push(installApplicationHttp())
  const services: SettingsWorkspaceServices = {
    scope: computed(() => auth.isAuthenticated ? auth.identityScopeKey : null),
    get isSuperAdmin() { return auth.isSuperAdmin },
    notifications: { showSuccess: vi.fn(), showError: vi.fn() },
    DelegatedSettings,
    refreshSettings: vi.fn().mockResolvedValue(undefined)
  }
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/admin/settings', component: SettingsWorkspace },
    { path: '/admin/other', component: emptyPanel }
  ] })
  await router.push('/admin/settings')
  await router.isReady()
  const appStore = useAppStore(pinia)
  const adminStore = useAdminSettingsStore(pinia)
  const servicesPlugin = { install(app: App) {
    app.provide(identityNotificationsKey, { ...services.notifications, showInfo: vi.fn(), showWarning: vi.fn() })
    if (!realRefresh) { app.provide(settingsWorkspaceServicesKey, services); return }
    app.provide(uiFeedbackKey, { success: vi.fn(), error: vi.fn() })
    app.runWithContext(() => installSettingsServices(app))
    const installed = app.runWithContext(useSettingsWorkspaceServices)
    // Use production refresh wiring and stores; isolate unrelated panels and capture feedback.
    installed.notifications = services.notifications
    installed.DelegatedSettings = DelegatedSettings
    installed.refreshSettings = vi.fn(installed.refreshSettings)
    services.refreshSettings = installed.refreshSettings
  } }
  const wrapper = mount(RouterView, { attachTo: document.body, shallow: true, global: {
    plugins: [pinia, router, servicesPlugin],
    stubs: { AdminRolePermissions: realRoles ? false : roleEditor, BaseDialog: false, ConfirmDialog: false, Teleport: true, Transition: false, SettingsWorkspace: false, DelegatedSettings: false, SettingsConflictReview: false, AppearanceSettingsPanel: false }
  } })
  cleanup.push(() => wrapper.unmount())
  await flushPromises()
  return { wrapper, router, services, get, put, auth, publicGet, paymentGet, appStore, adminStore, roleGet, rolePut }
}

beforeEach(() => {
  roleLeave.mockReset().mockResolvedValue(true)
  localStorage.clear()
  globalAdminStepUp.onCancel()
  delete window.__APP_CONFIG__
  const favicon = document.querySelector('link[rel="icon"]')?.cloneNode(true)
  cleanup.push(() => {
    document.querySelector('link[rel="icon"]')?.remove()
    if (favicon) document.head.appendChild(favicon)
  })
})
afterEach(async () => {
  cleanup.splice(0).reverse().forEach(dispose => dispose())
  globalAdminStepUp.onCancel()
  await flushPromises()
  apiClient.defaults.adapter = originalAdapter
  apiClient.defaults.baseURL = originalBaseURL
  apiClient.defaults.timeout = originalTimeout
  if (originalInjected) window.__APP_CONFIG__ = originalInjected
  else delete window.__APP_CONFIG__
  localStorage.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('settings page save through API, HTTP and global step-up', () => {
  it.each(['super_admin', 'admin'] as const)('does not replay %s PUT after leaving for another admin page in the same session', async role => {
    const { wrapper, router, put, services } = await setup(role)
    put.mockImplementationOnce(async config => fail(config, 403, 'STEP_UP_REQUIRED'))
    const input = role === 'super_admin' ? 'input[placeholder="admin.settings.site.siteNamePlaceholder"]' : '#delegated-site_name'
    const button = role === 'super_admin' ? '[data-testid="save-settings"]' : '[data-testid="save-general"]'
    await wrapper.get(input).setValue('Mine')
    await wrapper.get(button).trigger('click')
    await flushPromises()
    expect(globalAdminStepUp.visible.value).toBe(true)
    expect(put).toHaveBeenCalledTimes(1)
    const signal = put.mock.calls[0][0].signal
    expect(signal?.aborted).toBe(false)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await router.push('/admin/other')
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    expect(localStorage.getItem('auth_token')).toBe(token('same-session', role))
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).not.toHaveBeenCalled()
    expect(services.notifications.showError).not.toHaveBeenCalled()
    expect(services.refreshSettings).not.toHaveBeenCalled()
  })

  it.each([false, true])('retries exactly once on the active page, including a second STEP_UP_REQUIRED=%s', async rejectRetry => {
    const { wrapper, put, services } = await setup()
    put.mockImplementationOnce(async config => fail(config, 403, 'STEP_UP_REQUIRED'))
    if (rejectRetry) put.mockImplementationOnce(async config => fail(config, 403, 'STEP_UP_REQUIRED'))
    await wrapper.get('input[placeholder="admin.settings.site.siteNamePlaceholder"]').setValue('Mine')
    await wrapper.get('[data-testid="save-settings"]').trigger('click')
    await flushPromises()
    expect(globalAdminStepUp.visible.value).toBe(true)
    // Refreshing a token within this logical session must not cancel the save.
    localStorage.setItem('auth_token', token('same-session', 'super_admin', 1))
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(2)
    expect(put.mock.calls.map(([config]) => JSON.parse(config.data))).toEqual([
      { revision: revisionA, site_name: 'Mine' }, { revision: revisionA, site_name: 'Mine' }
    ])
    expect(put.mock.calls[1][0].signal).toBe(put.mock.calls[0][0].signal)
    expect(globalAdminStepUp.visible.value).toBe(false)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(rejectRetry ? 0 : 1)
    if (rejectRetry) expect(wrapper.get('[role="alert"]').text()).toContain('STEP_UP_REQUIRED')
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(2)
  })

  it('cancels a pending save when the authorization scope changes and silently keeps cancelled drafts', async () => {
    const { wrapper, put, auth, services } = await setup('admin')
    put.mockImplementationOnce(async config => fail(config, 403, 'STEP_UP_REQUIRED'))
    await wrapper.get('#delegated-site_name').setValue('Mine')
    await wrapper.get('[data-testid="save-general"]').trigger('click')
    await flushPromises()
    globalAdminStepUp.onCancel()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect((wrapper.get('#delegated-site_name').element as HTMLInputElement).value).toBe('Mine')
    put.mockImplementationOnce(async config => fail(config, 403, 'STEP_UP_REQUIRED'))
    await wrapper.get('[data-testid="save-general"]').trigger('click')
    await flushPromises()
    const signal = put.mock.calls[1][0].signal
    if (!auth.user) throw new Error('Missing test identity')
    auth.user.policy_version = 2
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(2)
    expect(services.notifications.showSuccess).not.toHaveBeenCalled()
    expect(services.notifications.showError).not.toHaveBeenCalled()
  })
})

describe('delegated settings share the aggregate transaction', () => {
  it.each(['general', 'content'] as const)('acknowledges only the saved %s section and preserves untouched drafts in the other section', async section => {
    const { wrapper, get, put } = await setup('admin')
    await wrapper.get('#delegated-site_name').setValue('Mine')
    await wrapper.get('textarea').setValue('My terms')
    await wrapper.get(`[data-testid="save-${section}"]`).trigger('click')
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#delegated-site_name').element as HTMLInputElement).value).toBe('Mine')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('My terms')
    const other = section === 'general' ? 'content' : 'general'
    expect(wrapper.get(`[data-testid="save-${section}"]`).attributes('disabled')).toBeDefined()
    expect(wrapper.get(`[data-testid="save-${other}"]`).attributes('disabled')).toBeUndefined()
    await wrapper.get(`[data-testid="save-${other}"]`).trigger('click')
    await flushPromises()
    const fields = section === 'general' ? { login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'My terms' }] } : { site_name: 'Mine' }
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB, ...fields })
  })

  it.each(['general', 'content'] as const)('saving %s keeps the other section and edits made during submission', async section => {
    const { wrapper, get, put, services } = await setup('admin')
    await wrapper.get('#delegated-site_name').setValue('Mine')
    await wrapper.get('textarea').setValue('My terms')
    const response = deferred<SystemSettings>()
    put.mockReturnValueOnce(response.promise)
    await wrapper.get(`[data-testid="save-${section}"]`).trigger('click')
    await flushPromises()
    const submitted = section === 'general' ? { site_name: 'Mine' } : { login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'My terms' }] }
    expect(JSON.parse(put.mock.calls[0][0].data)).toEqual({ revision: revisionA, ...submitted })
    // Returning a submitted field to its original baseline still counts as a newer edit.
    await wrapper.get('#delegated-site_name').setValue('Original')
    await wrapper.get('textarea').setValue('Newer terms')
    response.resolve({ ...snapshot(), ...submitted, revision: revisionB })
    await flushPromises()
    expect((wrapper.get('#delegated-site_name').element as HTMLInputElement).value).toBe('Original')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Newer terms')
    expect(get).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="save-content"]').trigger('click')
    await flushPromises()
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB, login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'Newer terms' }] })
    if (section === 'general') {
      expect(wrapper.get('[data-testid="save-general"]').attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-testid="save-general"]').trigger('click')
      await flushPromises()
      expect(JSON.parse(put.mock.calls[2][0].data)).toEqual({ revision: revisionB, site_name: 'Original' })
    }
  })

  it('reloads 409 differences without losing either draft and only saves after an explicit merge and submit', async () => {
    const { wrapper, get, put } = await setup('admin')
    await wrapper.get('#delegated-site_name').setValue('Mine')
    await wrapper.get('textarea').setValue('My terms')
    put.mockImplementationOnce(async config => fail(config, 409, 'SETTINGS_REVISION_CONFLICT'))
    get.mockResolvedValueOnce({ ...snapshot(), revision: revisionB, site_name: 'Theirs', login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'Their terms' }] })
    await wrapper.get('[data-testid="save-general"]').trigger('click')
    await flushPromises()
    const conflict = wrapper.get('[data-testid="settings-conflict"]')
    expect(conflict.get('[data-field="site_name"]').text()).toContain('Original')
    expect(conflict.get('[data-field="site_name"]').text()).toContain('Theirs')
    expect(conflict.get('[data-field="site_name"]').text()).toContain('Mine')
    expect(conflict.get('[data-field="login_agreement_documents"]').text()).toContain('My terms')
    expect(get).toHaveBeenCalledTimes(2)
    expect(put).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="save-general"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="save-content"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#delegated-site_name').setValue('Revised mine')
    await conflict.get('input[value="site_name"]').setValue(true)
    await wrapper.get('[data-testid="merge-settings"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="settings-conflict"]').exists()).toBe(false)
    expect((wrapper.get('#delegated-site_name').element as HTMLInputElement).value).toBe('Revised mine')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Their terms')
    expect(put).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="save-general"]').trigger('click')
    await flushPromises()
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB, site_name: 'Revised mine' })
  })

  it('enforces section read/manage permissions for inputs, comparison and submitted fields', async () => {
    const { wrapper, put, get } = await setup('admin', ['settings.general.read', 'settings.content.read', 'settings.content.manage'])
    expect(wrapper.get('#delegated-site_name').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="save-general"]').exists()).toBe(false)
    // Synthetic events cannot bypass the patch/submit permission filters either.
    await wrapper.get('#delegated-site_name').setValue('Forbidden')
    await wrapper.get('textarea').setValue('My terms')
    put.mockImplementationOnce(async config => fail(config, 409, 'SETTINGS_REVISION_CONFLICT'))
    get.mockResolvedValueOnce({ ...snapshot(), revision: revisionB, site_name: 'Theirs', smtp_host: 'private-host' })
    await wrapper.get('[data-testid="save-content"]').trigger('click')
    await flushPromises()
    expect(Object.keys(JSON.parse(put.mock.calls[0][0].data)).sort()).toEqual(['login_agreement_documents', 'revision'])
    const review = wrapper.get('[data-testid="settings-conflict"]')
    expect(review.find('[data-field="site_name"] input').exists()).toBe(false)
    expect(review.find('[data-field="smtp_host"]').exists()).toBe(false)
    expect(review.text()).not.toContain('Forbidden')
    expect(review.text()).not.toContain('private-host')
  })
})

describe('settings save refreshes the installed application projections', () => {
  const inputFor = (role: 'super_admin' | 'admin') => role === 'super_admin'
    ? 'input[placeholder="admin.settings.site.siteNamePlaceholder"]' : '#delegated-site_name'
  const buttonFor = (role: 'super_admin' | 'admin') => role === 'super_admin'
    ? '[data-testid="save-settings"]' : '[data-testid="save-general"]'

  it.each(['super_admin', 'admin'] as const)('starts a new public GET after %s saves and rejects a late pre-save snapshot', async role => {
    const { wrapper, publicGet, appStore, adminStore, put, services } = await setup(role, allPermissions, true)
    await appStore.fetchPublicSettings()
    expect(appStore.siteName).toBe('Original')
    const old = deferred<ReturnType<typeof publicSnapshot>>()
    publicGet.mockReturnValueOnce(old.promise)
    const staleRead = appStore.fetchPublicSettings(true)
    await flushPromises()
    await wrapper.get(inputFor(role)).setValue('Saved')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(publicGet).toHaveBeenCalledTimes(3)
    expect(services.refreshSettings).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledTimes(1)
    expect(appStore.siteName).toBe('Saved')
    expect(appStore.cachedPublicSettings?.site_name).toBe('Saved')
    expect(window.__APP_CONFIG__?.site_name).toBe('Saved')
    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/saved-logo.svg')
    expect(adminStore.paymentEnabled).toBe(true)
    old.resolve(publicSnapshot('Stale'))
    await expect(staleRead).resolves.toBeNull()
    expect(appStore.siteName).toBe('Saved')
    expect(window.__APP_CONFIG__?.site_name).toBe('Saved')
    expect(appStore.publicSettingsLoading).toBe(false)
    expect(services.notifications.showError).not.toHaveBeenCalled()
  })

  it.each(['super_admin', 'admin'] as const)('keeps the latest %s save when its earlier refresh settles last', async role => {
    const { wrapper, publicGet, appStore, put, services } = await setup(role, allPermissions, true)
    const first = deferred<ReturnType<typeof publicSnapshot>>()
    publicGet.mockReturnValueOnce(first.promise)
    await wrapper.get(inputFor(role)).setValue('First')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(publicGet).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    await wrapper.get(inputFor(role)).setValue('Second')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(put.mock.calls.map(([config]) => JSON.parse(config.data))).toEqual([
      { revision: revisionA, site_name: 'First' }, { revision: revisionB, site_name: 'Second' }
    ])
    expect(publicGet).toHaveBeenCalledTimes(2)
    expect(appStore.siteName).toBe('Second')
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(2)
    first.resolve(publicSnapshot('First'))
    await flushPromises()
    expect(appStore.siteName).toBe('Second')
    expect(window.__APP_CONFIG__?.site_name).toBe('Second')
    expect(services.notifications.showError).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get(buttonFor(role)).attributes('disabled')).toBeDefined()
  })

  it('retains post-submit edits and the other delegated section while the real refresh completes', async () => {
    const { wrapper, publicGet, appStore, get, put } = await setup('admin', allPermissions, true)
    const saved = deferred<SystemSettings>()
    const refreshed = deferred<ReturnType<typeof publicSnapshot>>()
    put.mockReturnValueOnce(saved.promise)
    publicGet.mockReturnValueOnce(refreshed.promise)
    await wrapper.get('#delegated-site_name').setValue('Submitted')
    await wrapper.get('textarea').setValue('Unsaved terms')
    await wrapper.get('[data-testid="save-general"]').trigger('click')
    await flushPromises()
    // The user restores the original value during PUT; it is still a newer edit.
    await wrapper.get('#delegated-site_name').setValue('Original')
    saved.resolve({ ...snapshot(), revision: revisionB, site_name: 'Submitted' })
    await flushPromises()
    expect(publicGet).toHaveBeenCalledTimes(1)
    await wrapper.get('textarea').setValue('Newer unsaved terms')
    refreshed.resolve(publicSnapshot('Submitted'))
    await flushPromises()
    expect(appStore.siteName).toBe('Submitted')
    expect((wrapper.get('#delegated-site_name').element as HTMLInputElement).value).toBe('Original')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Newer unsaved terms')
    expect(get).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="save-general"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="save-content"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="save-content"]').trigger('click')
    await flushPromises()
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB,
      login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'Newer unsaved terms' }]
    })
    expect(wrapper.get('[data-testid="save-general"]').attributes('disabled')).toBeUndefined()
  })

  it.each(['super_admin', 'admin'] as const)('reports a confirmed %s save separately from refresh failure without replaying PUT', async role => {
    const { wrapper, publicGet, appStore, put, services } = await setup(role, allPermissions, true)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    publicGet.mockRejectedValueOnce(new Error('Public GET unavailable'))
    await wrapper.get(inputFor(role)).setValue('Saved')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    expect(services.notifications.showError).toHaveBeenCalledTimes(1)
    expect(services.notifications.showError).toHaveBeenCalledWith('admin.settings.workspace.refreshFailed')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get(buttonFor(role)).attributes('disabled')).toBeDefined()
    expect((wrapper.get(inputFor(role)).element as HTMLInputElement).value).toBe('Saved')
    expect(appStore.publicSettingsLoading).toBe(false)
    // Reloading the projection succeeds without another write or resetting the editor baseline.
    await services.refreshSettings()
    expect(appStore.siteName).toBe('Saved')
    expect(put).toHaveBeenCalledTimes(1)
    await wrapper.get(inputFor(role)).setValue('Next')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB, site_name: 'Next' })
    expect(services.notifications.showError).toHaveBeenCalledTimes(1)
  })

  it.each(['settings', 'payment'] as const)('reports a failed private %s refresh after saving without replay or losing newer edits', async source => {
    const { wrapper, get, paymentGet, put, services, adminStore } = await setup('super_admin', allPermissions, true)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const pending = deferred<never>()
    if (source === 'settings') get.mockReturnValueOnce(pending.promise)
    else paymentGet.mockReturnValueOnce(pending.promise)
    await wrapper.get(inputFor('super_admin')).setValue('Saved')
    await wrapper.get(buttonFor('super_admin')).trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    await wrapper.get(inputFor('super_admin')).setValue('Newer draft')
    pending.reject(new Error('Private projection unavailable'))
    await flushPromises()
    expect(services.notifications.showError).toHaveBeenCalledTimes(1)
    expect(services.notifications.showError).toHaveBeenCalledWith('admin.settings.workspace.refreshFailed')
    expect((wrapper.get(inputFor('super_admin')).element as HTMLInputElement).value).toBe('Newer draft')
    expect(wrapper.get(buttonFor('super_admin')).attributes('disabled')).toBeUndefined()
    expect(adminStore.loading).toBe(false)
    await services.refreshSettings()
    expect(adminStore.loaded).toBe(true)
    expect(put).toHaveBeenCalledTimes(1)
    await wrapper.get(buttonFor('super_admin')).trigger('click')
    await flushPromises()
    expect(JSON.parse(put.mock.calls[1][0].data)).toEqual({ revision: revisionB, site_name: 'Newer draft' })
    expect(services.notifications.showError).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['super_admin', 'leave'], ['admin', 'leave'], ['super_admin', 'scope'], ['admin', 'scope'],
    ['admin', 'permissions']
  ] as const)('suppresses the old %s refresh notification after %s invalidation, including a scope round trip', async (role, change) => {
    const { wrapper, router, publicGet, put, auth, services } = await setup(role, allPermissions, true)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const refreshed = deferred<ReturnType<typeof publicSnapshot>>()
    publicGet.mockReturnValueOnce(refreshed.promise)
    await wrapper.get(inputFor(role)).setValue('Saved')
    await wrapper.get(buttonFor(role)).trigger('click')
    await flushPromises()
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    if (change === 'leave') await router.push('/admin/other')
    else {
      if (!auth.user) throw new Error('Missing test identity')
      if (change === 'scope') {
        auth.user.policy_version = 2
        auth.user.policy_version = 1
      } else {
        auth.user.permissions = ['settings.general.read']
        auth.user.permissions = [...allPermissions]
      }
    }
    await flushPromises()
    refreshed.reject(new Error('Late public refresh failure'))
    await flushPromises()
    expect(put).toHaveBeenCalledTimes(1)
    expect(services.notifications.showSuccess).toHaveBeenCalledTimes(1)
    expect(services.notifications.showError).not.toHaveBeenCalled()
  })
})


describe('settings role editor navigation', () => {
  it('waits for role confirmation, keeps both drafts on cancel and unmounts roles only on acceptance', async () => {
    const { wrapper, put, services } = await setup()
    const input = 'input[placeholder="admin.settings.site.siteNamePlaceholder"]'
    await wrapper.get(input).setValue('Keep the settings draft')
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-role-editor]').exists()).toBe(true)
    roleLeave.mockResolvedValueOnce(false)
    await wrapper.get('[data-settings-tab=general]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
    expect(wrapper.find('[data-role-editor]').exists()).toBe(true)
    expect((wrapper.get(input).element as HTMLInputElement).value).toBe('Keep the settings draft')
    roleLeave.mockResolvedValueOnce(true)
    await wrapper.get('[data-settings-tab=general]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-settings-tab=general]').attributes('aria-current')).toBe('page')
    expect(wrapper.find('[data-role-editor]').exists()).toBe(false)
    expect((wrapper.get(input).element as HTMLInputElement).value).toBe('Keep the settings draft')
    expect(put).not.toHaveBeenCalled()
    await wrapper.get('[data-testid=save-settings]').trigger('click'); await flushPromises()
    expect(JSON.parse(put.mock.calls[0][0].data)).toEqual({ revision: revisionA, site_name: 'Keep the settings draft' })
    expect(services.refreshSettings).toHaveBeenCalledTimes(1)
  })

  it.each(['session', 'scope-round-trip', 'revoke', 'leave'] as const)('ignores an accepted role confirmation after %s', async boundary => {
    const { wrapper, auth, router, put } = await setup()
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    const pending = deferred<boolean>()
    roleLeave.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-settings-tab=general]').trigger('click')
    expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
    if (boundary === 'leave') await router.push('/admin/other')
    else if (boundary === 'revoke') {
      if (!auth.user) throw new Error('Missing fixture user')
      auth.user.role = 'admin'
    } else {
      if (boundary === 'session') {
        localStorage.setItem('auth_token', token('new-session', 'super_admin'))
        auth.checkAuth()
      } else {
        if (!auth.user) throw new Error('Missing fixture user')
        auth.user.policy_version = 2
        auth.user.policy_version = 1
      }
    }
    await flushPromises()
    pending.resolve(true); await flushPromises()
    expect(put).not.toHaveBeenCalled()
    if (boundary === 'session' || boundary === 'scope-round-trip') {
      expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
    } else expect(wrapper.find('[data-settings-tab=general]').exists()).toBe(false)
  })

  it('does not let an older confirmation override a newer tab request', async () => {
    const { wrapper } = await setup()
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    const first = deferred<boolean>()
    roleLeave.mockReturnValueOnce(first.promise).mockResolvedValueOnce(false)
    await wrapper.get('[data-settings-tab=general]').trigger('click')
    await wrapper.get('[data-settings-tab=users]').trigger('click'); await flushPromises()
    first.resolve(true); await flushPromises()
    expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
  })

  it('blocks tab changes while the settings transaction saves without changing its save strategy', async () => {
    const { wrapper, put, services } = await setup()
    const pending = deferred<SystemSettings>()
    put.mockReturnValueOnce(pending.promise)
    await wrapper.get('input[placeholder="admin.settings.site.siteNamePlaceholder"]').setValue('Saving now')
    await wrapper.get('[data-testid=save-settings]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-settings-tab=roles]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-role-editor]').exists()).toBe(false)
    expect(roleLeave).not.toHaveBeenCalled()
    pending.resolve({ ...snapshot(), revision: revisionB, site_name: 'Saving now' }); await flushPromises()
    expect(put).toHaveBeenCalledTimes(1)
    expect(services.refreshSettings).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-settings-tab=roles]').attributes('disabled')).toBeUndefined()
  })
})


describe('settings workspace with the real identity role editor', () => {
  it('blocks tabs while a role PUT is pending, then preserves and saves the independent settings draft', async () => {
    const { wrapper, put, rolePut, roleGet } = await setup('super_admin', allPermissions, false, true)
    const input = 'input[placeholder="admin.settings.site.siteNamePlaceholder"]'
    await wrapper.get(input).setValue('Unrelated site draft')
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    await wrapper.get('[data-module=accounts]').setValue('read')
    await wrapper.get('[data-testid=review]').trigger('click')
    await wrapper.get('textarea').setValue('Allow visibility')
    const pending = deferred<AdminRolePermissionsResponse>()
    rolePut.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    await wrapper.get('[data-settings-tab=general]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
    expect(rolePut).toHaveBeenCalledTimes(1)
    expect(put).not.toHaveBeenCalled()
    const current = await roleGet()
    pending.resolve({ ...current, policy: { ...current.policy, version: 8, permissions: ['accounts.read'] } })
    await flushPromises()
    await wrapper.get('[data-settings-tab=general]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid=admin-role-permissions]').exists()).toBe(false)
    expect((wrapper.get(input).element as HTMLInputElement).value).toBe('Unrelated site draft')
    await wrapper.get('[data-testid=save-settings]').trigger('click'); await flushPromises()
    expect(JSON.parse(put.mock.calls[0][0].data)).toEqual({ revision: revisionA, site_name: 'Unrelated site draft' })
  })

  it('uses the real role confirmation on tab changes and route navigation', async () => {
    const { wrapper, router, rolePut } = await setup('super_admin', allPermissions, false, true)
    await wrapper.get('[data-settings-tab=roles]').trigger('click'); await flushPromises()
    await wrapper.get('[data-module=accounts]').setValue('read')
    await wrapper.get('[data-settings-tab=general]').trigger('click'); await flushPromises()
    wrapper.getComponent(ConfirmDialog).vm.$emit('cancel'); await flushPromises()
    expect(wrapper.get('[data-settings-tab=roles]').attributes('aria-current')).toBe('page')
    expect((wrapper.get('[data-module=accounts]').element as HTMLSelectElement).value).toBe('read')
    const cancelled = router.push('/admin/other'); await flushPromises()
    wrapper.getComponent(ConfirmDialog).vm.$emit('cancel'); await cancelled
    expect(router.currentRoute.value.path).toBe('/admin/settings')
    const accepted = router.push('/admin/other'); await flushPromises()
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm'); await accepted
    expect(router.currentRoute.value.path).toBe('/admin/other')
    expect(rolePut).not.toHaveBeenCalled()
  })
})

it('retires the template tab and saves the global BPS switch with feature settings', async () => {
  const h = await setup()
  expect(h.wrapper.findAll('button').some(button => button.text() === 'admin.settings.tabs.bps')).toBe(false)
  const features = h.wrapper.findAll('button').find(button => button.text() === 'admin.settings.tabs.features')
  if (!features) throw new Error('Features tab missing')
  await features.trigger('click')
  const gateway = h.wrapper.get('[data-testid="excel-bps-gateway-settings"]')
  await gateway.get('input[type="checkbox"]').setValue(false)
  await h.wrapper.get('[data-testid="save-settings"]').trigger('click'); await flushPromises()
  expect(h.put).toHaveBeenCalledTimes(1)
  expect(JSON.parse(h.put.mock.calls[0]?.[0].data)).toMatchObject({ excel_bps_enabled: false })
})
