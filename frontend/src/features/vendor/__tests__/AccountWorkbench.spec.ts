import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import VendorManagement from '../components/VendorManagement.vue'
import VendorEditor from '../components/VendorEditor.vue'
import nativeEn from '@/i18n/locales/en'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { it, expect, vi, beforeEach } from 'vitest'
import AccountWorkbench from '../components/AccountWorkbench.vue'
import AccountForm from '../components/AccountForm.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import AccountImport from '../components/AccountImport.vue'
import UsageWorkbench from '../components/UsageWorkbench.vue'
import type { ResourceScope } from '../types'
import en from '@/i18n/locales/en/vendor'
import zh from '@/i18n/locales/zh/vendor'
import ja from '@/i18n/locales/ja/vendor'
import zhTW from '@/i18n/locales/zh-TW/vendor'
const api = vi.hoisted(() => ({
  accounts: vi.fn(),
  account: vi.fn(),
  proxies: vi.fn(),
  accountAction: vi.fn(),
  deleteAccount: vi.fn(),
  updateAccount: vi.fn(),
  revokeAuthorization: vi.fn(),
  exportAccounts: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  inventory: vi.fn(),
  scope: vi.fn()
}))
const native = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), groups: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: native }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAll: native.groups }, accounts: { bulkUpdate: vi.fn() } } }))
const copyToClipboard = vi.hoisted(() => vi.fn().mockResolvedValue(true))
const download = vi.hoisted(() => vi.fn())
vi.mock('../helpers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../helpers')>()),
  download
}))
vi.mock('../api', () => ({
  resourceAPI: (scope: ResourceScope) => {
    api.scope(scope)
    return api
  },
  vendorAPI: api,
  vendorManagementAPI: () => api
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
const account = {
  id: 12,
  name: 'Owned account',
  vendor_id: 8,
  platform: 'anthropic',
  groups: [],
  concurrency: 3,
  priority: 50,
  quota: {},
  status: 'active',
  archived: false,
  archived_at: null,
  today_usage: { cost_usd: '1.25' },
  all_time_usage: { requests: 37, total_tokens: 4200, cost_usd: '10.5', cache_hit_rate: 0.25 }
}
async function openDetails(
  locale: string,
  messages: typeof en,
  nativeDialog = false,
  scope: ResourceScope = { admin: true, vendorId: 8 }
) {
  const w = mount(AccountWorkbench, {
    ...(nativeDialog ? { attachTo: document.body } : {}),
    props: {
      scope,
      vendor: { id: 8, account_limit: 100, groups: [] } as any
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale,
          messages: { [locale]: { vendor: messages } }
        })
      ],
      stubs: {
        teleport: true,
        Pagination: true,
        GroupBadge: true,
        TotpStepUpDialog: true,
        AccountForm: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        AccountImport: true,
        UsageWorkbench: true,
        BaseDialog: nativeDialog ? false : {
          name: 'BaseDialog',
          template: '<section class="dialog" role="dialog"><slot /><slot name="footer" /></section>'
        }
      }
    }
  })
  await flushPromises()
  await w
    .findAll('button')
    .find((b) => b.text() === messages.details)!
    .trigger('click')
  await flushPromises()
  return w
}
beforeEach(() => {
  vi.clearAllMocks()
  native.get.mockImplementation(async (url: string) => ({ data: url.startsWith('/admin/vendors/') ? { id: 8, group_ids: [], groups: [] } : { ...account, id: 12, vendor_id: 8, credentials: {}, extra: {} } }))
  native.put.mockResolvedValue({ data: { ...account, id: 12 } })
  native.groups.mockResolvedValue([])
  api.proxies.mockResolvedValue({ items: [], total: 0 })
  api.account.mockResolvedValue(account)
  api.accountAction.mockResolvedValue({ status: 'success' })
  api.deleteAccount.mockResolvedValue({ archived: true })
  api.updateAccount.mockResolvedValue({})
  api.revokeAuthorization.mockResolvedValue({})
  api.accounts.mockResolvedValue({
    items: [account],
    total: 1,
    page: 1,
    page_size: 20,
    overview: {
      total: 12,
      available: 9,
      paused: 2,
      cooling: 1,
      disabled: 0,
      high_quota: 2,
      abnormal: 1,
      quota_near_limit_percent: 90,
      quota_full_percent: 99,
      quota_distribution: {
        five_hour: { normal: 8, near_limit: 2, full: 1, unknown: 1 }
      },
      failures: { authentication: 1 }
    }
  })
})
const dualAccount = { ...account, platform: 'openai', type: 'oauth', vendor_id: 8,
  authorization_modes: ['native', 'bps'], bps_models: null,
  authorizations: [
    { mode: 'native', authorized: true, enabled: true, status: 'active', can_refresh: true },
    { mode: 'bps', authorized: true, enabled: true, status: 'active', can_refresh: true }
  ] }
