import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import BPSDefaultsPanel from '../BPSDefaultsPanel.vue'
import { defaultExcelBPSDefaults } from '@/utils/excelBPSDefaults'
import { getExcelBPSDefaults, saveExcelBPSDefaults } from '@/api/admin/excelBPSDefaults'

enableAutoUnmount(afterEach)
vi.mock('@/api/admin/excelBPSDefaults', () => ({ getExcelBPSDefaults: vi.fn(), saveExcelBPSDefaults: vi.fn() }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(getExcelBPSDefaults).mockResolvedValue(defaultExcelBPSDefaults())
  vi.mocked(saveExcelBPSDefaults).mockImplementation(async value => value)
})
const open = () => mount(BPSDefaultsPanel, { props: { groups: [] }, global: { stubs: {
  BPSDefaultsCard: { name: 'BPSDefaultsCard', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }
} } })

describe('BPS template settings', () => {
  it('only reads on open and saves a template on explicit action', async () => {
    const wrapper = open(); await flushPromises()
    expect(saveExcelBPSDefaults).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click'); await flushPromises()
    expect(saveExcelBPSDefaults).toHaveBeenCalledTimes(1)
    expect(saveExcelBPSDefaults).toHaveBeenCalledWith(defaultExcelBPSDefaults())
    expect(wrapper.text()).toContain('autoBPSOps.defaults.saved')
  })
  it('blocks saving after a read failure and allows a reload', async () => {
    vi.mocked(getExcelBPSDefaults).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = open(); await flushPromises()
    expect(wrapper.get('fieldset').attributes()).toHaveProperty('disabled')
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click')
    expect(saveExcelBPSDefaults).not.toHaveBeenCalled()
    await wrapper.findAll('button')[1]!.trigger('click'); await flushPromises()
    expect(wrapper.get('fieldset').attributes()).not.toHaveProperty('disabled')
  })
  it('does not save invalid model selections', async () => {
    const wrapper = open(); await flushPromises()
    wrapper.findComponent({ name: 'BPSDefaultsCard' }).vm.$emit('update:modelValue', { ...defaultExcelBPSDefaults(), models: [] })
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click'); await flushPromises()
    expect(saveExcelBPSDefaults).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('autoBPSOps.defaults.modelsRequired')
  })
  it('prevents double save and retains edits when saving fails', async () => {
    let reject!: (error: Error) => void
    vi.mocked(saveExcelBPSDefaults).mockReturnValueOnce(new Promise((_, no) => { reject = no }))
    const wrapper = open(); await flushPromises()
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click')
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click')
    expect(saveExcelBPSDefaults).toHaveBeenCalledTimes(1)
    reject(new Error('offline')); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await wrapper.get('[data-testid="bps-save-defaults"]').trigger('click'); await flushPromises()
    expect(saveExcelBPSDefaults).toHaveBeenCalledTimes(2)
  })
})
