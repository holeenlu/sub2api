import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CloudGatewaySettings from '../CloudGatewaySettings.vue'
import CloudReconciliation from '../CloudReconciliation.vue'
const api = vi.hoisted(() => ({ getCloudStatus: vi.fn() }))
vi.mock('@/api/admin/accounts', () => api)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSuperAdmin: false }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const props = { accountId: 1, enabled: false, canEdit: true }
const mountSettings = () => mount(CloudGatewaySettings, { props, global: { stubs: { CloudReconciliation: true } } })
beforeEach(() => { vi.resetAllMocks(); api.getCloudStatus.mockResolvedValue({ enabled: false, runtime_blocked: false }) })
describe('Cloud native authorization switch', () => {
  it('provides only a switch without OAuth or model verification controls', async () => {
    const wrapper = mountSettings(); await flushPromises()
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    wrapper.unmount()
  })
  it('preserves unresolved-usage and editor-permission restrictions', async () => {
    api.getCloudStatus.mockResolvedValue({ enabled: false, runtime_blocked: true })
    const wrapper = mountSettings(); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ enabled: true })
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ canEdit: false })
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('refreshes the switch after reconciliation without reopening the editor', async () => {
    api.getCloudStatus.mockResolvedValueOnce({ enabled: false, runtime_blocked: true })
    const wrapper = mountSettings(); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    wrapper.getComponent(CloudReconciliation).vm.$emit('refreshed')
    await flushPromises()
    expect(api.getCloudStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.findComponent(CloudReconciliation).exists()).toBe(false)
    wrapper.unmount()
  })
  it('does not let an old account status overwrite the selected account', async () => {
    let resolveOld!: (value: unknown) => void
    api.getCloudStatus.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = mountSettings()
    await wrapper.setProps({ accountId: 2 }); await flushPromises()
    resolveOld({ enabled: false, runtime_blocked: true }); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('status')?.at(-1)).toEqual([{ enabled: false, runtime_blocked: false }])
    wrapper.unmount()
  })
})
