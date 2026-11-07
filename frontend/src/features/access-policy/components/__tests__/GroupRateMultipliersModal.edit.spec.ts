import { peakRatePresentation } from '@/features/pricing'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'

const mocks = vi.hoisted(() => ({ getGroupRateMultipliers: vi.fn(), batchSetGroupRateMultipliers: vi.fn() }))
const groupsApiFixture = vi.hoisted(() => (mocks))
const accountsApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected accounts.list request in this test') }), getById: vi.fn(() => { throw new Error('Unexpected accounts.getById request in this test') }) }))
const usersApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected users.list request in this test') }) }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn(() => { throw new Error('Unexpected proxies.getAll request in this test') }) }))
const settingsApiFixture = vi.hoisted(() => ({ getSettings: vi.fn(() => { throw new Error('Unexpected settings.getSettings request in this test') }), updateSettings: vi.fn(() => { throw new Error('Unexpected settings.updateSettings request in this test') }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  mocks.getGroupRateMultipliers.mockResolvedValue([
    { user_id: 7, user_email: 'user@example.com', user_name: '', user_status: 'active', rate_multiplier: 2, rpm_override: null }
  ])
})

async function openEditor() {
  const wrapper = mount(GroupRateMultipliersModal, {
    props: { show: false, group: { id: 1, name: 'Group', platform: 'openai', rate_multiplier: 1 } as AdminGroup },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      Icon: true, PlatformIcon: true, Pagination: true
    } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('group rate multiplier edits', () => {
  it.each(['0', '-1'])('does not stage invalid custom rate %s', async (value) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(value)
    expect(wrapper.findAll('button').some(button => button.text() === 'common.save')).toBe(false)
    expect(mocks.batchSetGroupRateMultipliers).not.toHaveBeenCalled()
  })

  it.each([['0.5', 0.5], ['1e-3', 0.001]])('saves positive rate %s', async (value, expected) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(value)
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRateMultipliers).toHaveBeenCalledWith(1, [{ user_id: 7, rate_multiplier: expected }])
  })

  it('still removes an override when its input is cleared', async () => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue('')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRateMultipliers).toHaveBeenCalledWith(1, [])
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
