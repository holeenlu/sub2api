import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PanelRateLimit from '@/features/access-policy/settings/PanelRateLimitSettingsPanel.vue'
import { accesspolicySettingsServicesKey } from '@/features/access-policy/settings/ports'
enableAutoUnmount(afterEach)
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
describe('independent configuration sections', () => {
  it('keeps a failed read visibly unavailable instead of saving fallback defaults', async () => {
    const get = vi.fn().mockRejectedValue(new Error('Unavailable'))
    const update = vi.fn().mockResolvedValue({})
    const wrapper = mount(PanelRateLimit, { global: { stubs: { Toggle: true, Icon: true }, provide: { [accesspolicySettingsServicesKey as symbol]: { control: { getPanelRateLimitSettings: get, updatePanelRateLimitSettings: update }, notifications: { showError: vi.fn(), showSuccess: vi.fn() } } } } })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.failedToLoad')
    expect((wrapper.get('fieldset').element as HTMLFieldSetElement).disabled).toBe(true)
    const save = wrapper.findAll('button').find(button => button.text().includes('panelRateLimit.save'))
    if (save) await save.trigger('click')
    expect(update).not.toHaveBeenCalled()
    get.mockResolvedValue({ enabled: false })
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect((wrapper.get('fieldset').element as HTMLFieldSetElement).disabled).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
