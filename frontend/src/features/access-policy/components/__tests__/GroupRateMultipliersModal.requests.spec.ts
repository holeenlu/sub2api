import { peakRatePresentation } from '@/features/pricing'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'

const mocks = vi.hoisted(() => ({ getGroupRateMultipliers: vi.fn(), batchSetGroupRateMultipliers: vi.fn(), showError: vi.fn() }))
const groupsApiFixture = vi.hoisted(() => (mocks))
const accountsApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected accounts.list request in this test') }), getById: vi.fn(() => { throw new Error('Unexpected accounts.getById request in this test') }) }))
const usersApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected users.list request in this test') }) }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn(() => { throw new Error('Unexpected proxies.getAll request in this test') }) }))
const settingsApiFixture = vi.hoisted(() => ({ getSettings: vi.fn(() => { throw new Error('Unexpected settings.getSettings request in this test') }), updateSettings: vi.fn(() => { throw new Error('Unexpected settings.updateSettings request in this test') }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => vi.resetAllMocks())
afterEach(() => vi.restoreAllMocks())

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const group = (id: number) => ({ id, name: `Group ${id}`, platform: 'openai', rate_multiplier: 1 } as AdminGroup)
const entries = (id: number) => [{ user_id: id, user_name: '', user_email: `user${id}@example.com`, user_status: 'active', rate_multiplier: 2, rpm_override: null }]

async function openGroup() {
  const wrapper = mount(GroupRateMultipliersModal, {
    props: { show: false, group: group(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      Icon: true, PlatformIcon: true, Pagination: true
    } }
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe('group rate multiplier request ownership', () => {
  it('keeps the reopened group data when an older response finishes last', async () => {
    const old = deferred()
    mocks.getGroupRateMultipliers.mockReturnValueOnce(old.promise).mockResolvedValueOnce(entries(2))
    const wrapper = await openGroup()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    await flushPromises()
    old.resolve(entries(1))
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
    expect(wrapper.get('tbody').text()).not.toContain('user1@example.com')
    await wrapper.get('tbody input[type="number"]').setValue('3')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRateMultipliers).toHaveBeenCalledWith(2, [{ user_id: 2, rate_multiplier: 3 }])
  })

  it('does not end the new load or report an error when an old request fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const old = deferred()
    const current = deferred()
    mocks.getGroupRateMultipliers.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openGroup()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    old.reject(new Error('old request failed'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(true)
    current.resolve(entries(2))
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
  })

  it('loads a different group while the dialog remains open', async () => {
    mocks.getGroupRateMultipliers.mockResolvedValueOnce(entries(1)).mockResolvedValueOnce(entries(2))
    const wrapper = await openGroup()
    await flushPromises()
    await wrapper.setProps({ group: group(2) })
    await flushPromises()
    expect(mocks.getGroupRateMultipliers).toHaveBeenLastCalledWith(2)
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
  })

  it('discards the previous group draft before loading the next group', async () => {
    mocks.getGroupRateMultipliers.mockResolvedValueOnce(entries(1)).mockReturnValueOnce(deferred().promise)
    const wrapper = await openGroup()
    await flushPromises()
    await wrapper.get('tbody input[type="number"]').setValue('3')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    expect(wrapper.findAll('button').some(button => button.text() === 'common.save')).toBe(false)
  })

  it('ignores a request failure after unmounting', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const request = deferred()
    mocks.getGroupRateMultipliers.mockReturnValue(request.promise)
    const wrapper = await openGroup()
    wrapper.unmount()
    request.reject(new Error('offline'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
  })
})


import { config as testConfig } from '@vue/test-utils'
import { ref as testRef, computed as testComputed } from 'vue'
import { groupPresentationKey } from '@/features/access-policy/presentation/context'
import { platformLabel } from '@/features/catalog'

import { useAppStore as mockedApp } from '@/app/model/app'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, accessPolicyUsersKey, riskControlProxiesKey, riskControlSettingsKey, accessPolicyModelSelectorKey } from '@/features/access-policy/services'
vi.mock('@/features/access-policy/api/groups', async () => ({ groupsAPI: groupsApiFixture }))

const permissionFallback = { isSuperAdmin: true, isSimpleMode: false, can: () => true, canEditAdminField: () => true, editableAdminFields: (_schema: string, payload: object) => payload }
const mockedPermissions = new Proxy(permissionFallback, { get(target, key) { return readPermissions()[key as keyof typeof target] ?? target[key as keyof typeof target] } })
Object.assign(testConfig.global.provide, {
 [groupPresentationKey as symbol]: { peakRate: peakRatePresentation, platformLabel, serverUtcOffset: testRef(undefined), isSimpleMode: testComputed(() => mockedPermissions.isSimpleMode) },
 [accessPolicyScopeKey as symbol]: testRef('session:admin:test'),
 [accessPolicyNotificationsKey as symbol]: new Proxy({}, { get(_target, key) { return mockedApp()[key as 'showError' | 'showSuccess'] } }),
 [accessPolicyPermissionsKey as symbol]: mockedPermissions,
 [accessPolicyAccountsKey as symbol]: { search: async (keyword: string, params: object, signal: AbortSignal) => (await accountsApiFixture.list(1, 20, { search: keyword, ...params }, { signal })).items, getById: (id: number) => accountsApiFixture.getById(id) },
 [accessPolicyUsersKey as symbol]: { search: async (keyword: string) => (await usersApiFixture.list(1, 10, { search: keyword })).items },
 [riskControlProxiesKey as symbol]: () => proxiesApiFixture.getAll(),
 [riskControlSettingsKey as symbol]: { enable: async () => { const current = await settingsApiFixture.getSettings(); await settingsApiFixture.updateSettings({ risk_control_enabled: true, revision: current.revision || '' }) } },
 [accessPolicyModelSelectorKey as symbol]: { name: 'ModelWhitelistSelector', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }
})
function readPermissions() { return permissionFallback }
