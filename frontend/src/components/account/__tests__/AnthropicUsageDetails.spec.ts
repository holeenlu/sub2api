import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import AnthropicUsageDetails from '../AnthropicUsageDetails.vue'
import UsageProgressBar from '../UsageProgressBar.vue'
import type { AnthropicUsageDetails as Details, UsageObservation } from '@/types'

const i18n = () => createI18n({ legacy: false, locale: 'zh', messages: { zh } })
const at = '2026-10-19T03:00:00Z'
const observation = (value: unknown, source: UsageObservation['source'] = 'upstream_api', sampled_at: string | null = at): UsageObservation => ({ value, source, sampled_at })

describe('Anthropic usage provenance', () => {
  it('renders zero, false and null separately with source and original sample time', async () => {
    const details: Details = {
      profile_access: 'unknown',
      observations: {
        'profile.rate_limit_tier': observation('default_claude_max_20x'),
        'overage.disabled_reason': observation('org_level_disabled', 'upstream_headers'),
        'extra_usage.monthly_limit': observation(0),
        'extra_usage.is_enabled': observation(false),
        'extra_usage.utilization': observation(null),
        'authorization.scopes': observation([], 'local_credentials', null),
        'seven_day.window_stats': observation({ requests: 2, cost: 12 }, 'local_statistics'),
        'seven_day.estimated_total_cost': observation(30, 'estimate'),
        limits: observation([
          { kind: 'weekly_scoped', percent: 0, is_active: false, scope: { model: { display_name: 'Fable' } } },
          { kind: 'future_scope', percent: null, scope: { surface: { display_name: 'Cowork' } } }
        ])
      }
    }
    const wrapper = mount(AnthropicUsageDetails, { props: { details }, global: { plugins: [i18n()] } })
    expect(wrapper.text()).toContain('OAuth 权限未知')
    expect(wrapper.text()).toContain('default_claude_max_20x')
    expect(wrapper.text()).toContain('org_level_disabled')
    const disclosure = wrapper.get('details')
    disclosure.element.open = true
    await disclosure.trigger('toggle')
    expect(wrapper.get('[data-field="extra_usage.monthly_limit"]').text()).toContain('原始单位）: 0')
    expect(wrapper.get('[data-field="extra_usage.is_enabled"]').text()).toContain('否')
    expect(wrapper.get('[data-field="extra_usage.utilization"]').text()).toContain('未知')
    expect(wrapper.get('[data-field="authorization.scopes"]').text()).toContain('采样时间未知')
    expect(wrapper.get('[data-field="seven_day.window_stats.cost"]').text()).toContain('本地统计')
    expect(wrapper.get('[data-field="seven_day.estimated_total_cost"]').text()).toContain('估算值')
    expect(wrapper.get('time').attributes('datetime')).toBe(at)
    const limits = wrapper.findAll('[data-test="anthropic-limit"]')
    expect(limits[0].text()).toContain('0%')
    expect(limits[0].text()).toContain('Fable')
    expect(limits[1].text()).toContain('—')
    expect(limits[1].text()).toContain('Cowork')
    wrapper.unmount()
  })

  it('keeps an unknown window unknown and labels local statistics and estimates independently', () => {
    const wrapper = mount(UsageProgressBar, {
      props: {
        label: '7d', color: 'emerald', utilization: 0, utilizationKnown: false,
        observation: observation(null),
        windowStats: { requests: 2, tokens: 100, cost: 12 },
        statsObservation: observation({}, 'local_statistics'),
        estimatedTotalCost: 30, estimateObservation: observation(30, 'estimate')
      },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).not.toContain('0%')
    expect(wrapper.text()).toContain('本地统计')
    expect(wrapper.text()).toContain('估算值')
    expect(wrapper.get('[data-source="upstream_api"]').attributes('title')).toContain('采样时间')
    wrapper.unmount()
  })
})