async function openOpenAI(scope: ResourceScope = { admin: false }) {
  api.account.mockResolvedValue(dualAccount)
  api.accounts.mockResolvedValue({ items: [dualAccount], total: 1, overview: { total: 1 } })
  const w = await openDetails('en', en, false, scope)
  await w.setProps({ vendor: { id: 8, platforms: ['openai'], openai_authorization_modes: ['native', 'bps'], groups: [] } as any })
  return w
}
it('shows one dual-grant account and filters by the selected mode, clearing it for Claude', async () => {
  const w = await openOpenAI()
  expect(w.get('tbody').text()).toContain(en.authorization.dual)
  expect(w.get('.vendor-accounts-table').findAll('tbody tr')).toHaveLength(1)
  w.findAllComponents(Select).find(s => s.props('ariaLabel') === en.authorization.mode)!.vm.$emit('update:modelValue', 'bps')
  await flushPromises()
  expect(api.accounts.mock.lastCall?.[0]).toMatchObject({ authorization_mode: 'bps', page: 1 })
  w.findAllComponents(Select).find(s => s.props('ariaLabel') === en.platform)!.vm.$emit('update:modelValue', 'anthropic')
  await flushPromises()
  expect(api.accounts.mock.lastCall?.[0]).toMatchObject({ platform: 'anthropic', authorization_mode: '' })
  w.unmount()
})
it('opens the selected authorization form above the detail drawer and returns after save', async () => {
  const w = await openOpenAI()
  await w.get('[data-testid=authorize-bps]').trigger('click')
  const form = w.getComponent(AccountForm)
  expect(form.props()).toMatchObject({ reauthorize: true, authorizationMode: 'bps', account: { id: 12 } })
  expect(w.find('[data-detail-section=authorizations]').exists()).toBe(false)
  form.vm.$emit('saved')
  await flushPromises()
  expect(w.findComponent(AccountForm).exists()).toBe(false)
  expect(w.find('[data-detail-section=authorizations]').exists()).toBe(true)
  expect(api.account).toHaveBeenCalledTimes(2)
  w.unmount()
})
it('uses a dedicated scoped grant revocation after confirmation and retains the account', async () => {
  const w = await openOpenAI({ admin: true, vendorId: 8 })
  await w.get('[data-testid=revoke-bps]').trigger('click')
  expect(api.revokeAuthorization).not.toHaveBeenCalled()
  expect(w.get('[data-testid=confirm-revoke-authorization]').exists()).toBe(true)
  api.revokeAuthorization.mockRejectedValueOnce(new Error('Grant changed; reload'))
  await w.get('[data-testid=confirm-revoke-authorization]').trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Grant changed; reload')
  expect(w.find('[data-testid=confirm-revoke-authorization]').exists()).toBe(true)
  await w.get('[data-testid=confirm-revoke-authorization]').trigger('click')
  await flushPromises()
  expect(api.revokeAuthorization).toHaveBeenLastCalledWith(12, 'bps')
  expect(api.scope).toHaveBeenLastCalledWith({ admin: true, vendorId: 8 })
  expect(api.deleteAccount).not.toHaveBeenCalled()
  expect(w.find('[data-testid=confirm-revoke-authorization]').exists()).toBe(false)
  expect(w.find('[data-detail-section=authorizations]').exists()).toBe(true)
  w.unmount()
})
it('refreshes only the selected authorization mode', async () => {
  const w = await openOpenAI()
  await w.get('[data-testid=refresh-bps]').trigger('click')
  await flushPromises()
  expect(api.accountAction).toHaveBeenCalledWith(12, 'refresh', 'bps')
  expect(api.account).toHaveBeenCalledTimes(2)
  expect(api.updateAccount).not.toHaveBeenCalled()
  w.unmount()
})
it.each([{ admin: false, readOnly: true, vendorId: 8 }, { admin: true, readOnly: true, vendorId: 0 }])('keeps per-mode status visible but hides mutations in read-only scope %j', async scope => {
  const w = await openOpenAI(scope)
  expect(w.get('[data-detail-section=authorizations]').text()).toContain(en.authorization.status.active)
  expect(w.find('[data-testid=authorize-bps]').exists()).toBe(false)
  const vm = w.vm as unknown as { changeAuthorization: (patch: object) => Promise<void> }
  await vm.changeAuthorization({ bps_enabled: false })
  expect(api.updateAccount).not.toHaveBeenCalled()
  expect(api.revokeAuthorization).not.toHaveBeenCalled()
  w.unmount()
})

it.each([
  { admin: false, children: true, readOnly: true },
  { admin: true, vendorId: 0, readOnly: true }
])('shows account ownership and opens details in the row owner scope %j', async scope => {
  api.accounts.mockResolvedValue({ items: [{ ...account, vendor_id: 23, vendor_name: 'Owner twenty-three' }], total: 1 })
  const w = await openDetails('en', en, false, scope)
  expect(w.get('[data-resource-owner]').text()).toContain('Owner twenty-three')
  expect(w.get('[data-resource-owner]').text()).toContain('#23')
  expect(api.scope).toHaveBeenLastCalledWith({ admin: scope.admin, readOnly: true, vendorId: 23 })
  expect(w.find('button[aria-label="' + en.addAccount + '"]').exists()).toBe(false)
  await w.get('[data-resource-owner] button').trigger('click')
  expect(w.emitted('selectVendor')).toEqual([[23]])
  w.unmount()
})
it.each([
  ['en', en],
  ['zh', zh],
  ['ja', ja],
  ['zh-TW', zhTW]
] as const)(
  'archives the submitted account after confirmation closes and retains read-only history in %s',
  async (locale, messages) => {
    let finish!: (result: { archived: true }) => void
    api.deleteAccount.mockImplementationOnce(
      () =>
        new Promise<{ archived: true }>((resolve) => {
          finish = resolve
        })
    )
    const w = await openDetails(locale, messages)
    await w.get('tbody input[type=checkbox]').setValue(true)
    await w.getComponent(BaseDialog).get('button[aria-label="' + messages.moreActions + '"]').trigger('click')
    await w
      .findAll('button')
      .find((b) => b.text() === messages.delete)!
      .trigger('click')
    expect(w.text()).toContain(messages.deleteAccountWarning)
    await w
      .findAll('button')
      .find((b) => b.text() === messages.confirm)!
      .trigger('click')
    expect(api.deleteAccount).toHaveBeenCalledWith(12)
    const confirmation = w
      .findAllComponents(BaseDialog)
      .find((d) => d.text().includes(messages.deleteAccountWarning))!
    confirmation.vm.$emit('close')
    await flushPromises()
    expect(w.text()).not.toContain(messages.deleteAccountWarning)
    const archived = {
      ...account,
      archived: true,
      archived_at: '2026-10-17T02:34:56Z',
      vendor_paused_at: '2026-10-17T02:34:56Z',
      schedulable: false,
      blockers: ['archived']
    }
    api.account.mockResolvedValue(archived)
    api.accounts.mockResolvedValue({
      items: [archived],
      total: 1,
      page: 1,
      page_size: 20
    })
    finish({ archived: true })
    await flushPromises()
    expect(api.accounts).toHaveBeenCalledTimes(2)
    expect(w.find('[role=alert]').exists()).toBe(false)
    expect(w.findAllComponents(BaseDialog)).toHaveLength(0)
    expect(w.text()).toContain(messages.selected.replace('{count}', '0'))
    const row = w.get('tbody tr')
    expect(row.get('.badge-gray').text()).toBe(messages.archived)
    expect(row.get('time[datetime]').attributes('datetime')).toBe(archived.archived_at)
    expect(row.getComponent(StatusBadge).props()).toMatchObject({
      status: 'active',
      label: messages.active
    })
    expect(row.text()).toContain('$1.25')
    expect(row.text()).toContain('$10.50')
    expect(row.find('input[type=checkbox]').exists()).toBe(false)
    expect(w.get('thead input[type=checkbox]').attributes('disabled')).toBeDefined()
    expect(row.findAll('button').map((b) => b.text())).toEqual([
      account.name,
      messages.unknown + '#8',
      messages.details,
      messages.usage
    ])
    await row.get('button').trigger('click')
    await flushPromises()
    const detail = w.getComponent(BaseDialog)
    expect(detail.get('.badge-gray').text()).toBe(messages.archived)
    expect(detail.get('[data-detail-field=archivedAt]').attributes('title')).toBe(archived.archived_at)
    expect(detail.get('[data-detail-field=status]').text()).toBe(messages.active)
    expect(detail.get('[data-detail-section=blockers]').text()).toBe(
      messages.accountBlockers.archived
    )
    const history = detail.get('[data-usage-window=all_time_usage]')
    expect(history.get('[data-metric=requests]').text()).toContain('37')
    expect(history.get('[data-metric=total_tokens]').text()).toContain('4,200')
    expect(history.get('[data-metric=cost_usd]').text()).toContain('$10.50')
    expect(detail.findAll('button').map(b => b.text())).toEqual([messages.viewUsage])
    detail.vm.$emit('close')
    await flushPromises()
    await row.get('button[aria-label="' + messages.usage + ' ' + account.name + '"]').trigger('click')
    expect(w.getComponent(UsageWorkbench).props()).toMatchObject({
      accountId: 12,
      admin: true,
      vendorId: 8
    })
    expect(api.accountAction).not.toHaveBeenCalled()
    expect(api.deleteAccount).toHaveBeenCalledTimes(1)
    expect(api.exportAccounts).not.toHaveBeenCalled()
    w.unmount()
  }
)

