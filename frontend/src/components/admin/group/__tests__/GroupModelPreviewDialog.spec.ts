import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GroupModelPreviewDialog from '../GroupModelPreviewDialog.vue'
const { getModelPlazaPreview } = vi.hoisted(() => ({ getModelPlazaPreview: vi.fn() }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlazaPreview }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key, te: () => true }) }))

it('loads the selected admin group and explains excluded entries', async () => {
  getModelPlazaPreview.mockResolvedValue({ group: { id: 10, models: [] }, issues: [{ model: 'price-only', reason: 'pricing_only_or_not_allowed' }] })
  const wrapper = mount(GroupModelPreviewDialog, { props: { groupId: 10 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, PlazaGroupSection: true } } })
  await flushPromises()
  expect(getModelPlazaPreview).toHaveBeenCalledWith(10, expect.any(AbortSignal))
  expect(wrapper.text()).toContain('price-only')
  expect(wrapper.text()).toContain('modelPlaza.preview.reasons.pricing_only_or_not_allowed')
  expect(wrapper.findComponent({ name: 'PlazaGroupSection' }).props('group').id).toBe(10)
  wrapper.unmount()
})

describe('preview request lifecycle', () => {
  it('cancels the request on close', async () => {
    getModelPlazaPreview.mockImplementation(() => new Promise(() => {}))
    const wrapper = mount(GroupModelPreviewDialog, { props: { groupId: 11 }, global: { stubs: { BaseDialog: true, PlazaGroupSection: true } } })
    const signal = getModelPlazaPreview.mock.calls.at(-1)![1]
    await wrapper.setProps({ groupId: null })
    expect(signal.aborted).toBe(true)
    wrapper.unmount()
  })
})
