import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { operationsServicesKey } from '../../services'
import { createOperationsTestServices } from '../../__tests__/services'
import ChannelStatusPage from '../pages/ChannelStatusPage.vue'

vi.mock('../pages/ChannelStatusV1Page.vue', () => ({
  __esModule: true,
  default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v1' }) }),
}))
vi.mock('../pages/ChannelStatusV2Page.vue', () => ({
  __esModule: true,
  default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v2' }) }),
}))
enableAutoUnmount(afterEach)

function mountPage(settings: { channel_monitor_enabled?: boolean; channel_monitor_mode?: string } | null) {
  const publicSettings = ref(settings)
  const services = createOperationsTestServices({ publicSettings })
  return { publicSettings, wrapper: mount(ChannelStatusPage, { global: { provide: { [operationsServicesKey as symbol]: services } } }) }
}

describe('ChannelStatusPage mode resolution', () => {
  it.each([null, {}, { channel_monitor_mode: undefined }, { channel_monitor_mode: '' }, { channel_monitor_mode: 'unknown' }, { channel_monitor_mode: 'v2' }])('renders the default V2 for %j', async settings => {
    const { wrapper } = mountPage(settings)
    await flushPromises()
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })

  it('renders V1 only when explicitly configured and enabled', async () => {
    const { wrapper, publicSettings } = mountPage({ channel_monitor_mode: 'v1' })
    await flushPromises()
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    publicSettings.value = { channel_monitor_mode: 'v1', channel_monitor_enabled: false }
    await flushPromises()
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })

  it('reacts to settings arriving after the default render and clearing on scope change', async () => {
    const { wrapper, publicSettings } = mountPage(null)
    await flushPromises()
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    publicSettings.value = { channel_monitor_mode: 'v1' }
    await flushPromises()
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    publicSettings.value = null
    await flushPromises()
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })
})