it('excludes archived accounts from page selection and clears newly archived selections during export confirmation', async () => {
  const archived = {
    ...account,
    id: 13,
    name: 'Archived account',
    archived: true,
    archived_at: '2026-10-17T02:34:56Z'
  }
  api.accounts.mockResolvedValueOnce({ items: [account, archived], total: 2 })
  const w = await openDetails('en', en)
  w.getComponent(BaseDialog).vm.$emit('close')
  await flushPromises()
  await w.get('thead input[type=checkbox]').setValue(true)
  expect(w.text()).toContain(en.selected.replace('{count}', '1'))
  expect(w.findAll('tbody input[type=checkbox]')).toHaveLength(1)
  expect((w.get('thead input[type=checkbox]').element as HTMLInputElement).checked).toBe(true)

  let finish!: (page: unknown) => void
  api.accounts.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      })
  )
  await w
    .findAll('button')
    .find((b) => b.text() === en.refresh)!
    .trigger('click')
  await w
    .findAll('button')
    .find((b) => b.text() === en.moreActions)!
    .trigger('click')
  await w.get('button[aria-label="' + en.export + '"]').trigger('click')
  expect(w.getComponent(BaseDialog).text()).toContain(en.exportWarning)
  finish({
    items: [{ ...account, archived: true, archived_at: archived.archived_at }, archived],
    total: 2
  })
  await flushPromises()
  const confirmation = w.getComponent(BaseDialog)
  expect(confirmation.text()).toContain(en.selected.replace('{count}', '0'))
  const confirm = confirmation.findAll('button').find((b) => b.text() === en.confirm)!
  expect(confirm.attributes('disabled')).toBeDefined()
  await confirm.trigger('click')
  expect(api.exportAccounts).not.toHaveBeenCalled()
  expect(w.get('tbody').findAll('.badge-gray')).toHaveLength(2)
  w.unmount()
})

it('does not keep an old supplier delete confirmation or import open after scope changes', async () => {
  const w = await openDetails('en', en)
  await w.getComponent(BaseDialog).get('button[aria-label="' + en.moreActions + '"]').trigger('click')
  await w
    .findAll('button')
    .find((b) => b.text() === en.delete)!
    .trigger('click')
  await w
    .findAll('button')
    .find((b) => b.text() === en.moreActions)!
    .trigger('click')
  await flushPromises()
  await w
    .findAll('button')
    .find((b) => b.text() === en.import)!
    .trigger('click')
  expect(w.findComponent(AccountImport).exists()).toBe(true)
  let finishDetail!: (value: unknown) => void
  api.account.mockImplementationOnce(() => new Promise((resolve) => { finishDetail = resolve }))
  await w.get('button[aria-label="' + en.details + ' ' + account.name + '"]').trigger('click')
  await w.setProps({
    scope: { admin: true, vendorId: 9 },
    vendor: { id: 9, account_limit: 100, groups: [] } as any
  })
  await flushPromises()
  finishDetail({ ...account, name: 'Previous supplier detail' })
  await flushPromises()
  expect(w.findComponent(AccountImport).exists()).toBe(false)
  expect(w.text()).not.toContain(en.deleteAccountWarning)
  expect(w.findComponent(BaseDialog).exists()).toBe(false)
  expect(w.text()).not.toContain('Previous supplier detail')
  expect(api.deleteAccount).not.toHaveBeenCalled()
  w.unmount()
})

