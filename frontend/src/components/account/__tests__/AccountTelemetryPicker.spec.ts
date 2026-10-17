import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTelemetryPicker from '../AccountTelemetryPicker.vue'
import { readAccountTelemetry, withAccountTelemetry, type AccountTelemetry } from '@/utils/accountTelemetry'

const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), copy: vi.fn() }))
vi.mock('@/api/admin/ops', () => ({ listClientTelemetry: mocks.list, getClientTelemetry: mocks.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: ref(false), copyToClipboard: mocks.copy }) }))
const payload = '{ "event_type":"ClaudeCodeInternalEvent","event_data":{"future":9007199254740993}}'
const sample = { id: 70, user_id: 9, account_id: 55, api_key_id: 7, client: 'claude', device_id: 'client-device', payload_json: payload, headers: { 'User-Agent': ['client-UA'] }, received_count: 8 }
function editor(initial: AccountTelemetry | null = null) {
  const value = ref(initial)
  const wrapper = mount(defineComponent({ components: { AccountTelemetryPicker }, setup: () => ({ value }), template: '<AccountTelemetryPicker v-model="value" />' }), {
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, Pagination: true } },
  })
  return { wrapper, value }
}
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue({ items: [sample], total: 1 }); mocks.get.mockResolvedValue(sample) })

describe('account telemetry copy', () => {
  it('copies independent bytes and headers without IDs, then survives source removal', async () => {
    const { wrapper, value } = editor()
    expect(mocks.list).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="account-telemetry-open"]').trigger('click')
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledWith({ page: 1, page_size: 10, time_range: 'all', user_id: undefined })
    await wrapper.get('[data-testid="account-telemetry-select"]').trigger('click')
    await flushPromises()
    expect(value.value).toEqual({ client: 'claude', payload_json: payload, headers: { 'User-Agent': ['client-UA'] } })
    expect(value.value?.headers['User-Agent']).not.toBe(sample.headers['User-Agent'])
    expect((wrapper.get('[data-testid="account-telemetry-body"]').element as HTMLTextAreaElement).value).toBe(payload)
    const saved = withAccountTelemetry({ unrelated: true }, value.value)!
    mocks.get.mockRejectedValue(new Error('sample deleted'))
    wrapper.unmount()
    const reopened = editor(readAccountTelemetry(saved))
    expect((reopened.wrapper.get('[data-testid="account-telemetry-body"]').element as HTMLTextAreaElement).value).toBe(payload)
    expect(mocks.get).toHaveBeenCalledTimes(1)
    await reopened.wrapper.get('[data-testid="account-telemetry-clear"]').trigger('click')
    expect(withAccountTelemetry(saved, reopened.value.value)).toEqual({ unrelated: true })
    reopened.wrapper.unmount()
  })

  it('discards late detail loads when the editor is unmounted', async () => {
    let resolve!: (value: unknown) => void
    mocks.get.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { wrapper, value } = editor()
    await wrapper.get('[data-testid="account-telemetry-open"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="account-telemetry-select"]').trigger('click')
    wrapper.unmount()
    resolve(sample)
    await flushPromises()
    expect(value.value).toBeNull()
  })
})
