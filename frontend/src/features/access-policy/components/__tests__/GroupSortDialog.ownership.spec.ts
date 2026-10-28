import { peakRatePresentation } from '@/features/pricing'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { groupPresentationKey, type GroupPresentation } from '../../presentation'
import { platformLabel, CONCRETE_PLATFORM_OPTIONS } from '@/features/catalog'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupSortDialog from '../GroupSortDialog.vue'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey } from '../../services'
const { getAll, update, showSuccess, showError } = vi.hoisted(() => ({ getAll: vi.fn(), update: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('../../api/groups', () => ({ groupsAPI: { getAll, updateSortOrder: update } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const row = (id: number) => ({ id, name: 'Group ' + id, platform: 'openai', sort_order: id * 10 } as AdminGroup)
beforeEach(() => { vi.clearAllMocks(); getAll.mockResolvedValue([row(1), row(2)]); update.mockResolvedValue(undefined) })
function render() {
 const presentation: GroupPresentation = { peakRate: peakRatePresentation, concretePlatformOptions: ref(CONCRETE_PLATFORM_OPTIONS), platformLabel, serverUtcOffset: ref(undefined), isSimpleMode: ref(false) }
 const scope = ref('epoch1:admin')
 const wrapper = shallowMount(GroupSortDialog, { global: { provide: { [groupPresentationKey as symbol]: presentation, [accessPolicyScopeKey as symbol]: scope, [accessPolicyNotificationsKey as symbol]: { showSuccess, showError }, [accessPolicyPermissionsKey as symbol]: { can: () => true } }, stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } } })
 return { wrapper, scope }
}
describe('group sort snapshot ownership', () => {
 it('does not send an empty reorder while the required list is still pending', async () => {
  let resolve!: (rows: AdminGroup[]) => void; getAll.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper } = render(); const button = wrapper.findAll('button').find(item => item.text() === 'common.save')!
  expect(button.attributes('disabled')).toBeDefined(); await button.trigger('click'); expect(update).not.toHaveBeenCalled()
  resolve([row(1), row(2)]); await flushPromises(); expect(button.attributes('disabled')).toBeUndefined()
  await button.trigger('click'); await flushPromises(); expect(update).toHaveBeenCalledWith([{ id: 1, sort_order: 0 }, { id: 2, sort_order: 10 }]); wrapper.unmount()
 })
 it('does not publish a previous scope reorder completion', async () => {
  let resolve!: () => void; update.mockReturnValueOnce(new Promise<void>(yes => { resolve = yes }))
  const { wrapper, scope } = render(); await flushPromises(); await wrapper.findAll('button').find(item => item.text() === 'common.save')!.trigger('click')
  scope.value = 'epoch2:admin'; resolve(); await flushPromises()
  expect(showSuccess).not.toHaveBeenCalled(); expect(showError).not.toHaveBeenCalled(); expect(wrapper.emitted('saved')).toBeUndefined(); wrapper.unmount()
 })
})