it.each([
  ['en', en],
  ['zh', zh],
  ['ja', ja],
  ['zh-TW', zhTW]
] as const)(
  'renders the safe account detail projection and localizes its states in %s',
  async (locale, messages) => {
    const metrics = {
      requests: 2,
      successful_requests: 1,
      failed_requests: 1,
      input_tokens: 1200,
      output_tokens: 1022,
      total_tokens: 2222,
      cache_creation_tokens: 111,
      cache_read_tokens: 1000,
      cache_tokens: 1111,
      cost_usd: '0.73',
      cache_hit_rate: 0.4,
      success_rate: 0.5,
      avg_duration_ms: 123,
      avg_first_token_ms: 45
    }
    const codes = Object.keys(en.accountBlockers)
    api.account.mockResolvedValue({
      ...account,
      vendor_id: 8,
      vendor_name: 'Supplier Eight',
      email: 'verified@example.test',
      method: 'oauth',
      schedulable: false,
      groups: [
        { id: 2, name: 'Default group' },
        { id: 3, name: 'Coding group' }
      ],
      current_concurrency: 2,
      rpm: 2,
      tpm: 2222,
      tpm_with_cache: 3333,
      recent_minute: metrics,
      today_usage: {
        ...metrics,
        requests: 10,
        successful_requests: 9,
        failed_requests: 1,
        success_rate: 0.9
      },
      all_time_usage: {
        ...metrics,
        requests: 300,
        successful_requests: 200,
        failed_requests: 100,
        success_rate: 2 / 3
      },
      blockers: [...codes, 'vendor_paused'],
      rate_limit_reset_at: '2099-10-15T01:00:00Z',
      overload_until: '2099-10-15T02:00:00Z',
      temp_unschedulable_until: '2099-10-15T03:00:00Z',
      vendor_paused_at: '2026-10-15T00:00:00Z',
      model_limits: [{ scope: 'claude-fable', resets_at: '2099-10-15T04:00:00Z' }],
      plan_type: 'Team',
      subscription_expires_at: '2099-11-15T01:00:00Z',
      authorization_expires_at: '2099-11-16T01:00:00Z',
      expires_at: '2099-12-01T00:00:00Z',
      can_refresh: true,
      proxy_id: 6,
      proxy: {
        id: 6,
        name: 'Supplier proxy',
        protocol: 'https',
        endpoint: 'https://proxy.example.test:8443',
        country_code: 'TW',
        city: 'Taipei',
        region_source: 'label'
      },
      quota: {
        source: 'passive',
        five_hour: { utilization: 12, resets_at: '2099-10-15T05:00:00Z' }
      }
    })
    const w = await openDetails(locale, messages)
    const dialog = w.find('.dialog')
    expect(dialog.findAll('template')).toHaveLength(0)
    const field = (key: string) => dialog.find('[data-detail-field="' + key + '"]')
    expect(field('vendor').text()).toBe('Supplier Eight')
    expect(field('accountSource').exists()).toBe(false)
    expect(field('accountWeight').exists()).toBe(false)
    expect(field('priority').text()).toBe('50')
    expect(dialog.get('[data-detail-section=basic]').text()).toContain('#8')
    expect(dialog.findAll('section[data-detail-section]').map((s) => s.attributes('data-detail-section')))
      .toEqual(['status', 'basic', 'runtime', 'quota', 'usage-summary', 'cooldown', 'limits'])
    expect(field('verifiedEmail').text()).toBe('verified@example.test')
    expect(field('platform').text()).toBe('Anthropic')
    expect(field('status').text()).toBe(messages.active)
    expect(field('scheduling').text()).toBe(messages.notSchedulable)
    expect(field('concurrency').text()).toBe('2 / 3')
    expect(field('rpm').text()).toBe('2')
    expect(field('tpm').text()).toBe('2222')
    expect(field('tpm_with_cache').text()).toBe('3333')
    expect(dialog.get('[data-success-window=today_usage]').text()).toContain('90.0%')
    expect(dialog.get('[data-success-window=today_usage]').text()).toContain('9 / 10')
    expect(field('planType').text()).toBe('Team')
    expect(field('refreshAvailable').text()).toBe(messages.yes)
    expect(field('overloadUntil').attributes('title')).toBe('2099-10-15T02:00:00Z')
    expect(field('tempUnschedulableUntil').attributes('title')).toBe('2099-10-15T03:00:00Z')
    expect(field('subscriptionExpiresAt').attributes('title')).toBe('2099-11-15T01:00:00Z')
    expect(field('authorizationExpiresAt').attributes('title')).toBe('2099-11-16T01:00:00Z')
    expect(dialog.find('[data-detail-section=groups]').text()).toContain('Coding group · #3')
    expect(dialog.find('[data-detail-section=proxy]').text()).toContain(
      'https://proxy.example.test:8443'
    )
    expect(dialog.find('[data-detail-section=proxy]').text()).toContain('TW · Taipei')
    expect(dialog.find('[data-detail-section=proxy]').text()).toContain(
      messages.regionSources.label
    )
    await dialog.get('[data-copy-proxy]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('https://proxy.example.test:8443')
    expect(dialog.find('[data-detail-section=limits] tbody').text()).toContain('claude-fable')
    expect(dialog.text()).toContain('12%')
    const blockers = dialog.find('[data-detail-section=blockers]')
    expect(blockers.findAll('p')).toHaveLength(codes.length)
    for (const code of codes) {
      expect(blockers.text()).toContain(
        messages.accountBlockers[code as keyof typeof messages.accountBlockers]
      )
      if (code.includes('_')) expect(blockers.text()).not.toContain(code)
    }
    for (const [window, requests] of [['today_usage', '10'], ['all_time_usage', '300']]) {
      const section = dialog.get('[data-usage-window="' + window + '"]')
      expect(section.get('[data-metric=requests]').text()).toBe(requests)
      expect(section.get('[data-metric=total_tokens]').text()).toBe('2,222')
      expect(section.get('[data-metric=cost_usd]').text()).toBe('$0.73')
    }
    expect(dialog.get('[data-success-window=all_time_usage]').text()).toContain('66.7%')
    expect(dialog.get('[data-success-window=all_time_usage]').text()).toContain('200 / 300')
    expect(dialog.findComponent({ name: 'UsageMetrics' }).exists()).toBe(false)
    w.unmount()
  }
)
it('opens info in a full-height right drawer and restores focus after native close', async () => {
  const w = await openDetails('en', en, true)
  try {
    const opener = w.get('button[aria-label="' + en.details + ' ' + account.name + '"]')
    const dialog = w.getComponent(BaseDialog)
    expect(dialog.get('.modal-content').classes()).toEqual(expect.arrayContaining([
      'account-detail-drawer', '!fixed', 'right-0', 'inset-y-0', '!h-full', '!max-h-none',
      'max-w-lg', '!max-w-2xl'
    ]))
    expect(dialog.get('.modal-body').classes()).toContain('min-h-0')
    const label = dialog.get('[role=dialog]').attributes('aria-labelledby')
    expect(dialog.get('#' + label).text()).toContain(account.name)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(dialog.element.contains(document.activeElement)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(w.findComponent(BaseDialog).exists()).toBe(false)
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(opener.element)
    await opener.trigger('click')
    await flushPromises()
    await w.get('[role=dialog]').trigger('click')
    await flushPromises()
    expect(w.findComponent(BaseDialog).exists()).toBe(false)
    expect(document.activeElement).toBe(opener.element)
    await opener.trigger('click')
    await flushPromises()
    await w.get('.modal-header button').trigger('click')
    await flushPromises()
    expect(w.findComponent(BaseDialog).exists()).toBe(false)
    expect(document.activeElement).toBe(opener.element)
  } finally {
    w.unmount()
  }
})
it.each([
  ['https://proxy-user:proxy-password@proxy.example.test:8443/private-path?token=query-secret#fragment-secret', 'https://proxy.example.test:8443'],
  ['socks5://proxy-user:proxy-password@proxy.example.test:1080', 'socks5://proxy.example.test:1080'],
  ['socks5h://proxy-user:proxy-password@proxy.example.test:1080', 'socks5h://proxy.example.test:1080'],
  ['javascript:query-secret', ''],
  ['malformed endpoint', '']
])('copies only a safe proxy endpoint from an archived detail: %s', async (endpoint, safe) => {
  api.account.mockResolvedValue({
    ...account,
    archived: true,
    proxy_id: 6,
    proxy: { endpoint, username: 'proxy-user', password: 'proxy-password' },
    credentials: { access_token: 'downstream-secret' }
  })
  const w = await openDetails('en', en)
  const dialog = w.getComponent(BaseDialog)
  if (safe) {
    await dialog.get('[data-copy-proxy]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledTimes(1)
    expect(copyToClipboard).toHaveBeenCalledWith(safe)
    expect(dialog.findAll('button').map(b => b.text())).toEqual(['', en.viewUsage])
  } else {
    expect(dialog.get('[data-detail-section=proxy]').text()).toContain(en.unknown)
    expect(dialog.findAll('button').map(b => b.text())).toEqual([en.viewUsage])
    expect(copyToClipboard).not.toHaveBeenCalled()
  }
  for (const secret of ['proxy-user', 'proxy-password', 'private-path', 'query-secret', 'fragment-secret', 'downstream-secret']) {
    expect(dialog.html()).not.toContain(secret)
  }
  expect(api.accountAction).not.toHaveBeenCalled()
  expect(api.exportAccounts).not.toHaveBeenCalled()
  w.unmount()
})
it('keeps missing outcomes and metadata unknown, and distinguishes returned zeroes and empty limits', async () => {
  api.account.mockResolvedValue({
    ...account,
    groups: undefined,
    blockers: undefined,
    model_limits: undefined,
    today_usage: null,
    all_time_usage: {
      requests: 0,
      successful_requests: null,
      failed_requests: null,
      success_rate: null,
      cost_usd: '0'
    },
    recent_minute: null,
    rpm: null,
    tpm: 0,
    tpm_with_cache: 0,
    current_concurrency: null,
    can_refresh: false
  })
  const w = await openDetails('en', en)
  let dialog = w.find('.dialog')
  for (const key of [
    'vendor',
    'verifiedEmail',
    'planType',
    'rpm'
  ])
    expect(dialog.find('[data-detail-field="' + key + '"]').text()).toBe(en.unknown)
  expect(dialog.find('[data-detail-field=tpm]').text()).toBe('0')
  expect(dialog.find('[data-detail-field=refreshAvailable]').text()).toBe(en.no)
  expect(dialog.find('[data-detail-section=blockers]').text()).toContain(en.unknown)
  expect(dialog.find('[data-detail-section=limits]').text()).not.toContain(en.noModelLimits)
  expect(dialog.get('[data-detail-section=cooldown]').text()).toContain(en.notReported)
  expect(dialog.find('[data-success-window]').exists()).toBe(false)
  expect(dialog.get('[data-usage-window=all_time_usage] [data-metric=requests]').text()).toBe('0')
  expect(dialog.get('[data-usage-window=today_usage] [data-metric=requests]').text()).toBe('—')
  expect(dialog.get('[data-usage-window=all_time_usage] [data-metric=cost_usd]').text()).toBe('$0.00')
  expect(dialog.text()).toContain(en.costNotice)
  api.account.mockResolvedValue({
    ...account,
    proxy_id: null,
    blockers: [],
    model_limits: [],
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    groups: []
  })
  await w
    .findAll('button')
    .find((b) => b.text() === en.details)!
    .trigger('click')
  await flushPromises()
  dialog = w.find('.dialog')
  expect(dialog.find('[data-detail-section=proxy]').text()).toContain(en.direct)
  expect(dialog.get('[data-detail-section=cooldown]').text()).toContain(en.noActiveCooldown)
  expect(dialog.find('[data-detail-section=limits]').text()).toContain(en.noModelLimits)
  expect(dialog.find('[data-detail-section=blockers]').text()).toContain(en.noBlockers)
  w.unmount()
})
it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
  'keeps server pool-wide totals and clears selection in scope %j',
  async (scope) => {
    const w = mount(AccountWorkbench, {
      props: {
        scope,
        vendor: { account_limit: 100, groups: [] } as any
      },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: { en: { vendor: en } }
          })
        ],
        stubs: { Pagination: true, GroupBadge: true, TotpStepUpDialog: true }
      }
    })
    await flushPromises()
    expect(w.element.querySelectorAll('template')).toHaveLength(0)
    const actions = w.findAll('button').map((b) => b.text())
    expect(actions).toEqual(expect.arrayContaining([en.addAccount, en.moreActions]))
    expect(w.text()).toContain(en.quotaLoad)
    expect(w.text()).toContain('≥ 90%')
    expect(w.findComponent(StatusBadge).props('label')).toBe(en.active)
    expect(w.find('table').text()).toContain('25.0%')
    await w.find('tbody input[type=checkbox]').setValue(true)
    expect(w.text()).toContain('Selected: 1')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Authentication 1')!
      .trigger('click')
    await flushPromises()
    expect(api.accounts).toHaveBeenLastCalledWith(
      expect.objectContaining({ failure_category: 'authentication', page: 1 }),
      expect.any(AbortSignal)
    )
    expect(w.text()).toContain('Selected: 0')
    expect(w.text()).toMatch(/Total accounts\s*12/)
    w.unmount()
  }
)
it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
  'renders and opens account controls outside inert template DOM in scope %j',
  async (scope) => {
    const w = mount(AccountWorkbench, {
      attachTo: document.body,
      props: { scope, vendor: { account_limit: 100, groups: [] } as any },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: { en: { vendor: en } }
          })
        ],
        stubs: {
          teleport: true,
          Pagination: true,
          GroupBadge: true,
          TotpStepUpDialog: true,
          AccountForm: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
          AccountImport: true,
          UsageWorkbench: true,
          BaseDialog: {
            name: 'BaseDialog',
            props: ['title'],
            template:
              '<section class="dialog" :data-title="title" role="dialog"><slot /><slot name="footer" /></section>'
          }
        }
      }
    })
    await flushPromises()
    const click = async (label: string, container = w) => {
      // Vue test-utils can find buttons inside an inert native template; text
      // presence alone does not establish that the browser renders the action.
      expect(w.element.querySelectorAll('template')).toHaveLength(0)
      const control = container
        .findAll('button')
        .find(
          (b) =>
            b.attributes('aria-label') === label ||
            b.attributes('aria-label')?.startsWith(label + ' ') ||
            b.text() === label
        )!
      expect(control, label).toBeDefined()
      expect(control.element.isConnected).toBe(true)
      expect(control.element.closest('template')).toBeNull()
      expect(control.isVisible()).toBe(true)
      await control.trigger('click')
      await flushPromises()
    }
    await click(en.refresh)
    await click(en.addAccount)
    expect(w.getComponent(AccountForm).props('account')).toBeUndefined()
    w.getComponent(AccountForm).vm.$emit('close')
    await flushPromises()
    await click(en.moreActions)
    await click(en.import)
    expect(w.getComponent(AccountImport).props('scope')).toEqual(scope)
    w.getComponent(AccountImport).vm.$emit('close')
    await flushPromises()
    await w.find('tbody input[type=checkbox]').setValue(true)
    await click(en.moreActions)
    await click(en.export)
    expect(w.find('.dialog').text()).toContain(en.exportWarning)
    w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await flushPromises()
    await click(en.edit)
    if (scope.admin) {
      expect(w.getComponent(EditAccountModal).props('account').id).toBe(12)
      expect(api.proxies).toHaveBeenCalledWith({ page: 1, page_size: 50 })
      expect(native.get).toHaveBeenCalledWith('/admin/accounts/12', { params: { vendor_id: 8, view: 'management' } })
      await w.getComponent(EditAccountModal).props('saveAccount')(12, { concurrency: 23 })
      expect(native.put).toHaveBeenCalledWith('/admin/accounts/12', { concurrency: 23 }, { params: { vendor_id: 8, view: 'management' } })
      w.getComponent(EditAccountModal).vm.$emit('close')
    } else {
      expect(w.getComponent(AccountForm).props('account').id).toBe(12)
      w.getComponent(AccountForm).vm.$emit('close')
    }
    await flushPromises()
    await click(en.usage)
    expect(w.getComponent(UsageWorkbench).props()).toEqual(
      expect.objectContaining({
        accountId: 12,
        admin: scope.admin,
        vendorId: 8
      })
    )
    w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await flushPromises()
    api.accounts.mockResolvedValueOnce({
      items: [{ ...account, vendor_paused_at: '2026-10-15T00:00:00Z' }],
      total: 1
    })
    await click(en.pause)
    expect(api.accountAction).toHaveBeenLastCalledWith(12, 'pause')
    await click(en.resume)
    expect(api.accountAction).toHaveBeenLastCalledWith(12, 'resume')
    await click(en.details)
    expect(api.account).toHaveBeenCalledWith(12)
    const detail = w.findComponent({ name: 'BaseDialog' })
    await detail.get('button[aria-label="' + en.moreActions + '"]').trigger('click')
    expect(detail.findAll('button').map((b) => b.text())).toEqual(
      expect.arrayContaining([
        en.reauthorize,
        en.refreshToken,
        en.refreshQuota,
        en.probe,
        en.sync,
        en.delete
      ])
    )
    await click(en.reauthorize)
    expect(w.getComponent(AccountForm).props('reauthorize')).toBe(true)
    w.getComponent(AccountForm).vm.$emit('close')
    await flushPromises()
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    for (const [label, operation] of [
      [en.refreshToken, 'refresh'],
      [en.refreshQuota, 'usage-refresh'],
      [en.probe, 'probe'],
      [en.sync, 'sync']
    ]) {
      await w.getComponent(BaseDialog).get('button[aria-label="' + en.moreActions + '"]').trigger('click')
      await click(label)
      expect(api.accountAction).toHaveBeenLastCalledWith(12, operation)
    }
    confirm.mockRestore()
    await w.getComponent(BaseDialog).get('button[aria-label="' + en.moreActions + '"]').trigger('click')
    await click(en.delete)
    expect(w.text()).toContain(en.deleteAccountWarning)
    await click(en.confirm)
    expect(api.deleteAccount).toHaveBeenCalledWith(12)
    expect(w.element.querySelectorAll('template')).toHaveLength(0)
    w.unmount()
  }
)

