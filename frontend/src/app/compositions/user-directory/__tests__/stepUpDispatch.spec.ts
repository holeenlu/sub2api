import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { AxiosError, isCancel, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { apiClient, configureHttpClient } from '@/shared/http'
import { installApplicationHttp } from '@/app/http/install'
import { globalAdminStepUp, type AdminUser } from '@/features/identity'
import GroupReplaceModal from '../GroupReplaceModal.vue'
import UserPlatformQuotaModal from '../UserPlatformQuotaModal.vue'
import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'
import UserApiKeysModal from '../UserApiKeysModal.vue'

// Only the actor, feedback, and visual shell are fixtures. All five write APIs,
// apiClient interceptors, Axios cancellation and application step-up recovery run normally.
const fixture = vi.hoisted(() => ({
  actor: { identityScopeKey: 'session-a', isAdmin: true },
  showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => fixture.actor }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => fixture }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const group = { id: 7, name: 'fixture-group', platform: 'openai', is_exclusive: true, status: 'active', subscription_type: 'standard', rate_multiplier: 1 }
const quota = { platform_quotas: [{ platform: 'openai', daily_limit_usd: 10, weekly_limit_usd: null, monthly_limit_usd: null }] }
const key = { id: 5, name: 'fixture-key', key: 'synthetic-only-key', group_id: null, status: 'active', created_at: '2026-10-01' }
const user = (id: number): AdminUser => ({ id, email: 'fixture@example.test', allowed_groups: [7], group_rates: { 7: 0.5 } } as AdminUser)
const token = 'header.' + btoa(JSON.stringify({ user_id: 800, role: 'super_admin', sid: 'unchanged-actor-session' })) + '.synthetic-signature'
const cases = [
  { operation: 'replace', method: 'post', url: '/admin/users/1/replace-group', payload: { old_group_id: 1, new_group_id: 7 } },
  { operation: 'quotaSave', method: 'put', url: '/admin/users/1/platform-quotas', payload: { quotas: expect.arrayContaining([{ platform: 'openai', daily_limit_usd: 10, weekly_limit_usd: null, monthly_limit_usd: null }]) } },
  { operation: 'quotaReset', method: 'post', url: '/admin/users/1/platform-quotas/reset', payload: { platform: 'openai', window: 'daily' } },
  { operation: 'allowed', method: 'put', url: '/admin/users/1', payload: { allowed_groups: [7], restrict_public_groups: false, group_rates: { 7: 0.5 } } },
  { operation: 'keyGroup', method: 'put', url: '/admin/api-keys/5', payload: { group_id: 7 } }
] as const
type Operation = typeof cases[number]['operation']
let previousAdapter: typeof apiClient.defaults.adapter
let responseInterceptor: number
let writes: InternalAxiosRequestConfig[]
let cancellations: unknown[]

beforeEach(() => {
  vi.clearAllMocks()
  fixture.actor = reactive({ identityScopeKey: 'session-a', isAdmin: true })
  localStorage.setItem('auth_token', token)
  localStorage.setItem('auth_user', JSON.stringify({ id: 800, role: 'super_admin' }))
  globalAdminStepUp.onCancel()
  writes = []
  cancellations = []
  previousAdapter = apiClient.defaults.adapter
  const adapter: AxiosAdapter = async config => {
    const response = (data: unknown) => ({ config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data } })
    if (config.method === 'get') {
      if (config.url?.endsWith('/platform-quotas')) return response(quota)
      if (config.url?.endsWith('/api-keys')) return response({ items: [key] })
      if (config.url === '/admin/groups') return response({ items: [group] })
      if (config.url === '/admin/groups/all') return response([group])
      throw new Error('Unexpected synthetic read: ' + config.url)
    }
    writes.push(config)
    if (writes.length === 1) {
      throw new AxiosError('Verification required', 'ERR_BAD_REQUEST', config, undefined, {
        config, status: 403, statusText: 'Forbidden', headers: {}, data: { code: 'STEP_UP_REQUIRED' }
      })
    }
    return response({ ...quota, ...user(1), migrated_keys: 2, api_key: { ...key, group_id: 7 }, auto_granted_group_access: false })
  }
  apiClient.defaults.adapter = adapter
  installApplicationHttp()
  responseInterceptor = apiClient.interceptors.response.use(undefined, error => {
    if (isCancel(error)) cancellations.push(error)
    return Promise.reject(error)
  })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})
enableAutoUnmount(afterEach)
afterEach(async () => {
  globalAdminStepUp.onCancel()
  await flushPromises()
  apiClient.interceptors.response.eject(responseInterceptor)
  apiClient.defaults.adapter = previousAdapter
  configureHttpClient({})
  localStorage.clear()
  vi.restoreAllMocks()
})

async function open(operation: Operation) {
  const component = operation === 'replace' ? GroupReplaceModal : operation.startsWith('quota') ? UserPlatformQuotaModal : operation === 'allowed' ? UserAllowedGroupsModal : UserApiKeysModal
  const wrapper = mount(component, {
    props: { show: true, user: user(1), ...(operation === 'replace' ? { oldGroup: { id: 1, name: 'Old' }, allGroups: [group] } : {}) },
    global: { stubs: {
      BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /><button data-testid="close" @click="$emit(\'close\')">close</button></div>' },
      Icon: true, PlatformIcon: true, GroupBadge: true,
      GroupOptionItem: { props: ['name'], template: '<span>{{ name }}</span>' }, Teleport: true
    } }
  })
  await flushPromises()
  return wrapper
}
async function submit(wrapper: VueWrapper, operation: Operation): Promise<void> {
  if (operation === 'replace') await wrapper.get('input[type="radio"]').setValue()
  if (operation === 'keyGroup') {
    await wrapper.get('button').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'fixture-group')!.trigger('click')
  } else if (operation === 'quotaReset') {
    await wrapper.findAll('button').find(button => button.text() === '↻' && button.attributes('disabled') === undefined)!.trigger('click')
  } else await wrapper.get('button.btn-primary').trigger('click')
  await flushPromises()
  expect(globalAdminStepUp.visible.value).toBe(true)
}

