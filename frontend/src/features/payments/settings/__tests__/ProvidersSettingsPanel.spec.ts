import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProvidersSettingsPanel from '../ProvidersSettingsPanel.vue'
import { paymentsSettingsServicesKey } from '../ports'
import type { ProviderInstance } from '../../api/types'
import { deferred } from '../../__tests__/harness'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => {
  if (key === 'admin.settings.payment.field_certSerial') return '证书序列号'
  if (key === 'payment.errors.MISSING_FIELD') return '缺少：' + params?.key
  return key
} }) }))
const ProviderList = defineComponent({ name: 'PaymentProviderList', props: ['providers', 'loading'],
  emits: ['refresh', 'create', 'edit', 'delete', 'toggleField', 'toggleType', 'reorder'], template: '<div />' })
const ProviderDialog = defineComponent({ name: 'PaymentProviderDialog', props: ['show', 'saving', 'editing'],
  emits: ['close', 'save'], methods: { reset() {}, loadProvider() {} }, template: '<div />' })
const Confirm = defineComponent({ name: 'ConfirmDialog', props: ['show'], emits: ['confirm', 'cancel'], template: '<div />' })
const provider: ProviderInstance = { id: 1, provider_key: 'stripe', name: 'provider', config: {}, supported_types: ['alipay'],
  enabled: true, payment_mode: 'redirect', refund_enabled: true, allow_user_refund: true, limits: '', sort_order: 0 }
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })
function setup(options: { get?: ReturnType<typeof vi.fn>; scope?: string | null } = {}) {
  const scopeKey = ref<string | null>(options.scope === undefined ? 'admin:1' : options.scope)
  const payment = { getProviders: options.get ?? vi.fn().mockResolvedValue({ data: [provider] }),
    createProvider: vi.fn().mockResolvedValue({ data: provider }), updateProvider: vi.fn().mockResolvedValue({ data: provider }), deleteProvider: vi.fn().mockResolvedValue({}) }
  const notifications = { showSuccess: vi.fn(), showError: vi.fn(), showWarning: vi.fn() }
  const wrapper = mount(ProvidersSettingsPanel, {
    props: { modelValue: { payment_enabled: true, payment_enabled_types: ['stripe', 'alipay', 'wxpay'] } },
    global: { provide: { [paymentsSettingsServicesKey as symbol]: { scopeKey, payment, notifications, docUrl: '' } },
      stubs: { PaymentProviderList: ProviderList, PaymentProviderDialog: ProviderDialog, ConfirmDialog: Confirm } },
  })
  wrappers.push(wrapper)
  return { wrapper, scopeKey, payment, notifications, list: wrapper.getComponent(ProviderList), dialog: wrapper.getComponent(ProviderDialog) }
}

