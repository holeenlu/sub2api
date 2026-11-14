import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupEditorDialog from '../GroupEditorDialog.vue'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey } from '@/features/access-policy/services'
const { create, update, getAll, showSuccess, showError } = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), getAll: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/features/access-policy/api/groups', () => ({ groupsAPI: { create, update, getAll } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => { vi.clearAllMocks(); getAll.mockResolvedValue([]); create.mockResolvedValue({ id: 1 }); update.mockResolvedValue({ id: 7 }) })
function render(group: AdminGroup | null = null) {
 const allowed = ref(true)
 const ratesWritable = ref(false)
 const scope = ref('epoch1:restricted-admin')
 const filter = vi.fn((_schema: string, payload: { name: string }) => ({ name: payload.name, platform: 'anthropic' }))
 const wrapper = shallowMount(GroupEditorDialog, { props: { group }, global: { provide: { [accessPolicyScopeKey as symbol]: scope, [accessPolicyNotificationsKey as symbol]: { showSuccess, showError }, [accessPolicyPermissionsKey as symbol]: { isSuperAdmin: false, isSimpleMode: true, can: () => allowed.value, canEditAdminField: (_schema: string, field: string) => field !== 'rate_multiplier' || ratesWritable.value, editableAdminFields: filter }, [accessPolicyAccountsKey as symbol]: { getById: vi.fn(), search: vi.fn() } }, stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } } })
 return { wrapper, scope, filter, allowed, ratesWritable }
}
describe('the Group composition owns one save command', () => {
 it('applies the injected field whitelist and dispatches one create while pending', async () => {
  let resolve!: (value: object) => void; create.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper, filter } = render(); await flushPromises()
  wrapper.getComponent({ name: 'GroupIdentityPanel' }).vm.$emit('update:modelValue', { name: 'Restricted', description: 'private', i18n: {}, platform: 'anthropic' }); await flushPromises()
  await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
  expect(filter).toHaveBeenCalledWith('group.create', { name: 'Restricted', description: 'private', i18n: {}, platform: 'anthropic' })
  expect(create).toHaveBeenCalledTimes(1); expect(create).toHaveBeenCalledWith({ name: 'Restricted', platform: 'anthropic' })
  resolve({ id: 1 }); await flushPromises(); expect(wrapper.emitted('saved')).toHaveLength(1); wrapper.unmount()
 })
 it('does not publish a previous scope save completion', async () => {
  let resolve!: (value: object) => void; create.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper, scope } = render(); await flushPromises()
  wrapper.getComponent({ name: 'GroupIdentityPanel' }).vm.$emit('update:modelValue', { name: 'Old', description: '', i18n: {}, platform: 'anthropic' }); await flushPromises()
  await wrapper.get('form').trigger('submit'); scope.value = 'epoch2:supplier:7'; resolve({ id: 9 }); await flushPromises()
  expect(wrapper.emitted('saved')).toBeUndefined(); expect(showSuccess).not.toHaveBeenCalled(); expect(showError).not.toHaveBeenCalled(); wrapper.unmount()
 })
})

const storedGroup = (id: number) => ({ id, name: 'Original', description: null, i18n: {}, platform: 'anthropic', rate_multiplier: 1, is_exclusive: false, status: 'active', subscription_type: 'standard', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, fallback_group_id: null, fallback_group_id_on_invalid_request: null, model_routing: null, rpm_limit: 0 } as AdminGroup)
async function setName(wrapper: ReturnType<typeof render>['wrapper'], name: string) {
 wrapper.getComponent({ name: 'GroupIdentityPanel' }).vm.$emit('update:modelValue', { name, description: '', i18n: {}, platform: 'anthropic' })
 await flushPromises()
}
describe('Group editor invalidation and failure ownership', () => {
 it('permanently invalidates the old draft after a scope switch, even before parent unmount', async () => {
  const { wrapper, scope } = render(); await flushPromises(); await setName(wrapper, 'Old scope')
  scope.value = 'epoch2:restricted-admin'; await wrapper.get('form').trigger('submit')
  expect(create).not.toHaveBeenCalled(); expect(update).not.toHaveBeenCalled(); expect(wrapper.emitted('close')).toBeTruthy(); wrapper.unmount()
 })
 it('does not send an old draft to a replacement group id', async () => {
  const { wrapper } = render(storedGroup(7)); await flushPromises(); await setName(wrapper, 'Unsaved old group')
  await wrapper.setProps({ group: storedGroup(8) }); await wrapper.get('form').trigger('submit')
  expect(update).not.toHaveBeenCalled(); expect(wrapper.emitted('close')).toBeTruthy(); wrapper.unmount()
 })
 it('drops completion after permission revocation even if permission is later restored', async () => {
  let resolve!: (value: object) => void; create.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper, allowed } = render(); await flushPromises(); await setName(wrapper, 'Revoked')
  await wrapper.get('form').trigger('submit'); allowed.value = false; allowed.value = true
  resolve({ id: 9 }); await flushPromises()
  expect(wrapper.emitted('saved')).toBeUndefined(); expect(showSuccess).not.toHaveBeenCalled(); expect(showError).not.toHaveBeenCalled()
  await wrapper.get('form').trigger('submit'); expect(create).toHaveBeenCalledTimes(1); wrapper.unmount()
 })
 it('invalidates in-flight work when field access changes while groups.manage remains granted', async () => {
  let resolve!: (value: object) => void; create.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper, ratesWritable } = render(); await flushPromises(); await setName(wrapper, 'Old field grant')
  await wrapper.get('form').trigger('submit'); ratesWritable.value = true
  resolve({ id: 9 }); await flushPromises()
  expect(wrapper.emitted('saved')).toBeUndefined(); expect(showSuccess).not.toHaveBeenCalled()
  await wrapper.get('form').trigger('submit'); expect(create).toHaveBeenCalledTimes(1); wrapper.unmount()
 })
 it('keeps an edited draft after a failed PUT and never retries or mutates the source group', async () => {
  const group = storedGroup(7); update.mockRejectedValueOnce({ response: { status: 409, data: { message: 'Conflict' } } })
  const { wrapper, filter } = render(group); await flushPromises(); await setName(wrapper, 'Keep my draft')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(update).toHaveBeenCalledTimes(1); expect(filter).toHaveBeenCalledWith('group.update', expect.objectContaining({ name: 'Keep my draft' }))
  expect(wrapper.getComponent({ name: 'GroupIdentityPanel' }).props('modelValue').name).toBe('Keep my draft')
  expect(group.name).toBe('Original'); expect(wrapper.emitted('saved')).toBeUndefined(); expect(wrapper.emitted('close')).toBeUndefined(); expect(showError).toHaveBeenCalledTimes(1); wrapper.unmount()
 })
})
