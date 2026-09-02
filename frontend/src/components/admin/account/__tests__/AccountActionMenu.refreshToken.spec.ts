import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'test-account',
    platform: 'anthropic',
    type: 'oauth',
    proxy_id: null,
    concurrency: 3,
    priority: 50,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

const position = { top: 100, left: 100 }

function mountMenu(account: Account) {
  return mount(AccountActionMenu, {
    props: { show: true, account, position },
    attachTo: document.body,
  })
}

const bodyText = () => document.body.textContent ?? ''

// 后端下发通用刷新能力；Anthropic setup-token 还在前端按固定凭据语义早拒。
describe('AccountActionMenu refresh-token visibility', () => {
  it('shows refresh when the backend reports the account as refreshable', () => {
    const wrapper = mountMenu(makeAccount({ type: 'oauth', can_refresh_token: true }))
    expect(bodyText()).toContain('admin.accounts.refreshToken')
    wrapper.unmount()
  })

  // 直接导入的 setup-token 没有 refresh_token，后端下发 false。
  it('hides refresh but keeps re-authorize when the backend reports false', () => {
    const wrapper = mountMenu(
      makeAccount({
        type: 'setup-token',
        can_refresh_token: false,
        credentials: { token_type: 'Bearer', scope: 'user:inference' },
      })
    )
    expect(bodyText()).toContain('admin.accounts.reAuthorize')
    expect(bodyText()).not.toContain('admin.accounts.refreshToken')
    wrapper.unmount()
  })

  // 历史错误字段或旧后端返回 true 时，仍不得显示无效刷新入口。
  it('hides refresh for an Anthropic setup-token despite a stale true flag', () => {
    const wrapper = mountMenu(
      makeAccount({
        type: 'setup-token',
        can_refresh_token: true,
        credentials: { expires_at: '2026-12-01T00:00:00Z' },
        credentials_status: { has_refresh_token: true },
      })
    )
    expect(bodyText()).toContain('admin.accounts.reAuthorize')
    expect(bodyText()).not.toContain('admin.accounts.refreshToken')
    wrapper.unmount()
  })

  it('hides refresh for shadow accounts regardless of the backend flag', () => {
    const wrapper = mountMenu(
      makeAccount({ type: 'oauth', can_refresh_token: true, parent_account_id: 7 })
    )
    expect(bodyText()).not.toContain('admin.accounts.refreshToken')
    wrapper.unmount()
  })

  // 老版本后端不带该字段时不误藏入口。
  it('falls back to showing refresh when the field is absent', () => {
    const wrapper = mountMenu(makeAccount({ platform: 'openai', type: 'setup-token' }))
    expect(bodyText()).toContain('admin.accounts.refreshToken')
    wrapper.unmount()
  })
})
