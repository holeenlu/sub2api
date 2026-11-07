import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { ipGeoLookupKey } from '@/shared/lib/ipGeoLookup'
import { enableAutoUnmount, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  getEntry: vi.fn(),
  fetchOne: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const table: Record<string, string> = {
          'usage.ipGeo.fetch': 'Fetch region',
          'usage.ipGeo.fetching': 'Fetching...',
          'usage.ipGeo.failed': 'Failed',
          'usage.ipGeo.private': 'Private address',
          'usage.ipGeo.refreshTitle': 'Refresh',
          'usage.ipGeo.detailOrg': 'ISP',
          'usage.ipGeo.detailTimezone': 'Timezone',
          'usage.ipGeo.detailAccuracy': 'Accuracy',
          'usage.ipGeo.detailCoordinates': 'Coordinates',
        }
        return table[key] ?? key
      },
    }),
  }
})

import IpGeoCell from '../IpGeoCell.vue'
enableAutoUnmount(afterEach)
const mountCell = (ip: string) => mount(IpGeoCell, { props: { ip }, global: { provide: { [ipGeoLookupKey as symbol]: { ...mocks, scope: ref('test-scope') } } } })

describe('IpGeoCell', () => {
  beforeEach(() => {
    mocks.getEntry.mockReset()
    mocks.fetchOne.mockReset()
  })

  it('renders a clickable fetch link in idle state and triggers fetchOne on click', async () => {
    mocks.getEntry.mockReturnValue({ status: 'idle' })
    const wrapper = mountCell('8.8.8.8')
    expect(wrapper.text()).toContain('Fetch region')
    await wrapper.find('button').trigger('click')
    expect(mocks.fetchOne).toHaveBeenCalledWith('8.8.8.8', false, { signal: expect.any(AbortSignal) })
  })

  it('renders loading state', () => {
    mocks.getEntry.mockReturnValue({ status: 'loading' })
    const wrapper = mountCell('8.8.8.8')
    expect(wrapper.text()).toContain('Fetching...')
  })

  it('renders success state with label, tooltip detail, and a refresh button', async () => {
    mocks.getEntry.mockReturnValue({
      status: 'success',
      label: 'CN · Guangdong · Shenzhen',
      detail: {
        organization: 'AS4134 Chinanet',
        timezone: 'Asia/Shanghai',
        accuracy: 10,
        latitude: '22.5',
        longitude: '114.0',
      },
    })
    const wrapper = mountCell('121.35.47.43')
    expect(wrapper.text()).toContain('CN · Guangdong · Shenzhen')
    const buttons = wrapper.findAll('button')
    expect(buttons.length).toBe(2)
    expect(buttons[0].attributes('title')).toContain('AS4134 Chinanet')
    expect(buttons[0].attributes('title')).toContain('Asia/Shanghai')
    await buttons[1].trigger('click')
    expect(mocks.fetchOne).toHaveBeenCalledWith('121.35.47.43', true, { signal: expect.any(AbortSignal) })
  })

  it('opens the external lookup page when the label is clicked', async () => {
    mocks.getEntry.mockReturnValue({ status: 'success', label: 'US · California', detail: {} })
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const wrapper = mountCell('8.8.4.4')
    await wrapper.findAll('button')[0].trigger('click')
    expect(openSpy).toHaveBeenCalledWith(
      'https://www.iplocation.net/ip-lookup?query=8.8.4.4',
      '_blank',
      'noopener,noreferrer'
    )
    openSpy.mockRestore()
  })

  it('renders failed state as a clickable retry', async () => {
    mocks.getEntry.mockReturnValue({ status: 'error' })
    const wrapper = mountCell('8.8.8.8')
    expect(wrapper.text()).toContain('Failed')
    await wrapper.find('button').trigger('click')
    expect(mocks.fetchOne).toHaveBeenCalledWith('8.8.8.8', false, { signal: expect.any(AbortSignal) })
  })

  it('renders private state as non-clickable text', () => {
    mocks.getEntry.mockReturnValue({ status: 'private' })
    const wrapper = mountCell('192.168.1.1')
    expect(wrapper.text()).toContain('Private address')
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
