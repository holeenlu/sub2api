import { installMonitorTestServices } from '@/features/operations/monitor/__tests__/services'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ChannelStatusV1View from '../pages/ChannelStatusV1Page.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), status: vi.fn() }))
vi.mock('@/features/operations/api', async original => ({ ...await original<typeof import('@/features/operations/api')>(), channelMonitorUserAPI: mocks }))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

function detail(availability: number) {
  return { id: 1, models: [{ model: 'model', availability_15d: availability, availability_30d: availability }] }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  localStorage.clear()
  localStorage.setItem('channel-status-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 30 }))
  scopeKey.value = 'actor-a:user'
  mocks.list.mockImplementation(async () => ({ items: [{ id: 1, name: 'Monitor', primary_model: 'model', primary_status: 'operational', availability_7d: 99 }] }))
  mocks.status.mockResolvedValue(detail(90))
})

function mountView() {
  return shallowMount(ChannelStatusV1View, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      MonitorHero: { props: ['loading'], emits: ['update:window', 'refresh'], template: `<div :data-loading="loading">
        <button class="fifteen" @click="$emit('update:window', '15d')">15d</button>
        <button class="thirty" @click="$emit('update:window', '30d')">30d</button>
        <button class="refresh" @click="$emit('refresh')">Refresh</button>
      </div>` },
      MonitorCardGrid: false,
      MonitorCard: { props: ['availabilityValue'], template: '<output>{{ availabilityValue }}</output>' }
    } }
  })
}

describe('channel monitor automatic detail refresh', () => {
  it.each(['fifteen', 'thirty'])('refreshes the cached %s-day availability on timer ticks', async (tab) => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.' + tab).trigger('click')
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('90')
    mocks.status.mockResolvedValue(detail(98))
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('98')
    expect(mocks.status).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-loading]').attributes('data-loading')).toBe('false')
  })

  it('keeps the seven-day view on the list endpoint without extra detail requests', async () => {
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(mocks.status).not.toHaveBeenCalled()
    expect(wrapper.get('output').text()).toBe('99')
  })

  it('still refreshes cached availability on manual reload', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.fifteen').trigger('click')
    await flushPromises()
    mocks.status.mockResolvedValue(detail(98))
    await wrapper.get('.refresh').trigger('click')
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('98')
  })
})

const scopeKey = ref<string | null>('actor-a:user')
const { services } = installMonitorTestServices({ scopeKey })

describe('channel monitor scope ownership', () => {
  it('ignores a late list after logout and stops the refresh timer', async () => {
    let resolve!: (value: { items: { id: number; name: string }[] }) => void
    mocks.list.mockReturnValueOnce(new Promise(res => { resolve = res }))
    const wrapper = mountView()
    await flushPromises()
    const signal = mocks.list.mock.calls[0][0].signal as AbortSignal
    scopeKey.value = null
    await flushPromises()
    resolve({ items: [{ id: 1, name: 'old actor' }] })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(120000)
    expect(signal.aborted).toBe(true)
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('output')).toHaveLength(0)
    expect(services.notifyError).not.toHaveBeenCalled()
  })

  it('does not apply a pending detail from the previous actor', async () => {
    let resolve!: (value: ReturnType<typeof detail>) => void
    mocks.status.mockReturnValueOnce(new Promise(res => { resolve = res }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.fifteen').trigger('click')
    await flushPromises()
    const signal = mocks.status.mock.calls[0][1].signal as AbortSignal
    scopeKey.value = 'actor-b:user'
    await flushPromises()
    resolve(detail(12))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.get('output').text()).toBe('90')
  })
})
