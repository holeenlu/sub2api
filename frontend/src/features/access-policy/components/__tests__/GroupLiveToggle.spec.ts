import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import GroupLiveToggle from '../GroupLiveToggle.vue'
import { accessPolicyScopeKey } from '../../services'
const getLiveCapability = vi.hoisted(() => vi.fn())
vi.mock('../../api/groups', () => ({ groupsAPI: { getLiveCapability } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
function render() { return mount(GroupLiveToggle, { props: { modelValue: false }, global: { provide: { [accessPolicyScopeKey as symbol]: ref('admin:1') }, stubs: { Toggle: { name: 'Toggle', emits: ['update:modelValue'], template: '<button @click="$emit(\'update:modelValue\', true)" />' }, ConfirmDialog: { name: 'ConfirmDialog', props: ['show'], template: '<div />' } } } }) }
describe('Live capability evidence', () => {
 it('does not label an unavailable query as unsupported or enable Live', async () => { getLiveCapability.mockRejectedValueOnce(new Error('offline')); const wrapper = render(); await wrapper.get('button').trigger('click'); await flushPromises(); expect(wrapper.getComponent({ name: 'ConfirmDialog' }).props('show')).toBe(false); expect(wrapper.emitted('update:modelValue')).toBeUndefined(); expect(wrapper.get('[role="alert"]').text()).toBe('admin.groups.failedToLoad'); wrapper.unmount() })
 it('asks for confirmation only for an explicit unsupported response', async () => { getLiveCapability.mockResolvedValueOnce({ supported: false }); const wrapper = render(); await wrapper.get('button').trigger('click'); await flushPromises(); expect(wrapper.getComponent({ name: 'ConfirmDialog' }).props('show')).toBe(true); expect(wrapper.emitted('update:modelValue')).toBeUndefined(); wrapper.unmount() })
 it('keeps a missing support fact unknown', async () => { getLiveCapability.mockResolvedValueOnce({}); const wrapper = render(); await wrapper.get('button').trigger('click'); await flushPromises(); expect(wrapper.getComponent({ name: 'ConfirmDialog' }).props('show')).toBe(false); expect(wrapper.emitted('update:modelValue')).toBeUndefined(); wrapper.unmount() })
})