it('labels native account filters and keeps lowercase platform/method values in scoped queries', async () => {
  const w = await openDetails('en', en)
  expect(w.find('select').exists()).toBe(false)
  const controls = w.findAllComponents(Select)
  const platform = controls.find((c) => c.props('ariaLabel') === en.platform)!
  const method = controls.find((c) => c.props('ariaLabel') === en.method)!
  const status = controls.find((c) => c.props('ariaLabel') === en.status)!
  expect(controls.every((c) => c.find('button').attributes('aria-label'))).toBe(true)
  expect(platform.props('options')).toEqual([
    { value: '', label: en.allPlatforms },
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'openai', label: 'OpenAI' }
  ])
  expect(method.props('options')).toEqual([
    { value: '', label: en.allMethods },
    { value: 'oauth', label: 'OAuth' },
    { value: 'setup-token', label: 'OAuth Token' },
    { value: 'setup-token-manual', label: 'Setup Token' },
    { value: 'refresh_token', label: 'Refresh Token' }
  ])
  expect(status.props('options')).toContainEqual({ value: 'archived', label: en.archived })
  await platform.find('button').trigger('click')
  await platform
    .findAll('[role=option]')
    .find((o) => o.text() === 'OpenAI')!
    .trigger('click')
  await method.find('button').trigger('click')
  await method
    .findAll('[role=option]')
    .find((o) => o.text() === 'Refresh Token')!
    .trigger('click')
  await status.find('button').trigger('click')
  await status
    .findAll('[role=option]')
    .find((o) => o.text() === en.archived)!
    .trigger('click')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.accounts).toHaveBeenLastCalledWith(
    expect.objectContaining({ platform: 'openai', method: 'refresh_token', status: 'archived' }),
    expect.any(AbortSignal)
  )
  expect(w.find('tbody').text()).toContain('Anthropic')
  w.unmount()
})

