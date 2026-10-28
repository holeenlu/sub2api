import { peakRatePresentation } from '@/features/pricing'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { groupPresentationKey, type GroupPresentation } from '../../presentation'
import { platformLabel, CONCRETE_PLATFORM_OPTIONS } from '@/features/catalog'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyUsersKey } from '../../services'
const { getEntries, save, showSuccess, showError } = vi.hoisted(() => ({ getEntries: vi.fn(), save: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('../../api/groups', () => ({ groupsAPI: { getGroupRateMultipliers: getEntries, batchSetGroupRateMultipliers: save } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => { vi.clearAllMocks(); getEntries.mockResolvedValue([{ user_id: 8, user_name: 'User', user_email: 'user@example.com', user_status: 'active', rate_multiplier: 2, rpm_override: null }]); save.mockResolvedValue(undefined) })
function render() {
 const presentation: GroupPresentation = { peakRate: peakRatePresentation, concretePlatformOptions: ref(CONCRETE_PLATFORM_OPTIONS), platformLabel, serverUtcOffset: ref(undefined), isSimpleMode: ref(false) }
 const allowed = ref(true)
 const wrapper = mount(GroupRateMultipliersModal, { props: { show: true, group: { id: 7, name: 'Group', platform: 'openai', rate_multiplier: 1 } as AdminGroup }, global: { provide: { [groupPresentationKey as symbol]: presentation, [accessPolicyScopeKey as symbol]: ref('billing-admin'), [accessPolicyNotificationsKey as symbol]: { showSuccess, showError }, [accessPolicyPermissionsKey as symbol]: { can: (permission: string) => permission === 'billing.rates.update' && allowed.value }, [accessPolicyUsersKey as symbol]: { search: vi.fn() } }, stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true, PlatformIcon: true, Pagination: true } } })
 return { wrapper, allowed }
}
async function saveRate(wrapper: ReturnType<typeof render>['wrapper']) {
 await wrapper.get('tbody input[type="number"]').setValue('3')
 await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
}
describe('billing-only access to group rate overrides', () => {
 it('loads initially visible dialogs and permits the billing capability without groups.manage', async () => {
  const { wrapper } = render(); await flushPromises(); await saveRate(wrapper); await flushPromises()
  expect(save).toHaveBeenCalledWith(7, [{ user_id: 8, rate_multiplier: 3 }]); expect(wrapper.emitted('success')).toHaveLength(1); wrapper.unmount()
 })
 it('does not accept a completion after the billing permission is revoked', async () => {
  let resolve!: () => void; save.mockReturnValueOnce(new Promise<void>(yes => { resolve = yes }))
  const { wrapper, allowed } = render(); await flushPromises(); await saveRate(wrapper)
  allowed.value = false; allowed.value = true; resolve(); await flushPromises()
  expect(wrapper.emitted('success')).toBeUndefined(); expect(showSuccess).not.toHaveBeenCalled(); expect(showError).not.toHaveBeenCalled(); wrapper.unmount()
 })
})
