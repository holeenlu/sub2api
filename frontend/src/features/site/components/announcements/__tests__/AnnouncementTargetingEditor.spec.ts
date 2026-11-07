import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { installAnnouncementTestServices } from '@/features/site/__tests__/announcementServices'
import AnnouncementTargetingEditor from '../AnnouncementTargetingEditor.vue'
import Select from '@/shared/ui/Select.vue'
import type { AnnouncementTargeting } from '../../../model/announcement'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
installAnnouncementTestServices()
enableAutoUnmount(afterEach)
describe('announcement targeting field patches', () => {
  it('ignores unknown condition types and keeps a valid zero-valued balance condition', async () => {
    const targeting: AnnouncementTargeting = { any_of: [{ all_of: [{ type: 'subscription', operator: 'in', group_ids: [1] }] }] }
    const wrapper = mount(AnnouncementTargetingEditor, { props: { modelValue: targeting, groups: [] }, global: { stubs: { Select: true, Icon: true } } })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', null)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'future-type')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'balance')
    const emitted = wrapper.emitted('update:modelValue')![0][0] as AnnouncementTargeting
    expect(emitted.any_of![0].all_of![0]).toEqual({ type: 'balance', operator: 'gte', value: 0 })
    expect(targeting.any_of![0].all_of![0].group_ids).toEqual([1])
  })

  it('reads frozen targeting and emits a detached group selection without mutating props', async () => {
    const targeting: AnnouncementTargeting = { any_of: [{ all_of: [{ type: 'subscription', operator: 'in', group_ids: [1] }] }] }
    Object.freeze(targeting.any_of![0].all_of![0].group_ids)
    Object.freeze(targeting.any_of![0].all_of![0])
    Object.freeze(targeting.any_of![0].all_of)
    Object.freeze(targeting.any_of![0])
    Object.freeze(targeting.any_of)
    Object.freeze(targeting)
    const wrapper = mount(AnnouncementTargetingEditor, { props: { modelValue: targeting, groups: [] }, global: { stubs: { Select: true, Icon: true } } })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="groups"]').trigger('click')
    expect(targeting.any_of![0].all_of![0].group_ids).toEqual([1])
    const emitted = wrapper.emitted('update:modelValue')![0][0] as AnnouncementTargeting
    expect(emitted.any_of![0].all_of![0].group_ids).toEqual([2])
  })
})