describe('ProvidersSettingsPanel ownership', () => {
  it('does not read providers without an authenticated scope', async () => {
    const { payment } = setup({ scope: null }); await flushPromises()
    expect(payment.getProviders).not.toHaveBeenCalled()
  })
  it('rejects old list results and preserves the next scope loading state', async () => {
    const old = deferred<{ data: ProviderInstance[] }>(), current = deferred<{ data: ProviderInstance[] }>()
    const { payment, scopeKey, list } = setup({ get: vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise) })
    const signal = payment.getProviders.mock.calls[0][0].signal as AbortSignal
    scopeKey.value = 'admin:2'; old.resolve({ data: [provider] }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(list.props('providers')).toEqual([]); expect(list.props('loading')).toBe(true)
    current.resolve({ data: [{ ...provider, id: 2 }] }); await flushPromises()
    expect(list.props('providers')[0].id).toBe(2); expect(list.props('loading')).toBe(false)
  })
  it('aborts a read on unmount without reporting a late error', async () => {
    const pending = deferred<unknown>()
    const { wrapper, payment, notifications } = setup({ get: vi.fn().mockReturnValue(pending.promise) })
    wrapper.unmount(); expect(payment.getProviders.mock.calls[0][0].signal.aborted).toBe(true)
    pending.reject(new Error('late read')); await flushPromises()
    expect(notifications.showError).not.toHaveBeenCalled()
  })
  it('keeps the newest list when refresh responses arrive out of order', async () => {
    const old = deferred<unknown>(), current = deferred<unknown>()
    const { list, payment } = setup({ get: vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise) })
    list.vm.$emit('refresh'); await flushPromises()
    current.resolve({ data: [{ ...provider, id: 2 }] }); await flushPromises()
    old.resolve({ data: [provider] }); await flushPromises()
    expect(payment.getProviders.mock.calls[0][0].signal.aborted).toBe(true)
    expect(list.props('providers')[0].id).toBe(2)
  })
  it('does not close another identity editor or refresh it after a late create', async () => {
    const { list, dialog, payment, scopeKey, notifications } = setup(); await flushPromises()
    const pending = deferred<unknown>(); payment.createProvider.mockReturnValueOnce(pending.promise)
    list.vm.$emit('create'); dialog.vm.$emit('save', { name: 'first', provider_key: 'stripe' }); await flushPromises()
    const signal = payment.createProvider.mock.calls[0][1].signal as AbortSignal
    scopeKey.value = 'admin:2'; await flushPromises(); list.vm.$emit('edit', { ...provider, id: 2 }); await flushPromises()
    pending.resolve({ data: provider }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(payment.getProviders).toHaveBeenCalledTimes(2)
    expect(dialog.props('show')).toBe(true); expect(dialog.props('editing').id).toBe(2)
    expect(notifications.showSuccess).not.toHaveBeenCalled()
  })
  it('blocks duplicate and unknown create commands while still permitting reads', async () => {
    const { list, dialog, payment, wrapper } = setup(); await flushPromises()
    const pending = deferred<unknown>(); payment.createProvider.mockReturnValueOnce(pending.promise)
    list.vm.$emit('create'); dialog.vm.$emit('save', { name: 'one' }); dialog.vm.$emit('save', { name: 'two' })
    expect(payment.createProvider).toHaveBeenCalledTimes(1)
    pending.reject(new Error('network outcome unknown')); await flushPromises()
    expect(wrapper.text()).toContain('network outcome unknown'); expect(dialog.props('saving')).toBe(true)
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    dialog.vm.$emit('save', { name: 'again' }); await flushPromises()
    expect(payment.createProvider).toHaveBeenCalledTimes(1); expect(payment.getProviders).toHaveBeenCalledTimes(3)
  })
  it('localizes provider field validation and permits a corrected retry', async () => {
    const { list, dialog, payment, notifications } = setup(); await flushPromises()
    payment.createProvider.mockRejectedValueOnce({ status: 400, reason: 'MISSING_FIELD', metadata: { key: 'certSerial' } })
    list.vm.$emit('create'); dialog.vm.$emit('save', { name: 'one' }); await flushPromises()
    expect(notifications.showError).toHaveBeenCalledWith('缺少：证书序列号')
    expect(dialog.props('show')).toBe(true); expect(dialog.props('saving')).toBe(false)
    dialog.vm.$emit('save', { name: 'fixed' }); await flushPromises()
    expect(payment.createProvider).toHaveBeenCalledTimes(2); expect(dialog.props('show')).toBe(false)
  })
  it('preserves explicit false refund cascade and supported type removal', async () => {
    const { list, payment } = setup(); await flushPromises()
    list.vm.$emit('toggleField', provider, 'refund_enabled'); await flushPromises()
    expect(payment.updateProvider).toHaveBeenLastCalledWith(1, { refund_enabled: false, allow_user_refund: false }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    list.vm.$emit('toggleType', provider, 'alipay'); await flushPromises()
    expect(payment.updateProvider).toHaveBeenLastCalledWith(1, { supported_types: [] }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })
  it('waits for all reorder responses before reloading a partially applied list', async () => {
    const { list, payment } = setup(); await flushPromises()
    const pending = deferred<unknown>()
    payment.updateProvider.mockRejectedValueOnce({ status: 400, message: 'first rejected' }).mockReturnValueOnce(pending.promise)
    const updates = [{ id: 2, sort_order: 0 }, { id: 1, sort_order: 1 }]
    list.vm.$emit('reorder', updates); await flushPromises()
    expect(payment.getProviders).toHaveBeenCalledTimes(1)
    list.vm.$emit('reorder', updates); expect(payment.updateProvider).toHaveBeenCalledTimes(2)
    pending.reject(new Error('second result unknown')); await flushPromises()
    expect(payment.getProviders).toHaveBeenCalledTimes(2)
    expect(payment.updateProvider).toHaveBeenNthCalledWith(1, 2, { sort_order: 0 }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    list.vm.$emit('reorder', updates); await flushPromises(); expect(payment.updateProvider).toHaveBeenCalledTimes(2)
  })
  it('discards a late deletion after the panel is disposed', async () => {
    const { list, wrapper, payment, notifications } = setup(); await flushPromises()
    const pending = deferred<unknown>(); payment.deleteProvider.mockReturnValueOnce(pending.promise)
    list.vm.$emit('delete', provider); wrapper.getComponent(Confirm).vm.$emit('confirm')
    wrapper.unmount(); expect(payment.deleteProvider.mock.calls[0][1].signal.aborted).toBe(true)
    pending.resolve({}); await flushPromises()
    expect(payment.getProviders).toHaveBeenCalledTimes(1); expect(notifications.showSuccess).not.toHaveBeenCalled()
  })
})
