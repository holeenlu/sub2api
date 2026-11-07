import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import type { Composer } from 'vue-i18n'
import type { AdminUsageLog, UsageSamplingDetails } from '../../api/types'
import { createUsageTestServices } from '../../__tests__/services'
import { usageServicesKey } from '../../services'
import { adminUsageExportHeaders, adminUsageExportRows, userUsageExportHeaders, userUsageExportRows } from '../../model/exportRows'
import { escapeUsageCSV } from '../../api/usageExport'
import en from '../../locales/en'
import ja from '../../locales/ja'
import zh from '../../locales/zh'
import zhTW from '../../locales/zh-TW'
import UsageTable from '../UsageTable.vue'
import UsageSamplingBreakdown from '../UsageSamplingBreakdown.vue'

vi.mock('@/shared/lib/ipGeoLookup', () => ({ useIpGeoLookup: () => ({
  scope: ref('sampling-test'), getEntry: () => ({ status: 'idle' }), fetchOne: vi.fn(), fetchBatch: vi.fn(),
}) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, args?: { count: number }) => args ? `${key} ${args.count}` : key }) }))
describe('Cloud sampling detail', () => {
  it.each([undefined, null])('keeps historical %s details absent rather than inventing a zero segment', (details) => {
    const wrapper = mount(UsageSamplingBreakdown, { props: { details } })
    expect(wrapper.find('[data-testid="sampling-breakdown"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it.each(['cloud_response', 'cloud_turn'] as const)('renders %s tokens and costs without duplicating reasoning', (scope) => {
    const wrapper = mount(UsageSamplingBreakdown, { props: { details: { version: 1, scope, pricing_mode: 'token', hosted_cost: 0.01, hosted_actual_cost: 0.02, samples: [
      { input_tokens: 20, cache_read_tokens: 80, cache_creation_tokens: 0, output_tokens: 5, reasoning_tokens: 4, total_cost: 0.12, actual_cost: 0.24 },
      { input_tokens: 30, cache_read_tokens: 100, cache_creation_tokens: 0, output_tokens: 8, reasoning_tokens: 6, total_cost: 0.2, actual_cost: 0.4 },
    ] } } })
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.findAll('tbody tr')[0].findAll('td').map(x => x.text())).toEqual(['1', '20', '80', '0', '5', '$0.12000000', '$0.24000000'])
    expect(wrapper.text()).toContain('usage.samplingHosted: $0.01000000 / $0.02000000')
    wrapper.unmount()
  })
  it('does not invent per-step charges on root-priced plans', () => {
    const wrapper = mount(UsageSamplingBreakdown, { props: { details: { version: 1, scope: 'cloud_turn', pricing_mode: 'per_request', hosted_cost: 0, hosted_actual_cost: 0, samples: [{ input_tokens: 1, output_tokens: 2, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_cost: null, actual_cost: null }] } } })
    expect(wrapper.text()).toContain('usage.samplingRootPrice')
    expect(wrapper.findAll('tbody td').slice(-2).map(x => x.text())).toEqual(['—', '—'])
    wrapper.unmount()
  })
})


const translate = ((key: string) => key) as Composer['t']
const samplingDetails: UsageSamplingDetails = {
  version: 1, scope: 'cloud_response', pricing_mode: 'token', hosted_cost: 0, hosted_actual_cost: 0,
  samples: [{ input_tokens: 20, cache_read_tokens: 80, cache_creation_tokens: 0, output_tokens: 5,
    reasoning_tokens: 4, total_cost: 0, actual_cost: null }],
}
function usageLog(details?: UsageSamplingDetails | null): AdminUsageLog {
  return {
    id: 1, user_id: 1, api_key_id: 1, request_id: 'cloud-stage-1', model: 'gpt-5',
    group_id: null, subscription_id: null, billing_status: 'settled', billing_mode: 'token',
    input_tokens: 20, output_tokens: 5, cache_read_tokens: 80, cache_creation_tokens: 0,
    cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0, cache_creation_30m_tokens: 0, cache_creation_12h_tokens: 0,
    input_cost: 0.12, output_cost: 0.01, cache_creation_cost: 0, cache_read_cost: 0,
    total_cost: 0.13, actual_cost: 0.26, rate_multiplier: 2, long_context_billing_applied: false, billing_type: 0,
    stream: true, native_compaction_v2: false, duration_ms: 1000, first_token_ms: 100,
    image_count: 0, image_size: null, image_input_size: null, image_output_size: null,
    image_size_source: null, image_size_breakdown: null,
    image_input_tokens: 0, image_input_cost: 0, image_output_tokens: 0, image_output_cost: 0,
    user_agent: null, cache_ttl_overridden: false, created_at: '2026-10-28T00:00:00Z', sampling_details: details,
  }
}

describe('shared usage sampling consumers', () => {
  it.each([true, false])('shows sampling in the token popover with account billing %s', async (showAccountBilling) => {
    const wrapper = mount(UsageTable, {
      props: { data: [usageLog(samplingDetails)], columns: [{ key: 'tokens', label: 'Tokens' }], showAccountBilling },
      global: {
        provide: { [usageServicesKey as symbol]: createUsageTestServices({
          permissions: ref({ isSuperAdmin: showAccountBilling, canReadBillingCost: showAccountBilling, canExport: true }),
        }) },
        stubs: { Teleport: true },
      },
    })
    try {
      await wrapper.get('button[aria-label="usage.tokenDetails"]').trigger('click')
      const breakdown = wrapper.get('[data-testid="sampling-breakdown"]')
      expect(breakdown.text()).toContain('usage.samplingCount 1')
      expect(breakdown.findAll('tbody td').map(cell => cell.text())).toEqual(['1', '20', '80', '0', '5', '$0.00000000', '—'])
      expect(wrapper.get('[role="tooltip"] span.font-semibold.text-blue-400').text()).toBe('105')
    } finally {
      wrapper.unmount()
    }
  })

  it.each(['cloud_response', 'cloud_turn'] as const)('exports %s details in CSV and role-filtered admin rows', (scope) => {
    const details = { ...samplingDetails, scope }
    const row = usageLog(details)
    const userHeaders = userUsageExportHeaders(translate)
    const userRow = userUsageExportRows([row], translate)[0]
    expect(userHeaders.slice(-2)).toEqual(['usage.samplingSteps', 'usage.samplingExport'])
    expect(userRow).toHaveLength(userHeaders.length)
    expect(userRow.slice(-2)).toEqual(['1', escapeUsageCSV(JSON.stringify(details))])
    // Parse the escaped CSV field to verify null, zero, reasoning and stage scope survive export.
    expect(JSON.parse(userRow.at(-1)!.slice(1, -1).replaceAll('""', '"'))).toEqual(details)
    for (const isSuperAdmin of [false, true]) {
      for (const canReadBillingCost of [false, true]) {
        const permissions = { isSuperAdmin, canReadBillingCost, canExport: true }
        const headers = adminUsageExportHeaders(translate, permissions)
        const exported = adminUsageExportRows([row], translate, permissions)[0]
        expect(headers.slice(-2)).toEqual(['usage.samplingSteps', 'usage.samplingExport'])
        expect(exported).toHaveLength(headers.length)
        expect(exported.slice(-2)).toEqual([1, JSON.stringify(details)])
        expect(headers.includes('usage.accountBilled')).toBe(canReadBillingCost)
        expect(headers.includes('admin.usage.account')).toBe(isSuperAdmin)
      }
    }
  })

  it.each([undefined, null])('leaves missing %s sampling export fields blank', (details) => {
    const row = usageLog(details)
    expect(userUsageExportRows([row], translate)[0].slice(-2)).toEqual(['', ''])
    expect(adminUsageExportRows([row], translate, { isSuperAdmin: false, canReadBillingCost: false, canExport: true })[0].slice(-2)).toEqual(['', ''])
  })

  it('distinguishes a recorded empty sampling list from missing details', () => {
    const details = { ...samplingDetails, samples: [] }
    expect(userUsageExportRows([usageLog(details)], translate)[0].slice(-2)).toEqual(['0', escapeUsageCSV(JSON.stringify(details))])
  })

  it.each([['en', en], ['ja', ja], ['zh', zh], ['zh-TW', zhTW]] as const)('provides the six sampling labels for %s', (_locale, messages) => {
    for (const key of ['samplingSteps', 'samplingExport', 'samplingCount', 'samplingHint', 'samplingRootPrice', 'samplingHosted'] as const) {
      expect(messages.usage[key]).toBeTruthy()
    }
    expect(messages.usage.samplingCount).toContain('{count}')
  })
})