it.each([true, false])(
  'keeps supplier operations readable in native desktop=%s tables and maintenance collapsed',
  async (desktop) => {
    const media = vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      matches: desktop,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
    const supplier = {
      id: 8,
      user_id: 18,
      name: 'Supplier Eight',
      status: 'active',
      platforms: ['anthropic', 'openai'],
      groups: [{ id: 3, name: 'Shared coding' }],
      account_count: 0,
      account_limit: 100,
      proxy_limit: 20,
      contact_name: 'Operations contact',
      settlement_email: 'billing@example.test',
      binance_id: '987654321012',
      policy_version: 4
    }
    api.list.mockResolvedValue({ items: [supplier], total: 1 })
    api.detail.mockResolvedValue(supplier)
    const w = mount(VendorManagement, {
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: { en: nativeEn }
          })
        ],
        stubs: {
          Pagination: true,
          VendorEditor: true,
          IdentityReconciliation: true
        }
      }
    })
    try {
      await flushPromises()
      const table = w.getComponent(DataTable)
      expect(table.props('columns').map((c) => c.key)).toEqual([
        'name',
        'status',
        'supplierTier',
        'platform',
        'groups',
        'accounts',
        'proxies',
        'contact',
        'binanceId',
        'actions'
      ])
      expect(table.text()).toContain('Supplier Eight')
      expect(table.text()).toContain('Anthropic')
      expect(table.text()).toContain('OpenAI')
      expect(table.text()).toMatch(/0\s*\/\s*100/)
      expect(table.text()).toMatch(/—\s*\/\s*20/)
      expect(table.text()).toContain('Operations contact')
      expect(table.text()).toContain('billing@example.test')
      expect(table.text()).toContain('••••1012')
      expect(table.text()).not.toContain(supplier.binance_id)
      expect(table.findComponent(StatusBadge).props('label')).toBe(en.active)
      expect(w.find('details').attributes('open')).toBeUndefined()
      expect(w.find('details summary').text()).toBe(en.reconcileTitle)
      expect(w.find('details identity-reconciliation-stub').exists()).toBe(true)
      expect(w.html().indexOf('<details')).toBeGreaterThan(w.html().indexOf('Supplier Eight'))
      expect(api.detail).not.toHaveBeenCalled()
      await table
        .findAll('button')
        .find((b) => b.text() === supplier.name)!
        .trigger('click')
      await flushPromises()
      expect(api.detail).toHaveBeenCalledWith(8)
      expect(w.getComponent(VendorEditor).props('vendor')).toEqual(supplier)
    } finally {
      w.unmount()
      media.mockRestore()
    }
  }
)

