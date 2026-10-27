import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ExcelBPS403Badge from '../ExcelBPS403Badge.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}(${Object.entries(params).map(([name, value]) => `${name}=${value}`).join(',')})` : key
    })
  }
})

vi.mock('@/utils/format', async () => {
  const actual = await vi.importActual<typeof import('@/utils/format')>('@/utils/format')
  return {
    ...actual,
    formatDateTime: (date: Date) => date.toISOString()
  }
})

const disabledAt = '2026-09-26T15:04:05Z'
const movedAt = '2026-09-27T01:02:03Z'
const badge = '[data-test="excel-bps-403-badge"]'

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

function movedAccount(groupID: number, groupIDs: number[] | undefined, extra: Record<string, unknown> = {}): Account {
  return makeAccount({
    group_ids: groupIDs,
    extra: {
      openai_excel_bps: true,
      openai_excel_bps_403_moved_at: movedAt,
      openai_excel_bps_403_moved_group_id: groupID,
      ...extra
    }
  })
}

describe('ExcelBPS403Badge', () => {
  it.each(['pending', 'failed', 'probing', 'unknown'] as const)('shows the server-derived %s state and effective schedule', (state) => {
    const account = makeAccount({
      extra: { openai_excel_bps: false, openai_excel_bps_403_disabled_at: disabledAt },
      excel_bps_403_recovery: { state, interval_minutes: 30, last_probe_at: movedAt,
        next_probe_at: '2026-09-27T01:32:03Z',
        last_result: { status: 'failed', stage: 'request', reason: 'http_rejected', http_status: 403, duration_ms: 42, completed_at: movedAt } }
    })
    const wrapper = mount(ExcelBPS403Badge, { props: { account } })
    expect(wrapper.get(badge).text()).toBe(`admin.accounts.openai.excelBPS403State_${state}`)
    expect(wrapper.get(badge).attributes('title')).toContain('minutes=30')
    expect(wrapper.get(badge).attributes('title')).toContain('2026-09-27T01:32:03.000Z')
    expect(wrapper.get(badge).attributes('title')).toContain('status=403')
    expect(wrapper.get(badge).attributes('title')).toContain('excelBPS403LastAttempt')
  })

  it.each(['global_disabled', 'cloud_enabled', 'manual_disabled', 'recovery_disabled', 'account_expired', 'no_text_model'])('hides current alert for %s but retains history in details', (reason) => {
    const account = makeAccount({ extra: { openai_excel_bps_403_disabled_at: disabledAt },
      excel_bps_403_recovery: { state: 'paused', reason, interval_minutes: 60 } })
    const wrapper = mount(ExcelBPS403Badge, { props: { account } })
    expect(wrapper.find(badge).exists()).toBe(false)
    const details = mount(ExcelBPS403Badge, { props: { account, details: true } })
    expect(details.get('[data-test="excel-bps-403-details"]').text()).toContain(`excelBPS403Pause_${reason}`)
    expect(details.text()).toContain('2026-09-26T15:04:05.000Z')
    expect(details.text()).not.toContain('excelBPS403NextAttempt')
  })

  it('retains group action history without presenting it as an ongoing ban', () => {
    const account = movedAccount(7, [7])
    const wrapper = mount(ExcelBPS403Badge, { props: { account } })
    expect(wrapper.find(badge).exists()).toBe(false)
    const details = mount(ExcelBPS403Badge, { props: { account, details: true, groups: [{id: 7, name: 'BPS group'}] } })
    expect(details.text()).toContain('group=BPS group')
  })

  it('distinguishes a historical result from the current waiting cycle and shows safe identifiers', () => {
    const account = makeAccount({ extra: { openai_excel_bps_403_disabled_at: disabledAt },
      excel_bps_403_recovery: { state: 'pending', interval_minutes: 30, last_result_current_cycle: false,
        last_result: { status: 'failed', stage: 'request', reason: 'http_rejected', http_status: 403, duration_ms: 42, completed_at: movedAt,
          upstream_request_id: 'safe-request-id', error_code: 'account_deactivated', error_type: 'permission_error' } } })
    const wrapper = mount(ExcelBPS403Badge, { props: { account, details: true } })
    expect(wrapper.text()).toContain('excelBPS403State_pending')
    expect(wrapper.text()).toContain('excelBPS403HistoricalResult')
    expect(wrapper.text()).not.toContain('excelBPS403CurrentCycleResult')
    expect(wrapper.text()).toContain('code=account_deactivated')
    expect(wrapper.text()).toContain('requestId=safe-request-id')
  })

  it('does not treat legacy shutdown history or missing state as an active recovery', () => {
    const wrapper = mount(ExcelBPS403Badge, { props: { account: makeAccount({extra: {openai_excel_bps_403_disabled_at: disabledAt}}) } })
    expect(wrapper.find(badge).exists()).toBe(false)
  })
})
