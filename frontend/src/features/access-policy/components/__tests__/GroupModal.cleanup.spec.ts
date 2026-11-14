import { peakRatePresentation } from '@/features/pricing'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GroupRPMOverridesModal from '../GroupRPMOverridesModal.vue'
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
const mocks = vi.hoisted(() => ({ list: vi.fn().mockResolvedValue({ items: [] }), getGroupRPMOverrides: vi.fn().mockResolvedValue([]), getGroupRateMultipliers: vi.fn().mockResolvedValue([]) }))
const usersApiFixture = vi.hoisted(() => (mocks))
const groupsApiFixture = vi.hoisted(() => (mocks))
const accountsApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected accounts.list request in this test') }), getById: vi.fn(() => { throw new Error('Unexpected accounts.getById request in this test') }) }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn(() => { throw new Error('Unexpected proxies.getAll request in this test') }) }))
const settingsApiFixture = vi.hoisted(() => ({ getSettings: vi.fn(() => { throw new Error('Unexpected settings.getSettings request in this test') }), updateSettings: vi.fn(() => { throw new Error('Unexpected settings.updateSettings request in this test') }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers(); mocks.list.mockResolvedValue({ items: [] }); mocks.getGroupRPMOverrides.mockResolvedValue([]); mocks.getGroupRateMultipliers.mockResolvedValue([]) })
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
describe.each([GroupRPMOverridesModal, GroupRateMultipliersModal])('group modal lifecycle', (component) => {
  function open() {
    return mount(component, { props: { show: true, group: { id: 1, name: 'Group', platform: 'openai' } as AdminGroup },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Icon: true, PlatformIcon: true, Pagination: true } } })
  }
  it('removes its document click listener on unmount', () => {
    const add = vi.spyOn(document, 'addEventListener'); const remove = vi.spyOn(document, 'removeEventListener')
    const w = open()
    const handlers = add.mock.calls.filter(([event]) => event === 'click').map(([, handler]) => handler)
    expect(handlers.length).toBeGreaterThan(0)
    w.unmount()
    for (const handler of handlers) expect(remove).toHaveBeenCalledWith('click', handler)
  })
  it('cancels a queued search when navigating away', async () => {
    const w = open(); await w.get('input[type="text"]').setValue('alice')
    w.unmount(); await vi.advanceTimersByTimeAsync(300); await flushPromises()
    expect(mocks.list).not.toHaveBeenCalled()
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