it('keeps child account details, filters, pagination and history readable without writes or credentials', async () => {
  const scope = { admin: false, vendorId: 8, readOnly: true }
  const child = {
    ...account,
    proxy_id: 6,
    proxy: { name: 'Child proxy', endpoint: 'https://secret-user:secret-password@proxy.invalid:8443/private?token=secret-token' },
    credentials: { access_token: 'secret-access-token' }
  }
  api.accounts.mockResolvedValue({ items: [child], total: 40 })
  api.account.mockResolvedValue(child)
  const w = await openDetails('en', en, false, scope)
  expect(api.account).toHaveBeenCalledWith(12)
  expect(w.getComponent(BaseDialog).text()).toContain('https://proxy.invalid:8443')
  expect(w.getComponent(BaseDialog).findAll('button').map(b => b.text())).toEqual([en.viewUsage])
  for (const secret of ['secret-user', 'secret-password', 'secret-token', 'secret-access-token', '/private'])
    expect(w.html()).not.toContain(secret)
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  expect(w.findComponent({ name: 'Toggle' }).exists()).toBe(false)
  expect(w.findComponent({ name: 'ActionMenu' }).exists()).toBe(false)
  expect(w.findComponent(AccountForm).exists()).toBe(false)
  expect(w.findComponent(AccountImport).exists()).toBe(false)
  expect(w.findComponent({ name: 'TotpStepUpDialog' }).exists()).toBe(false)
  expect(w.get('tbody').findAll('button').map((b) => b.text())).toEqual([
    account.name, en.unknown + '#8', en.details, en.usage
  ])
  expect(w.findAll('button').some((b) => b.text() === en.addAccount)).toBe(false)
  w.getComponent(BaseDialog).vm.$emit('close')
  await w.get('button[aria-label="' + en.usage + ' ' + account.name + '"]').trigger('click')
  expect(w.getComponent(UsageWorkbench).props()).toMatchObject({ admin: false, vendorId: 8, accountId: 12 })
  w.getComponent(BaseDialog).vm.$emit('close')
  await w.get('input[aria-label="' + en.search + '"]').setValue('child')
  await w.get('form').trigger('submit')
  await flushPromises()
  w.getComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
  await flushPromises()
  expect(api.accounts).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'child', page: 2 }), expect.any(AbortSignal))
  for (const [value] of api.scope.mock.calls) expect(value).toEqual(scope)
  expect(api.accountAction).not.toHaveBeenCalled()
  expect(api.deleteAccount).not.toHaveBeenCalled()
  expect(api.exportAccounts).not.toHaveBeenCalled()
  expect(download).not.toHaveBeenCalled()
  w.unmount()
})

it('guards every account mutation entry point even if invoked directly in a child scope', async () => {
  const w = await openDetails('en', en, false, { admin: false, vendorId: 8, readOnly: true })
  const vm = w.vm as any
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
  try {
    vm.openForm()
    vm.openForm(account, true)
    vm.openImport()
    vm.openExport(account)
    vm.openDelete(account)
    expect(vm.form).toBe(false)
    expect(vm.importing).toBe(false)
    expect(vm.confirmExport).toBe(false)
    expect(vm.confirmDelete).toBeUndefined()
    vm.selected = new Set([12])
    vm.confirmDelete = account
    for (const operation of ['pause', 'resume', 'refresh', 'usage-refresh', 'probe', 'sync'])
      await vm.action(account, operation)
    await vm.remove()
    await vm.exportSelected()
    vm.copyProxyEndpoint()
    expect(confirm).not.toHaveBeenCalled()
    expect(api.accountAction).not.toHaveBeenCalled()
    expect(api.deleteAccount).not.toHaveBeenCalled()
    expect(api.exportAccounts).not.toHaveBeenCalled()
    expect(copyToClipboard).not.toHaveBeenCalled()
    expect(download).not.toHaveBeenCalled()
  } finally {
    confirm.mockRestore()
    w.unmount()
  }
})

it.each([false, true])('clears account dialogs and ignores late details when only readOnly changes from %s', async (readOnly) => {
  const w = await openDetails('en', en, false, { admin: false, vendorId: 8, readOnly })
  const vm = w.vm as any
  vm.openForm(account, true)
  vm.openImport()
  vm.openExport(account)
  vm.openDelete(account)
  vm.usageId = 12
  let finish!: (value: unknown) => void
  api.account.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
  const pending = vm.showDetail(account)
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: !readOnly } })
  finish({ ...account, name: 'Previous scope detail' })
  await pending
  await flushPromises()
  expect(w.findComponent(BaseDialog).exists()).toBe(false)
  expect(w.findComponent(AccountForm).exists()).toBe(false)
  expect(w.findComponent(AccountImport).exists()).toBe(false)
  expect(w.findComponent(UsageWorkbench).exists()).toBe(false)
  expect(vm.editing).toBeUndefined()
  expect(vm.reauthorize).toBe(false)
  expect(vm.confirmExport).toBe(false)
  expect(vm.selected.size).toBe(0)
  expect(w.text()).not.toContain('Previous scope detail')
  w.unmount()
})

