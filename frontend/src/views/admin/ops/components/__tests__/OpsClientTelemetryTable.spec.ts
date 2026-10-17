import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsClientTelemetryTable from '../OpsClientTelemetryTable.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), copy: vi.fn() }))
vi.mock('@/api/admin/ops', () => ({ listClientTelemetry: mocks.list, getClientTelemetry: mocks.detail }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: ref(false), copyToClipboard: mocks.copy }) }))
const body = '{ "large":9007199254740993,"extension":"<img src=x onerror=alert(1)>" }'
const sample = { id: 7, user_id: 11, api_key_id: 1, client: 'claude', device_id: 'device', last_seen_at: '2026-10-19T01:00:00Z', created_at: '2026-10-19T00:00:00Z', received_count: 8, session_id: 'session', payload_json: body }
function table() {
  return mount(OpsClientTelemetryTable, { global: { stubs: {
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
    Pagination: true,
  } } })
}
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue({ items: [sample], total: 1 }); mocks.detail.mockResolvedValue(sample) })

describe('client telemetry samples', () => {
  it('loads on demand, filters users, and copies exact raw events without rendering HTML', async () => {
    const wrapper = table()
    expect(mocks.list).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="telemetry-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="telemetry-user_id"]').setValue('11')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith({ time_range: '24h', session_id: undefined, user_id: 11, page: 1, page_size: 20 })
    await wrapper.get('[data-testid="telemetry-detail"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="telemetry-body"]').element as HTMLTextAreaElement).value).toBe(body)
    expect(wrapper.find('img').exists()).toBe(false)
    await wrapper.get('[data-testid="telemetry-copy"]').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith(body)
    wrapper.unmount()
  })

  it('discards stale refresh responses and shows query failures', async () => {
    const wrapper = table()
    let finish!: (value: unknown) => void
    mocks.list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('[data-testid="telemetry-toggle"]').trigger('click')
    await wrapper.setProps({ refreshToken: 1 })
    await flushPromises()
    finish({ items: [{ ...sample, device_id: 'stale-device' }], total: 1 })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale-device')
    mocks.list.mockRejectedValueOnce(new Error('unavailable'))
    await wrapper.setProps({ refreshToken: 2 })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="telemetry-detail"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
