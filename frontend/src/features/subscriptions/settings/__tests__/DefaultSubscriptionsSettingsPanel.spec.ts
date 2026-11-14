import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref, shallowReactive, type App } from 'vue'
import { CanceledError } from 'axios'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes'
import { adminGroupFixture as group } from '@/features/access-policy/api/__tests__/groupFixture'
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { AUTH_TOKEN_KEY, AUTH_USER_KEY } from '@/shared/session'
import Panel from '../DefaultSubscriptionsSettingsPanel.vue'
import { useSubscriptionsSettingsServices } from '../ports'
import { subscriptionScopeKey } from '../../ports'

vi.mock('../ports', () => ({ useSubscriptionsSettingsServices: vi.fn() }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => { localStorage.clear() })
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  localStorage.setItem(AUTH_USER_KEY, JSON.stringify({ id: 1, role: 'super_admin', policy_version: 1 }))
  localStorage.setItem(AUTH_TOKEN_KEY, 'session-a')
})
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function mountPanel(getAll = vi.fn<() => Promise<AdminGroup[]>>(), saved = true) {
  const groups = shallowReactive({ getAll })
  const scope = ref('admin:1:session-a')
  const modelValue = { default_subscriptions: saved ? [{ group_id: 7, validity_days: 30 }] : [] }
  vi.mocked(useSubscriptionsSettingsServices).mockReturnValue({ groups, platformIds: () => ['openai'], components: { GroupBadge, GroupOptionItem } })
  const wrapper = mount(Panel, {
    props: { modelValue },
    global: {
      plugins: [{ install(app: App) { app.provide(subscriptionScopeKey, scope) } }],
      stubs: { GroupBadge: true, GroupOptionItem: true, Toggle: true, Icon: true, Teleport: true }
    }
  })
  return { wrapper, groups, scope, modelValue }
}
type PanelWrapper = ReturnType<typeof mountPanel>['wrapper']
function button(wrapper: PanelWrapper, key: string) {
  const result = wrapper.findAll('button').find(item => item.text() === key)
  if (!result) throw new Error('Missing button: ' + key)
  return result
}
function options(wrapper: PanelWrapper) { return wrapper.getComponent({ name: 'Select' }).props('options') }
function savedDays(wrapper: PanelWrapper) { return wrapper.get('input[min="1"][max="36500"]') }

