import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages
import VendorManagement from '../components/VendorManagement.vue'

const list = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({ vendorManagementAPI: () => ({ list }) }))

describe('supplier resource totals', () => {
  it.each([1000, 0])('counts each direct owner once and displays a resource cap of %i correctly', async limit => {
    list.mockResolvedValue({
      total: 2,
      items: [
        { id: 1, tier: 1, name: 'Parent', status: 'active', platforms: [], groups: [], account_count: 0, proxy_count: 0, account_tree_count: 1, proxy_tree_count: 1, account_limit: limit, proxy_limit: limit },
        { id: 2, tier: 2, parent_vendor_id: 1, name: 'Child', status: 'active', platforms: [], groups: [], account_count: 1, proxy_count: 1, account_tree_count: 1, proxy_tree_count: 1, account_limit: limit, proxy_limit: limit },
      ],
    })
    const wrapper = mount(VendorManagement, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } })],
        stubs: {
          Icon: true, PlatformIcon: true, Select: true, Pagination: true,
          IdentityReconciliation: true,
          DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-accounts" :row="row" /><slot name="cell-proxies" :row="row" /></div></div>' },
        },
      },
    })
    await flushPromises()
    const cards = wrapper.findAll('section > div.grid > div')
    expect(cards).toHaveLength(4)
    expect(cards[2].findAll('p')[1].text()).toBe('1')
    expect(cards[3].findAll('p')[1].text()).toBe('1')
    const cap = limit === 0 ? 'Unlimited' : '1000'
    expect(wrapper.text().match(new RegExp('Including children: 1 / ' + cap, 'g'))).toHaveLength(2)
    expect(wrapper.get('button[aria-label="Account pool · Child"]').text()).toContain('1 / ' + cap)
    expect(wrapper.find('.text-amber-600').exists()).toBe(false)
    const parentAccounts = wrapper.get('button[aria-label="Account pool · Parent"]')
    await parentAccounts.trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual(['accounts', 1])
    wrapper.unmount()
  })
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
