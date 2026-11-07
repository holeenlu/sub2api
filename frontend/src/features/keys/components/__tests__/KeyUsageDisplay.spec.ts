import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import KeysTable from '../KeysTable.vue'
import KeyRestrictionFields from '../KeyRestrictionFields.vue'
import { keysServicesKey } from '../../services'
import { createKeysTestServices } from '../../__tests__/services'
import { keyFixture } from '../../__tests__/keyFixture'
import { editKeyDraft } from '../../model/keyDraft'
import { formatKeyCost } from '../../model/keyUsageDisplay'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } }, getLocale: () => 'en-US' }))
function metricsRow() {
  return { ...keyFixture(), rate_limit_5h: 10, rate_limit_1d: 20, rate_limit_7d: 30 }
}
const tableStub = {
  props: ['data'],
  template: '<div><section data-test="usage"><slot name="cell-usage" :row="data[0]" /></section><section data-test="windows"><slot name="cell-rate_limit" :row="data[0]" /></section></div>'
}
function renderTable(row = metricsRow()) {
  return mount(KeysTable, {
    props: { columns: [], apiKeys: [row], loading: false, selectedIds: [], userGroupRates: {}, groupOptions: [],
      usageStats: { 1: { api_key_id: 1, today_actual_cost: null } } },
    global: { provide: { [keysServicesKey as symbol]: createKeysTestServices() }, stubs: { DataTable: tableStub } }
  })
}
describe('Key usage keeps unknown separate from recorded zero', () => {
  it.each([undefined, null, NaN, Infinity, '0'])('shows a dash for the unknown observation %s', value => {
    expect(formatKeyCost(value)).toBe('—')
  })
  it('uses the existing fixed-cost formatter for known values and preserves precision', () => {
    expect(formatKeyCost(0)).toBe('$0.0000')
    expect(formatKeyCost(1.25, 2)).toBe('$1.25')
  })
  it('renders missing/invalid aggregates and counters as unknown without a zero progress bar', () => {
    const row = metricsRow()
    // Simulate incomplete JSON observations without broadening the API export contract.
    Reflect.deleteProperty(row, 'quota_used'); Reflect.deleteProperty(row, 'usage_5h')
    row.usage_1d = NaN; row.usage_7d = Infinity
    const wrapper = renderTable(row)
    expect(wrapper.get('[data-test="usage"]').text().match(/—/g)).toHaveLength(3)
    expect(wrapper.get('[data-test="windows"]').text().match(/—/g)).toHaveLength(3)
    expect(wrapper.findAll('[style*="width:"]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('$0.00'); expect(wrapper.text()).not.toContain('NaN')
    wrapper.unmount()
  })
  it('shows real zeros and their zero-width progress bars', async () => {
    const row = { ...metricsRow(), quota_used: 0, usage_5h: 0, usage_1d: 0, usage_7d: 0 }
    const wrapper = renderTable(row)
    await wrapper.setProps({ usageStats: { 1: { api_key_id: 1, today_actual_cost: 0, total_actual_cost: 0 } } })
    expect(wrapper.get('[data-test="usage"]').text()).toContain('$0.0000')
    expect(wrapper.get('[data-test="windows"]').text()).toContain('$0.00')
    expect(wrapper.findAll('[style*="width:"]')).toHaveLength(4)
    wrapper.unmount()
  })
  it('uses the same unknown presentation in the restriction editor', () => {
    const row = metricsRow()
    Reflect.deleteProperty(row, 'quota_used'); Reflect.deleteProperty(row, 'usage_5h')
    row.usage_1d = NaN; row.usage_7d = Infinity
    const wrapper = mount(KeyRestrictionFields, { props: { editing: true, selectedKey: row, modelValue: editKeyDraft(row) } })
    expect(wrapper.text().match(/—/g)).toHaveLength(4)
    expect(wrapper.text()).not.toContain('$0.0000'); expect(wrapper.findAll('[style*="width:"]')).toHaveLength(0)
    wrapper.unmount()
  })
})
