import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { managementProvide } from './managementTestServices'
import AccountStatusIndicator from '../AccountStatusIndicator.vue'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '../../api/accountTypes'

vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? key + JSON.stringify(params) : key })
}))
enableAutoUnmount(afterEach)

const future = '2099-01-01T00:00:00Z'
const limit = { rate_limited_at: '2026-10-27T00:00:00Z', rate_limit_reset_at: future }
const bpsScopes = [
  'openai:excel_bps:429',
  'openai:excel_bps:429:model:Z3B0LTU',
  'openai:excel_bps:429:endpoint:L2FwaS9yZXNwb25zZXM',
  'openai:excel_bps:429:model:Z3B0LTU:endpoint:L2FwaS9yZXNwb25zZXM',
  'openai:excel_bps:future-private-scope'
]
function account(overrides: Partial<Account> = {}): Account {
  return { id: 1, name: 'local-fixture', platform: 'openai', type: 'oauth',
    proxy_id: null, concurrency: 1, priority: 1, status: 'active', schedulable: true,
    error_message: null, last_used_at: null, expires_at: null, auto_pause_on_expired: true,
    created_at: '2026-10-27T00:00:00Z', updated_at: '2026-10-27T00:00:00Z',
    rate_limited_at: null, rate_limit_reset_at: null, overload_until: null,
    temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null,
    ...overrides }
}
function menu(value: Account) {
  return mount(AccountActionMenu, {
    props: { show: true, account: value, anchorRect: new DOMRect(100, 100, 24, 24), canManage: true, canDiagnose: true },
    global: { provide: managementProvide(), stubs: { Teleport: true } }
  })
}

describe('BPS cooldown presentation and recovery isolation', () => {
  it.each(bpsScopes)('does not display the internal scope %s as a model or whole-account limit', scope => {
    const wrapper = mount(AccountStatusIndicator, { props: { account: account({ extra: {
      model_rate_limits: { [scope]: limit }, allow_overages: true
    } }) } })
    expect(wrapper.text()).not.toContain(scope)
    expect(wrapper.text()).toContain('admin.accounts.status.active')
    expect(wrapper.text()).not.toContain('admin.accounts.status.rateLimited')
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.status.bpsScope')
    expect(wrapper.html()).not.toContain('modelCreditOveragesUntil')
  })

  it.each(bpsScopes)('does not offer generic recovery for BPS-only cooldown %s', scope => {
    expect(menu(account({ extra: { model_rate_limits: { [scope]: limit } } })).text())
      .not.toContain('admin.accounts.recoverState')
  })

  it('keeps real native model status and recovery separate when BPS also cools down', () => {
    const value = account({ extra: { model_rate_limits: { [bpsScopes[0]]: limit, 'gpt-5': limit } } })
    const wrapper = mount(AccountStatusIndicator, { props: { account: value } })
    expect(wrapper.text()).toContain('gpt-5')
    expect(wrapper.text()).not.toContain(bpsScopes[0])
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(true)
    expect(menu(value).text()).toContain('admin.accounts.recoverState')
  })

  it.each([{ rate_limit_reset_at: future }, { status: 'error' as const }, { overload_until: future }])(
    'retains recovery for independent account state %j', overrides => {
      expect(menu(account({ ...overrides, extra: { model_rate_limits: { [bpsScopes[0]]: limit } } })).text())
        .toContain('admin.accounts.recoverState')
    }
  )

  it('does not turn expired or invalid cooldowns into a recovery action or active BPS badge', () => {
    const value = account({ extra: { model_rate_limits: {
      [bpsScopes[0]]: { ...limit, rate_limit_reset_at: '2000-01-01T00:00:00Z' },
      [bpsScopes[1]]: { ...limit, rate_limit_reset_at: 'invalid' },
      'gpt-invalid': { ...limit, rate_limit_reset_at: 'invalid' }
    } } })
    const wrapper = mount(AccountStatusIndicator, { props: { account: value } })
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('gpt-invalid')
    expect(menu(value).text()).not.toContain('admin.accounts.recoverState')
  })

  it('updates the scoped status when an authoritative account refresh removes the cooldown', async () => {
    const wrapper = mount(AccountStatusIndicator, { props: { account: account({ extra: { model_rate_limits: { [bpsScopes[0]]: limit } } }) } })
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(true)
    await wrapper.setProps({ account: account() })
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(false)
  })

  it('shows the actual BPS model/endpoint intersection and server deadline without exposing its key', () => {
    const scope = 'openai:excel_bps:429:model:Z3B0LTU:endpoint:L2FwaS9yZXNwb25zZXM'
    const wrapper = mount(AccountStatusIndicator, { props: { account: account({ extra: { model_rate_limits: { [scope]: limit } } }) } })
    const badge = wrapper.get('[data-testid="bps-cooldown-badge"]')
    expect(badge.text()).toContain('bpsScopeModelEndpoint')
    expect(badge.text()).toContain('gpt-5')
    expect(badge.text()).toContain('/api/responses')
    expect(badge.attributes('title')).toContain('bpsCooldownUntil')
    expect(wrapper.html()).not.toContain(scope)
  })

  it('keeps a BPS model-permission restriction distinct from 429 and native model limits', () => {
    const scope = 'openai:excel_bps:model_permission:Z3B0LTU'
    const wrapper = mount(AccountStatusIndicator, { props: { account: account({ extra: { model_rate_limits: { [scope]: limit } } }) } })
    expect(wrapper.get('[data-testid="bps-cooldown-badge"]').text()).toContain('bpsModelPermission')
    expect(wrapper.text()).toContain('gpt-5')
    expect(wrapper.text()).not.toContain('admin.accounts.status.rateLimited')
    expect(wrapper.html()).not.toContain(scope)
  })

  it('does not display BPS metadata as active on a non-BPS account type', () => {
    const wrapper = mount(AccountStatusIndicator, { props: { account: account({ type: 'apikey', extra: { model_rate_limits: { [bpsScopes[0]]: limit } } }) } })
    expect(wrapper.find('[data-testid="bps-cooldown-badge"]').exists()).toBe(false)
  })

  it('does not widen the recovery command permission', () => {
    const wrapper = menu(account({ rate_limit_reset_at: future }))
    return wrapper.setProps({ canManage: false }).then(() => {
      expect(wrapper.text()).not.toContain('admin.accounts.recoverState')
    })
  })
})
