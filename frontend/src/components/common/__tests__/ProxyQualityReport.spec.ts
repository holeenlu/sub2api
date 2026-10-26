import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import ProxyQualityReport from '../ProxyQualityReport.vue'
import type { ProxyQualityCheckResult } from '@/types'
import { proxyQualityOverallStatus } from '@/utils/proxyQuality'

describe('shared proxy quality report', () => {
  it('uses backend item states independently of HTTP status and localizes the summary', () => {
    const report: ProxyQualityCheckResult = {
      proxy_id: 1, score: 38, grade: 'F', summary: '服务端中文摘要', checked_at: 1720000000,
      passed_count: 2, warn_count: 1, failed_count: 1, challenge_count: 1,
      base_latency_ms: 0, exit_ip: '192.0.2.1', country: 'US',
      items: [
        { target: 'base_connectivity', status: 'pass', latency_ms: 0 },
        { target: 'openai', status: 'pass', http_status: 401, latency_ms: 12 },
        { target: 'anthropic', status: 'warn', http_status: 429, message: 'Target rate limited' },
        { target: 'gemini', status: 'fail', message: 'Target connection failed' },
        { target: 'grok', status: 'challenge', http_status: 403, message: 'Target requires verification' }
      ]
    }
    const w = mount(ProxyQualityReport, {
      props: { name: 'Shared report', report },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] }
    })
    expect(w.text()).toContain('2 passed, 1 warnings, 1 failed, 1 challenges')
    expect(w.text()).not.toContain('服务端中文摘要')
    expect(w.text()).toContain('0 ms')
    expect(w.findAll('tbody tr').map(row => row.find('.badge').text())).toEqual(['Pass', 'Pass', 'Warn', 'Fail', 'Challenge'])
    expect(w.findAll('tbody tr')[1].text()).toContain('401')
    expect(w.findAll('tbody tr')[2].text()).toContain('—')
    expect(proxyQualityOverallStatus(report)).toBe('challenge')
    w.unmount()
  })
})