it('does not let a previous account action refresh or unlock a new child detail request', async () => {
  const w = await openDetails('en', en)
  const vm = w.vm as any
  let finishAction!: (value: unknown) => void
  api.accountAction.mockImplementationOnce(() => new Promise((resolve) => { finishAction = resolve }))
  const pendingAction = vm.action(account, 'refresh')
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: true } })
  await flushPromises()
  let finishDetail!: (value: unknown) => void
  api.account.mockImplementationOnce(() => new Promise((resolve) => { finishDetail = resolve }))
  const pendingDetail = vm.showDetail(account)
  finishAction({ status: 'success' })
  await pendingAction
  expect(vm.busy).toBe(true)
  expect(vm.actionResult).toBeUndefined()
  expect(api.accounts).toHaveBeenCalledTimes(2)
  finishDetail(account)
  await pendingDetail
  expect(vm.busy).toBe(false)
  w.unmount()
})

it.each(['download', 'late challenge', 'pending verification'])(
  'cancels account export %s across a permission change without stale downloads or retries',
  async (stage) => {
    const w = await openDetails('en', en)
    const vm = w.vm as any
    const controller = w.getComponent({ name: 'TotpStepUpDialog' }).props('controller')
    let finish!: (value: Blob) => void
    let fail!: (reason: unknown) => void
    api.exportAccounts.mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    vm.openExport(account)
    const pending = vm.exportSelected()
    if (stage === 'pending verification') {
      fail({ code: 'STEP_UP_REQUIRED' })
      await flushPromises()
      expect(controller.visible.value).toBe(true)
    }
    await w.setProps({ scope: { admin: true, vendorId: 8, readOnly: true } })
    if (stage === 'download') finish(new Blob(['secret-export']))
    if (stage === 'late challenge') fail({ code: 'STEP_UP_REQUIRED' })
    await pending
    await flushPromises()
    expect(controller.visible.value).toBe(false)
    expect(download).not.toHaveBeenCalled()
    expect(vm.exporting).toBe(false)
    expect(vm.actionError).toBe('')
    await w.setProps({ scope: { admin: true, vendorId: 8 } })
    controller.onVerified()
    await flushPromises()
    expect(api.exportAccounts).toHaveBeenCalledTimes(1)
    expect(vm.confirmExport).toBe(false)
    w.unmount()
  }
)

it.each([{ admin: false }, { admin: true, vendorId: 8 }])('retains verified credential export in writable account scope %j', async (scope) => {
  const data = new Blob(['owned credentials'])
  api.exportAccounts.mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' }).mockResolvedValueOnce(data)
  const w = await openDetails('en', en, false, scope)
  const vm = w.vm as any
  vm.openExport(account)
  const pending = vm.exportSelected()
  await flushPromises()
  const controller = w.getComponent({ name: 'TotpStepUpDialog' }).props('controller')
  expect(controller.visible.value).toBe(true)
  controller.onVerified()
  await pending
  expect(api.exportAccounts.mock.calls).toEqual([[[12], scope], [[12], scope]])
  expect(download).toHaveBeenCalledWith(data, 'supplier-authorization.json')
  w.unmount()
})

it('refreshes accounts in the background without clearing rows, and pauses polling for a form', async () => {
  vi.useFakeTimers()
  const w = await openDetails('en', en)
  try {
    w.getComponent(BaseDialog).vm.$emit('close')
    await flushPromises()
    let finish!: (page: unknown) => void
    api.accounts.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.accounts).toHaveBeenCalledTimes(2)
    expect(w.get('tbody').text()).toContain('Owned account')
    finish({ items: [account], total: 1 })
    await flushPromises()
    await w
      .findAll('button')
      .find((b) => b.text() === en.addAccount)!
      .trigger('click')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.accounts).toHaveBeenCalledTimes(2)
  } finally {
    w.unmount()
    vi.useRealTimers()
  }
})

it.each([
  { admin: true, vendorId: 8 },
  { admin: false, readOnly: true, children: true }
] as ResourceScope[])('opens historical usage from the detail drawer in the row owner scope and restores focus %j', async scope => {
  api.accounts.mockResolvedValue({ items: [{ ...account, vendor_id: 9 }], total: 1 })
  api.account.mockResolvedValue({ ...account, vendor_id: 9, vendor_name: 'Child owner' })
  const w = await openDetails('en', en, true, scope)
  try {
    const usage = w.getComponent(BaseDialog).findAll('button').find(b => b.text() === en.viewUsage)!
    await usage.trigger('click')
    await flushPromises()
    expect(w.findAllComponents(BaseDialog)).toHaveLength(1)
    expect(w.find('.account-detail-drawer').exists()).toBe(false)
    expect(w.getComponent(UsageWorkbench).props()).toMatchObject({ accountId: 12, vendorId: 9, admin: scope.admin, readOnly: scope.readOnly })
    w.getComponent(BaseDialog).vm.$emit('close')
    await flushPromises()
    expect(w.get('.account-detail-drawer').exists()).toBe(true)
    expect(document.activeElement?.textContent).toContain(en.viewUsage)
    expect(api.accountAction).not.toHaveBeenCalled()
    await w.getComponent(BaseDialog).findAll('button').find(b => b.text() === en.viewUsage)!.trigger('click')
    await w.setProps({ scope: { ...scope, vendorId: 10 } })
    expect(w.findComponent(UsageWorkbench).exists()).toBe(false)
    expect(w.findComponent(BaseDialog).exists()).toBe(false)
  } finally { w.unmount() }
})
it.each([false, true])('shows the supply action icon for paused=%s', async paused => {
  api.accounts.mockResolvedValue({ items: [{ ...account, vendor_paused_at: paused ? '2026-10-17T00:00:00Z' : null }], total: 1 })
  const w = await openDetails('en', en)
  w.getComponent(BaseDialog).vm.$emit('close')
  await flushPromises()
  await w.get('button[aria-label="' + en.moreActions + ' ' + account.name + '"]').trigger('click')
  const button = w.get('[role=menu]').findAll('button').find(b => b.text() === (paused ? en.resume : en.pause))!
  expect(button.getComponent(Icon).props('name')).toBe(paused ? 'play' : 'pause')
  await button.trigger('click')
  expect(api.accountAction).toHaveBeenCalledWith(12, paused ? 'resume' : 'pause')
  w.unmount()
})
