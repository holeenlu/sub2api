import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import nativeEn from '@/i18n/locales/en'
import VendorManagement from '../components/VendorManagement.vue'

const list = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({ vendorManagementAPI: () => ({ list }) }))

describe('supplier resource totals', () => {
  it('counts each direct owner once and displays parent capacity separately', async () => {
    list.mockResolvedValue({
      total: 2,
      items: [
        { id: 1, tier: 1, name: 'Parent', status: 'active', platforms: [], groups: [], account_count: 0, proxy_count: 0, account_tree_count: 1, proxy_tree_count: 1, account_limit: 1000, proxy_limit: 1000 },
        { id: 2, tier: 2, parent_vendor_id: 1, name: 'Child', status: 'active', platforms: [], groups: [], account_count: 1, proxy_count: 1, account_tree_count: 1, proxy_tree_count: 1, account_limit: 1000, proxy_limit: 1000 },
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
    expect(wrapper.text().match(/Including children: 1 \/ 1000/g)).toHaveLength(2)
    const parentAccounts = wrapper.get('button[aria-label="Account pool · Parent"]')
    await parentAccounts.trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual(['accounts', 1])
    wrapper.unmount()
  })
})