describe.each(cases)('$operation real HTTP step-up ownership', ({ operation, method, url, payload }) => {
  it.each(['target', 'close', 'unmount'])('does not dispatch again after %s with the same actor', async transition => {
    const wrapper = await open(operation)
    await submit(wrapper, operation)
    expect(writes).toHaveLength(1)
    const original = writes[0]
    expect(original.method).toBe(method)
    expect(original.url).toBe(url)
    expect(JSON.parse(original.data)).toEqual(payload)
    expect(original.signal).toBeInstanceOf(AbortSignal)
    expect(original.signal?.aborted).toBe(false)
    if (transition === 'target') await wrapper.setProps({ user: user(2) })
    else if (transition === 'close') await wrapper.get('[data-testid="close"]').trigger('click')
    else wrapper.unmount()
    expect(original.signal?.aborted).toBe(true)
    expect(localStorage.getItem('auth_token')).toBe(token)
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(globalAdminStepUp.visible.value).toBe(false)
    expect(cancellations.length).toBeGreaterThan(0)
    expect(writes).toHaveLength(1)
    expect(fixture.showSuccess).not.toHaveBeenCalled()
    expect(fixture.showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeUndefined()
  })

  it('preserves the single verified retry while this actor and target remain current', async () => {
    const wrapper = await open(operation)
    await submit(wrapper, operation)
    expect(writes).toHaveLength(1)
    const signal = writes[0].signal
    globalAdminStepUp.onVerified()
    await flushPromises()
    expect(writes).toHaveLength(2)
    expect(writes[1].signal).toBe(signal)
    expect(signal?.aborted).toBe(false)
    expect(writes.map(config => [config.method, config.url, JSON.parse(config.data)])).toEqual([
      [method, url, payload], [method, url, payload]
    ])
    expect(cancellations).toEqual([])
    expect(fixture.showSuccess).toHaveBeenCalledOnce()
    expect(fixture.showError).not.toHaveBeenCalled()
  })
})