describe('DefaultSubscriptionsSettingsPanel group loading', () => {
  it.each([403, 500])('shows HTTP %s as an error without replacing saved values or rendering a successful empty state', async status => {
    const { wrapper } = mountPanel(vi.fn().mockRejectedValue({ status, message: 'query failed' }))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.users.failedToLoadGroups')
    expect(wrapper.text()).not.toContain('common.noGroupsAvailable')
    expect(savedDays(wrapper).element).toHaveProperty('value', '30')
    expect(wrapper.getComponent({ name: 'Select' }).props('disabled')).toBe(true)
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it('retains the successful options and current draft if a subsequent read fails', async () => {
    const getAll = vi.fn<() => Promise<AdminGroup[]>>().mockResolvedValueOnce([group()]).mockRejectedValueOnce({ status: 500 })
    const { wrapper } = mountPanel(getAll)
    await flushPromises()
    await savedDays(wrapper).setValue(45)
    const before = options(wrapper)
    const reload: unknown = Reflect.get(wrapper.vm, 'loadSubscriptionGroups')
    if (typeof reload !== 'function') throw new Error('Missing group query action')
    await reload()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.users.failedToLoadGroups')
    expect(options(wrapper)).toBe(before)
    expect(savedDays(wrapper).element).toHaveProperty('value', '45')
  })
  it('accepts a normal token rotation within the same logical session', async () => {
    const token = (expires: number) => 'header.' + btoa(JSON.stringify({ user_id: 1, role: 'super_admin', sid: 'same-session', token_version: 1, exp: expires })) + '.signature'
    localStorage.setItem(AUTH_TOKEN_KEY, token(100))
    const pending = deferred<AdminGroup[]>()
    const { wrapper } = mountPanel(vi.fn(() => pending.promise))
    localStorage.setItem(AUTH_TOKEN_KEY, token(200))
    pending.resolve([group()])
    await flushPromises()
    expect(options(wrapper)).toEqual([expect.objectContaining({ value: 7 })])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it('does not admit a pending result or start another read after logout', async () => {
    const pending = deferred<AdminGroup[]>()
    const getAll = vi.fn(() => pending.promise)
    const { wrapper, scope } = mountPanel(getAll)
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    scope.value = ''
    pending.resolve([group({ id: 99 })])
    await flushPromises()
    expect(options(wrapper)).toEqual([])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('common.loading')
    expect(getAll).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it('shows failure and retries without discarding the existing configuration or edited draft', async () => {
    const getAll = vi.fn<() => Promise<AdminGroup[]>>()
      .mockRejectedValueOnce(new Error('network unavailable')).mockResolvedValueOnce([group()])
    const { wrapper, modelValue } = mountPanel(getAll)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.users.failedToLoadGroups')
    expect(wrapper.text()).not.toContain('common.noGroupsAvailable')
    expect(savedDays(wrapper).element).toHaveProperty('value', '30')
    await savedDays(wrapper).setValue(45)
    const patches = wrapper.emitted('patch')?.length
    const baseline = JSON.stringify(modelValue)
    await button(wrapper, 'common.tryAgain').trigger('click')
    await flushPromises()
    expect(getAll).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(options(wrapper)).toEqual([expect.objectContaining({ value: 7, label: 'Existing group', rate: 0 })])
    expect(savedDays(wrapper).element).toHaveProperty('value', '45')
    expect(JSON.stringify(modelValue)).toBe(baseline)
    expect(wrapper.emitted('patch')?.length).toBe(patches)
  })
  it('distinguishes loading from a successful empty list without changing configuration', async () => {
    const pending = deferred<AdminGroup[]>()
    const { wrapper } = mountPanel(vi.fn(() => pending.promise), false)
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('common.loading')
    expect(wrapper.text()).not.toContain('common.noGroupsAvailable')
    pending.resolve([])
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('common.noGroupsAvailable')
    expect(button(wrapper, 'admin.settings.defaults.addDefaultSubscription').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it('keeps valid subscription options and filters inactive or standard groups', async () => {
    const { wrapper } = mountPanel(vi.fn().mockResolvedValue([
      group(), group({ id: 8, status: 'inactive' }), group({ id: 9, subscription_type: 'standard' })
    ]))
    await flushPromises()
    expect(options(wrapper)).toEqual([expect.objectContaining({ value: 7, rate: 0, description: null })])
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it('does not present cancellation as failure or successful empty data', async () => {
    const { wrapper } = mountPanel(vi.fn().mockRejectedValue(new CanceledError('cancelled')))
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('common.noGroupsAvailable')
    expect(wrapper.text()).not.toContain('common.loading')
    expect(button(wrapper, 'common.tryAgain')).toBeDefined()
  })
  it.each(['resolve', 'reject'] as const)('discards late %s after the reader changes, without clearing the new request loading state', async outcome => {
    const old = deferred<AdminGroup[]>(), next = deferred<AdminGroup[]>()
    const { wrapper, groups } = mountPanel(vi.fn(() => old.promise))
    groups.getAll = vi.fn(() => next.promise)
    await flushPromises()
    expect(groups.getAll).toHaveBeenCalledTimes(1)
    if (outcome === 'resolve') old.resolve([group({ id: 99, name: 'Old target' })])
    else old.reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('common.loading')
    expect(options(wrapper)).toEqual([])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    next.resolve([group({ id: 10, name: 'New target' })])
    await flushPromises()
    expect(options(wrapper)).toEqual([expect.objectContaining({ value: 10 })])
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it.each(['resolve', 'reject'] as const)('discards late %s after unmount', async outcome => {
    const pending = deferred<AdminGroup[]>()
    const { wrapper } = mountPanel(vi.fn(() => pending.promise))
    wrapper.unmount()
    const before = Reflect.get(wrapper.vm, 'subscriptionGroups')
    if (outcome === 'resolve') pending.resolve([group({ id: 99 })])
    else pending.reject(new Error('unmounted failure'))
    await flushPromises()
    expect(Reflect.get(wrapper.vm, 'subscriptionGroups')).toBe(before)
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
  it.each(['resolve', 'reject'] as const)('does not accept late %s from a previous same-user session', async outcome => {
    const pending = deferred<AdminGroup[]>()
    const getAll = vi.fn<() => Promise<AdminGroup[]>>().mockReturnValueOnce(pending.promise).mockResolvedValue([])
    const { wrapper, scope } = mountPanel(getAll)
    localStorage.setItem(AUTH_TOKEN_KEY, 'session-b')
    scope.value = 'admin:1:session-b'
    if (outcome === 'resolve') pending.resolve([group({ id: 99, name: 'Prior session' })])
    else pending.reject(new Error('prior session failure'))
    await flushPromises()
    expect(options(wrapper)).toEqual([])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('common.loading')
    expect(wrapper.emitted('patch')).toBeUndefined()
  })
})
