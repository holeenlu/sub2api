import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, h, reactive } from 'vue'
import RiskModelSelector from '../RiskModelSelector.vue'
import { installAccessPolicyServices } from '../accessPolicyServices'
import { accessPolicyModelSelectorKey, accessPolicyPermissionsKey, accessPolicyScopeKey, requireAccessPolicyService } from '@/features/access-policy/services'
import { managedModelsAPI } from '@/features/catalog/managedModels'
import { apiClient } from '@/shared/http'

enableAutoUnmount(afterEach)
const actor = reactive({ identityScopeKey: 'risk-session', isAuthenticated: true, permissions: ['risk.read', 'risk.manage'], isSuperAdmin: false, isSimpleMode: false, can: (permission: string) => actor.permissions.includes(permission), canEditAdminField: () => false, editableAdminFields: <T extends object>(value: T) => value })
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => actor }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => ({}) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const rows = [
  { id: 'gpt-a', display_name: 'A model', enabled: true },
  { id: 'claude-b', display_name: 'B model', enabled: true },
  { id: 'disabled', display_name: 'Disabled', enabled: false }
]
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
function setup(modelValue: string[] = [], props = {}) {
  return mount(RiskModelSelector, {
    props: { modelValue, ...props },
    global: { provide: {
      [accessPolicyScopeKey as symbol]: computed(() => actor.isAuthenticated ? actor.identityScopeKey : ''),
      [accessPolicyPermissionsKey as symbol]: actor
    } }
  })
}
beforeEach(() => {
  Object.assign(actor, { identityScopeKey: 'risk-session', isAuthenticated: true, permissions: ['risk.read', 'risk.manage'] })
  // The selector consumes the real API facade. Only HTTP is substituted.
  vi.spyOn(apiClient, 'get').mockResolvedValue({ data: rows })
  vi.spyOn(apiClient, 'put').mockRejectedValue(new Error('Risk selection must not write accounts'))
})
afterEach(() => vi.restoreAllMocks())

describe('risk model selector permissions and explicit selection', () => {
  it('uses the installed risk binding with disjoint risk-only permissions and the composite enabled catalog', async () => {
    const Host = defineComponent({ setup() { const Selector = requireAccessPolicyService(accessPolicyModelSelectorKey); return () => h(Selector, { modelValue: [] }) } })
    const wrapper = mount(Host, { global: { plugins: [{ install: installAccessPolicyServices }] } })
    await vi.dynamicImportSettled()
    await flushPromises()
    expect(wrapper.findComponent(RiskModelSelector).exists()).toBe(true)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/models/catalog', { params: { platform: 'composite' }, signal: expect.any(AbortSignal) })
    expect(wrapper.findAll('input[type="checkbox"]').map(input => input.element.value)).toEqual(['gpt-a', 'claude-b'])
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !input.element.checked)).toBe(true)
    await wrapper.get('input[value="gpt-a"]').setValue(true)
    expect(wrapper.getComponent(RiskModelSelector).emitted('update:modelValue')).toEqual([[['gpt-a']]])
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it('can remove retired references and the final selection stays explicitly empty', async () => {
    const wrapper = setup(['retired'])
    await flushPromises()
    expect(wrapper.get('input[value="retired"]').element.checked).toBe(true)
    await wrapper.get('input[value="retired"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    await wrapper.setProps({ modelValue: [] })
    expect(wrapper.findAll('input[type="checkbox"]').every(input => !input.element.checked)).toBe(true)
    expect(wrapper.find('input[value="retired"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it.each([{ permissions: ['risk.read'], props: {} }, { permissions: ['risk.read', 'risk.manage'], props: { readOnly: true } }, { permissions: ['risk.read', 'risk.manage'], props: { disabled: true } }])('loads but does not edit a read-only selection: %j', async ({ permissions, props }) => {
    actor.permissions = permissions
    const wrapper = setup(['gpt-a', 'retired'], props)
    await flushPromises()
    expect(apiClient.get).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('input[type="checkbox"]').every(input => input.element.disabled)).toBe(true)
    await wrapper.get('input[value="gpt-a"]').trigger('change')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it.each([['accounts.manage'], ['models.manage'], ['risk.manage']])('requires risk.read even with %s', async permission => {
    actor.permissions = [permission]
    const wrapper = setup(['retired'])
    await flushPromises()
    expect(apiClient.get).not.toHaveBeenCalled()
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('filters by display name without changing selected IDs', async () => {
    const wrapper = setup(['retired'])
    await flushPromises()
    await wrapper.get('input[type="text"]').setValue('b model')
    expect(wrapper.findAll('input[type="checkbox"]').map(input => input.element.value)).toEqual(['claude-b'])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('aborts on identity replacement, suppresses old errors/results and rechecks read permission', async () => {
    const old = deferred<{ data: typeof rows }>()
    vi.mocked(apiClient.get).mockReturnValueOnce(old.promise)
    const wrapper = setup()
    const signal = vi.mocked(apiClient.get).mock.calls[0][1]?.signal
    actor.identityScopeKey = 'risk-session-b'
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    old.resolve({ data: [{ ...rows[0], id: 'stale' }] })
    await flushPromises()
    expect(wrapper.find('input[value="stale"]').exists()).toBe(false)
    actor.permissions = ['accounts.manage']
    await flushPromises()
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('aborts an in-flight catalog on unmount and surfaces backend failures without defaulting to all', async () => {
    const pending = deferred<{ data: typeof rows }>()
    vi.mocked(apiClient.get).mockReturnValueOnce(pending.promise)
    const wrapper = setup()
    const signal = vi.mocked(apiClient.get).mock.calls[0][1]?.signal
    wrapper.unmount()
    expect(signal?.aborted).toBe(true)
    pending.resolve({ data: rows })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    vi.spyOn(managedModelsAPI, 'catalog').mockRejectedValueOnce({ response: { data: { message: 'original backend error' } } })
    const failed = setup()
    await flushPromises()
    expect(failed.get('[role="alert"]').text()).toBe('original backend error')
    expect(failed.emitted('update:modelValue')).toBeUndefined()
    expect(apiClient.put).not.toHaveBeenCalled()
  })
})
