import { peakRatePresentation } from '@/features/pricing'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { groupPresentationKey, type GroupPresentation } from '../../presentation'
import { platformLabel, CONCRETE_PLATFORM_OPTIONS } from '@/features/catalog'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import CompositeRoutesDialog from '../CompositeRoutesDialog.vue'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey } from '../../services'
const { list, create, showSuccess, showError } = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('../../api/groups', () => ({ groupsAPI: { listCompositeRoutes: list, createCompositeRoute: create } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => { vi.clearAllMocks(); list.mockResolvedValue([]); create.mockResolvedValue({ id: 1 }) })
function render() {
 const presentation: GroupPresentation = { peakRate: peakRatePresentation, concretePlatformOptions: ref(CONCRETE_PLATFORM_OPTIONS), platformLabel, serverUtcOffset: ref(undefined), isSimpleMode: ref(false) }
 const scope = ref('epoch1:admin'); const allowed = ref(true)
 const wrapper = shallowMount(CompositeRoutesDialog, { props: { group: { id: 7, name: 'Composite', platform: 'composite' } as AdminGroup }, global: { provide: { [groupPresentationKey as symbol]: presentation, [accessPolicyScopeKey as symbol]: scope, [accessPolicyNotificationsKey as symbol]: { showSuccess, showError }, [accessPolicyPermissionsKey as symbol]: { can: () => allowed.value } }, stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
 return { wrapper, scope, allowed }
}
describe('composite route mutation ownership', () => {
 it('preserves priority zero, dispatches once, and releases saving after its own refresh', async () => {
  let refreshed!: (value: never[]) => void
  list.mockResolvedValueOnce([]).mockReturnValueOnce(new Promise(resolve => { refreshed = resolve }))
  const { wrapper } = render(); await flushPromises()
  await wrapper.get('form input[type="text"]').setValue('public-model')
  await wrapper.get('form input[type="number"]').setValue('0')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(create).toHaveBeenCalledTimes(1); expect(create).toHaveBeenCalledWith(7, expect.objectContaining({ public_model: 'public-model', priority: 0 }))
  expect(wrapper.get('form button[type="submit"]').attributes('disabled')).toBeDefined()
  await wrapper.get('form').trigger('submit'); expect(create).toHaveBeenCalledTimes(1)
  refreshed([]); await flushPromises(); expect(wrapper.get('form button[type="submit"]').attributes('disabled')).toBeUndefined(); wrapper.unmount()
 })
 it('retains the draft on conflict and never retries a command', async () => {
  create.mockRejectedValueOnce({ response: { status: 409, data: { message: 'Conflict' } } })
  const { wrapper } = render(); await flushPromises(); await wrapper.get('form input[type="text"]').setValue('keep-this')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(create).toHaveBeenCalledTimes(1); expect((wrapper.get('form input[type="text"]').element as HTMLInputElement).value).toBe('keep-this')
  expect(wrapper.get('form button[type="submit"]').attributes('disabled')).toBeUndefined(); expect(showError).toHaveBeenCalledTimes(1); expect(showSuccess).not.toHaveBeenCalled(); wrapper.unmount()
 })
 it('drops save completion after scope loss and prevents stale form resubmission', async () => {
  let resolve!: (value: object) => void; create.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
  const { wrapper, scope } = render(); await flushPromises(); await wrapper.get('form input[type="text"]').setValue('old')
  await wrapper.get('form').trigger('submit'); scope.value = 'epoch2:admin'; resolve({ id: 4 }); await flushPromises()
  expect(showSuccess).not.toHaveBeenCalled(); expect(showError).not.toHaveBeenCalled(); expect(list).toHaveBeenCalledTimes(1)
  await wrapper.get('form').trigger('submit'); expect(create).toHaveBeenCalledTimes(1); wrapper.unmount()
 })
})
