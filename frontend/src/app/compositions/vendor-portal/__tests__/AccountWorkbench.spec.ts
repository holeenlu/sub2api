import { AccountStatusIndicator } from '@/features/resources/panels'
import Icon from '@/shared/ui/icons/Icon.vue'
import { GroupBadge, groupPresentationKey, type GroupPresentation } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import { ref as groupPresentationRef, type ComponentPublicInstance, type Ref } from 'vue'
import { useSupplyServices, type SupplyIdentity } from '@/features/supply/services'
import { config as groupComponentTestConfig } from '@vue/test-utils'
import DataTable from '@/shared/ui/DataTable.vue'
import VendorManagement from '@/features/supply/components/VendorManagement.vue'
import VendorEditor from '@/features/supply/components/VendorEditor.vue'
import { loadApplicationLocale } from '@/app/i18n'
import vendorMessageShape from '@/features/supply/locales/en'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const assembledJaMessages = await loadApplicationLocale('ja')
const assembledZhTWMessages = await loadApplicationLocale('zh-TW')
const nativeEn = assembledEnMessages
const en = vendorMessages(assembledEnMessages, 'en')
const zh = vendorMessages(assembledZhMessages, 'zh')
const ja = vendorMessages(assembledJaMessages, 'ja')
const zhTW = vendorMessages(assembledZhTWMessages, 'zh-TW')
import Select from '@/shared/ui/Select.vue'
import StatusBadge from '@/shared/ui/StatusBadge.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import { enableAutoUnmount, mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, it, expect, vi, beforeEach } from 'vitest'
import AccountWorkbench from '../components/AccountWorkbench.vue'
import AccountInfoCell from '@/app/compositions/resource-workspace/components/AccountInfoCell.vue'
import PlatformTypeBadge from '@/app/compositions/resource-workspace/components/PlatformTypeBadge.vue'
import AccountForm from '../components/AccountForm.vue'
import { EditAccountModal, BulkEditAccountModal } from '@/features/resources/panels'
import AccountImport from '../components/AccountImport.vue'
import UsageWorkbench from '../components/UsageWorkbench.vue'
import type { Metrics, ResourceScope, Vendor, VendorAccount } from '@/features/supply/types'
enableAutoUnmount(afterEach)

function required<T>(value: T | null | undefined, description: string): T {
  if (value === null || value === undefined) throw new Error('Missing ' + description)
  return value
}

type VendorMessages = typeof vendorMessageShape.vendor
type SelectInstance = ComponentPublicInstance<Parameters<typeof Select<string>>[0]>
type VendorTableInstance = ComponentPublicInstance<Parameters<typeof DataTable<Vendor>>[0]>
type StatusInstance = ComponentPublicInstance<Parameters<typeof AccountStatusIndicator<VendorAccount>>[0]>

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function assertMessages<T extends Record<string, unknown>>(value: unknown, shape: T, path: string): asserts value is T {
  if (!isRecord(value)) throw new Error('Missing locale namespace: ' + path)
  for (const [key, expected] of Object.entries(shape)) {
    if (typeof expected === 'string') {
      if (typeof value[key] !== 'string') throw new Error('Missing locale message: ' + path + '.' + key)
    } else if (isRecord(expected)) {
      assertMessages(value[key], expected, path + '.' + key)
    } else {
      throw new Error('Unsupported locale shape: ' + path + '.' + key)
    }
  }
}

function vendorMessages(messages: unknown, locale: string): VendorMessages {
  if (!isRecord(messages)) throw new Error('Missing application locale: ' + locale)
  const vendor = messages.vendor
  assertMessages(vendor, vendorMessageShape.vendor, locale + '.vendor')
  return vendor
}

function vendorFixture(overrides: Partial<Vendor> = {}): Vendor {
  return {
    id: 8, user_id: 8, name: 'Supplier Eight', status: 'active', policy_version: 1,
    platforms: ['anthropic', 'openai'], group_ids: [], groups: [],
    account_defaults: { concurrency: 3, priority: 50 }, account_limit: 100, proxy_limit: 20,
    allow_direct: true, contact_name: '', settlement_email: '', binance_id: '',
    default_coefficient: '1', group_coefficients: {}, contract_version: 1,
    ...overrides
  }
}

function metricsFixture(overrides: Partial<Metrics> = {}): Metrics {
  return {
    requests: null, input_tokens: null, output_tokens: null, cache_creation_tokens: null,
    cache_read_tokens: null, cost_usd: null, cache_hit_rate: null,
    ...overrides
  }
}



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
vi.mock('@/shared/http', () => ({ apiClient: native, default: native }))
vi.mock('@/features/access-policy/groups', () => {
  const groups = { getAll: native.groups }
  return { groupsAPI: groups, default: groups }
})
const copyToClipboard = vi.hoisted(() => vi.fn().mockResolvedValue(true))
const download = vi.hoisted(() => vi.fn())
vi.mock('@/features/supply/helpers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/supply/helpers')>()),
  download
}))
vi.mock('@/features/supply/api', () => ({
  resourceAPI: (scope: ResourceScope) => {
    api.scope(scope)
    return api
  },
  vendorAPI: api,
  vendorManagementAPI: () => api
}))
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
const account: VendorAccount = {
  id: 12,
  name: 'Owned account',
  vendor_id: 8,
  proxy_id: null,
  group_ids: [],
  platform: 'anthropic',
  type: 'oauth',
  groups: [],
  concurrency: 3,
  priority: 50,
  quota: {},
  status: 'active',
  archived: false,
  archived_at: null,
  schedulable: true,
  created_at: '2026-10-23T03:30:57Z',
  today_usage: metricsFixture({ cost_usd: '1.25' }),
  all_time_usage: metricsFixture({ requests: 37, total_tokens: 4200, cost_usd: '10.5', cache_hit_rate: 0.25 })
}
async function openDetails(
  locale: string,
  messages: VendorMessages,
  nativeDialog = false,
  scope: ResourceScope = { admin: true, vendorId: 8 }
): Promise<VueWrapper<InstanceType<typeof AccountWorkbench>>> {
  const w = mount(AccountWorkbench, {
    ...(nativeDialog ? { attachTo: document.body } : {}),
    props: {
      scope,
      vendor: vendorFixture()
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
  (useSupplyServices().identity as Ref<SupplyIdentity>).value = { userId: 1, role: 'super_admin', isSuperAdmin: true }
  const groupPresentation: GroupPresentation = {
    peakRate: peakRatePresentation, serverUtcOffset: groupPresentationRef(undefined),
    isSimpleMode: groupPresentationRef(false), concretePlatformOptions: groupPresentationRef([]),
    platformLabel: (platform: string) => platform
  }
  groupComponentTestConfig.global.provide = { ...groupComponentTestConfig.global.provide, [groupPresentationKey]: groupPresentation }
  vi.clearAllMocks()
  native.get.mockImplementation(async (url: string) => ({ data: url.startsWith('/admin/vendors/') ? { id: 8, group_ids: [], groups: [] } : { ...account, id: 12, vendor_id: 8, credentials: {}, extra: {} } }))
  native.post.mockResolvedValue({})
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
const dualAccount: VendorAccount = {
  ...account, platform: 'openai', type: 'oauth', vendor_id: 8,
  authorization_modes: ['native', 'bps'], bps_models: null,
  authorizations: [
    { mode: 'native', authorized: true, enabled: true, status: 'active', can_refresh: true },
    { mode: 'bps', authorized: true, enabled: true, status: 'active', can_refresh: true }
  ]
}
async function openOpenAI(scope: ResourceScope = { admin: false }): Promise<VueWrapper<InstanceType<typeof AccountWorkbench>>> {
  api.account.mockResolvedValue(dualAccount)
  api.accounts.mockResolvedValue({ items: [dualAccount], total: 1, overview: { total: 1 } })
  const w = await openDetails('en', en, false, scope)
  await w.setProps({ vendor: vendorFixture({ platforms: ['openai'], openai_authorization_modes: ['native', 'bps'] }) })
  return w
}
it('shows one dual-grant account and filters by the selected mode, clearing it for Claude', async () => {
  const w = await openOpenAI()
  expect(w.get('tbody').text()).toContain(en.authorization.dual)
  expect(w.get('.vendor-accounts-table').findAll('tbody tr')).toHaveLength(1)
  w.findAllComponents<SelectInstance>({ name: 'Select' }).find(s => s.props('ariaLabel') === en.authorization.mode)!.vm.$emit('update:modelValue', 'bps')
  await flushPromises()
  expect(api.accounts.mock.lastCall?.[0]).toMatchObject({ authorization_mode: 'bps', page: 1 })
  w.findAllComponents<SelectInstance>({ name: 'Select' }).find(s => s.props('ariaLabel') === en.platform)!.vm.$emit('update:modelValue', 'anthropic')
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
  expect(w.find('[data-testid=confirm-revoke-authorization]').exists()).toBe(true)
  api.revokeAuthorization.mockRejectedValueOnce(new Error('Grant changed; reload'))
  await w.get('[data-testid=confirm-revoke-authorization]').trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Grant changed; reload')
  expect(w.find('[data-testid=confirm-revoke-authorization]').exists()).toBe(true)
  await w.get('[data-testid=confirm-revoke-authorization]').trigger('click')
  await flushPromises()
  expect(api.revokeAuthorization).toHaveBeenLastCalledWith(12, 'bps', expect.objectContaining({ signal: expect.any(AbortSignal) }))
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
  expect(api.accountAction).toHaveBeenCalledWith(12, 'refresh', 'bps', expect.objectContaining({ signal: expect.any(AbortSignal) }))
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
    expect(api.deleteAccount).toHaveBeenCalledWith(12, expect.objectContaining({ signal: expect.any(AbortSignal) }))
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
    expect(row.getComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).props('account')).toMatchObject({ status: 'active', schedulable: false, archived: true })
    expect(row.text()).toContain('$1.25')
    expect(row.text()).toContain('$10.50')
    expect(row.find('input[type=checkbox]').exists()).toBe(false)
    expect(w.get('thead input[type=checkbox]').attributes('disabled')).toBeDefined()
    expect(row.findAll('button:not([role=switch])').map((b) => b.text())).toEqual([
      account.name,
      '',
      messages.copy,
      messages.unknown + '#8',
      messages.details,
      messages.usage
    ])
    await row.get('button[aria-label="' + messages.details + ' ' + account.name + '"]').trigger('click')
    await flushPromises()
    const detail = w.getComponent(BaseDialog)
    expect(detail.get('.badge-gray').text()).toBe(messages.archived)
    expect(detail.get('[data-detail-field=archivedAt]').attributes('title')).toBe(archived.archived_at)
    expect(detail.get('[data-detail-field=status]').getComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).props('account')).toMatchObject({ status: 'active', schedulable: false, archived: true })
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
  expect(w.get('tbody').findAll('.badge-gray').filter(b => b.text() === en.archived)).toHaveLength(2)
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
    vendor: vendorFixture({ id: 9 })
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
    expect(field('status').getComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).props('account')).toMatchObject({ status: 'active', schedulable: false })
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
    expect(w.findAllComponents(GroupBadge).some(badge => badge.props('name') === 'Coding group · #3' && badge.props('platform') === 'anthropic' && badge.props('showRate') === false)).toBe(true)
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
        vendor: vendorFixture()
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
    const actions = w.findAll('button:not([role=switch])').map((b) => b.text())
    expect(actions).toEqual(expect.arrayContaining([en.addAccount, en.moreActions]))
    expect(w.text()).toContain(en.quotaLoad)
    expect(w.text()).toContain('≥ 90%')
    expect(w.findComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).props('account').status).toBe('active')
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
    api.accounts.mockResolvedValue({ items: [{ ...account, schedulable: true, platform_schedulable: true }], total: 1 })
    const w = mount(AccountWorkbench, {
      attachTo: document.body,
      props: { scope, vendor: vendorFixture() },
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
            (label === en.scheduling && b.attributes('role') === 'switch') ||
            (label !== en.scheduling && (b.attributes('aria-label') === label ||
              b.attributes('aria-label')?.startsWith(label + ' ') ||
              b.text() === label))
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
      expect(w.getComponent(EditAccountModal).props('account')).toMatchObject({ id: 12 })
      expect(api.proxies).toHaveBeenCalledWith({ page: 1, page_size: 50 }, expect.any(AbortSignal))
      expect(native.get).toHaveBeenCalledWith('/admin/accounts/12', { signal: expect.any(AbortSignal), params: { vendor_id: 8, view: 'management' } })
      await required(w.getComponent(EditAccountModal).props('saveAccount'), 'management save callback')(12, { concurrency: 23 })
      expect(native.put).toHaveBeenCalledWith('/admin/accounts/12', { concurrency: 23 }, { signal: expect.any(AbortSignal), params: { vendor_id: 8, view: 'management' } })
      w.getComponent(EditAccountModal).vm.$emit('close')
    } else {
      expect(w.getComponent(AccountForm).props('account')).toMatchObject({ id: 12 })
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
    await click(en.scheduling)
    if (scope.admin) expect(native.post).toHaveBeenLastCalledWith('/admin/accounts/12/schedulable', { schedulable: false }, { signal: expect.any(AbortSignal), params: { vendor_id: 8 } })
    else expect(api.accountAction).toHaveBeenLastCalledWith(12, 'pause', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await click(en.scheduling)
    if (!scope.admin) expect(api.accountAction).toHaveBeenLastCalledWith(12, 'resume', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await click(en.details)
    expect(api.account).toHaveBeenCalledWith(12, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const detail = w.findComponent({ name: 'BaseDialog' })
    await detail.get('button[aria-label="' + en.moreActions + '"]').trigger('click')
    expect(detail.findAll('button:not([role=switch])').map((b) => b.text())).toEqual(
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
      expect(api.accountAction).toHaveBeenLastCalledWith(12, operation, undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    }
    confirm.mockRestore()
    await w.getComponent(BaseDialog).get('button[aria-label="' + en.moreActions + '"]').trigger('click')
    await click(en.delete)
    expect(w.text()).toContain(en.deleteAccountWarning)
    await click(en.confirm)
    expect(api.deleteAccount).toHaveBeenCalledWith(12, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(w.element.querySelectorAll('template')).toHaveLength(0)
    w.unmount()
  }
)

it('labels native account filters and keeps lowercase platform/method values in scoped queries', async () => {
  const w = await openDetails('en', en)
  expect(w.find('select').exists()).toBe(false)
  const controls = w.findAllComponents<SelectInstance>({ name: 'Select' })
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
      const table = w.getComponent<VendorTableInstance>({ name: 'DataTable' })
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
      expect(api.detail).toHaveBeenCalledWith(8, expect.objectContaining({ signal: expect.any(AbortSignal) }))
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
  expect(api.account).toHaveBeenCalledWith(12, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(w.getComponent(BaseDialog).text()).toContain('https://proxy.invalid:8443')
  expect(w.getComponent(BaseDialog).findAll('button').map(b => b.text())).toEqual([en.viewUsage])
  for (const secret of ['secret-user', 'secret-password', 'secret-token', 'secret-access-token', '/private'])
    expect(w.html()).not.toContain(secret)
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  expect(w.get('button[role=switch]').attributes('disabled')).toBeDefined()
  expect(w.findComponent({ name: 'ActionMenu' }).exists()).toBe(false)
  expect(w.findComponent(AccountForm).exists()).toBe(false)
  expect(w.findComponent(AccountImport).exists()).toBe(false)
  expect(w.findComponent({ name: 'TotpStepUpDialog' }).exists()).toBe(false)
  expect(w.get('tbody').findAll('button:not([role=switch])').map((b) => b.text())).toEqual([
    account.name, '', en.copy, en.unknown + '#8', en.details, en.usage
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
    expect(w.find('.account-detail-drawer').exists()).toBe(true)
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
  expect(api.accountAction).toHaveBeenCalledWith(12, paused ? 'resume' : 'pause', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  w.unmount()
})

it.each(['anthropic', 'openai'])('resolves legacy account group badges from the %s account in list and details', async platform => {
  const item = { ...account, platform, groups: [{ id: 3, name: 'No platform in account DTO' }] }
  api.accounts.mockResolvedValue({ items: [item], total: 1, page: 1, page_size: 20 })
  api.account.mockResolvedValue(item)
  const w = await openDetails('en', en)
  const badges = w.findAllComponents(GroupBadge)
  expect(badges.filter(badge => badge.props('name').startsWith('No platform in account DTO')).map(badge => badge.props('platform'))).toEqual([platform, platform])
  await w.setProps({ vendor: vendorFixture({ groups: [{ id: 3, name: 'Composite group', platform: 'composite' }] }) })
  expect(badges.filter(badge => badge.props('name').startsWith('No platform in account DTO')).every(badge => badge.props('platform') === 'composite')).toBe(true)
  w.unmount()
})

it('shows the operations rate-limit state in both supplier list and details', async () => {
  const limited = { ...account, platform: 'openai', status: 'active', rate_limit_reset_at: '2099-01-01T00:00:00Z', blockers: ['quota_cooldown'] }
  api.accounts.mockResolvedValue({ items: [limited], total: 1, page: 1, page_size: 20 })
  api.account.mockResolvedValue(limited)
  const w = await openDetails('en', en)
  for (const indicator of w.findAllComponents<StatusInstance>({ name: 'AccountStatusIndicator' })) {
    expect(indicator.text()).toContain('admin.accounts.status.rateLimited')
    expect(indicator.text()).toContain('429')
    expect(indicator.text()).not.toContain('admin.accounts.status.active')
  }
  w.unmount()
})

it('keeps native health independent of supplier supply policy blockers', async () => {
  const blocked = { ...account, platform_schedulable: true, schedulable: false, rate_limit_reset_at: null, overload_until: null, temp_unschedulable_until: null, blockers: ['supply_policy_blocked'] }
  api.accounts.mockResolvedValue({ items: [blocked], total: 1, page: 1, page_size: 20 })
  api.account.mockResolvedValue(blocked)
  const w = await openDetails('en', en)
  expect(w.getComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).text()).toContain('admin.accounts.status.active')
  expect(w.text()).toContain(en.accountBlockers.supply_policy_blocked)
  w.unmount()
})

it.each([
  ['default_claude_max_5x', 'Max 5x'],
  ['default_claude_max_20x', 'Max 20x'],
  ['default_claude_pro', 'Pro']
])('renders Claude %s consistently in the shared badge and profile metadata', async (tier, label) => {
  const enriched = { ...account, rate_limit_tier: tier, plan_type: 'claude_max',
    billing_type: 'apple_subscription', method: 'setup-token-manual' }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en)
  const badge = w.get('[data-account-info]').getComponent(PlatformTypeBadge)
  expect(badge.props('planType')).toBe(label)
  expect(badge.text()).toContain(label)
  expect(badge.text()).toContain('Setup Token')
  expect(badge.props('billingType')).toBe('apple_subscription')
  expect(badge.text()).toContain('admin.accounts.identitySummary.payment.apple')
  expect(badge.text()).not.toContain('Pro 200')
  expect(w.get('[data-account-info]').text()).toContain(label)
})

it.each([
  ['prolite', 'Pro 100', 'agentIdentity', 'Agent Identity'],
  ['pro', 'Pro 200', 'personalAccessToken', 'PAT'],
  ['team', 'Business', undefined, 'Refresh Token']
])('renders OpenAI %s metadata using the same badge as admin', async (plan, label, authMode, authLabel) => {
  const enriched = { ...dualAccount, plan_type: plan, auth_mode: authMode, method: 'refresh_token',
    rate_limit_tier: 'default_claude_max_20x', billing_type: 'subscription',
    subscription_expires_at: '2099-01-01T00:00:00Z',
    extra: { privacy_mode: 'training_off', openai_compact_mode: 'force_off', openai_compact_supported: true } }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en)
  const badge = w.get('[data-account-info]').getComponent(PlatformTypeBadge)
  expect(badge.props()).toMatchObject({
    planType: plan, method: 'refresh_token', authMode, billingType: 'subscription',
    authorizationModes: ['native', 'bps'], privacyMode: 'training_off',
    subscriptionExpiresAt: enriched.subscription_expires_at,
    compact: { mode: 'force_off', supported: true }
  })
  expect(badge.text()).toContain(label)
  expect(badge.text()).toContain(authLabel)
  expect(badge.text()).toContain(en.authorization.dual)
  expect(badge.text()).toContain('admin.accounts.openai.compactUnsupported')
  expect(badge.text()).toContain('admin.accounts.identitySummary.payment.subscription')
  expect(badge.text()).not.toContain('Max 20x')
  expect(w.get('[data-account-info]').text()).toContain(label)
})

it.each([undefined, []].map(modes => [modes]))('does not derive a badge authorization from OAuth or fallback grants: %s', async modes => {
  const enriched = { ...dualAccount, authorization_modes: modes }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en)
  const badge = w.get('[data-account-info]').getComponent(PlatformTypeBadge)
  expect(badge.props('authorizationModes')).toEqual(modes)
  for (const label of [en.authorization.native, en.authorization.bps, en.authorization.dual]) {
    expect(badge.text()).not.toContain(label)
  }
})

it.each([
  { admin: true, vendorId: 8 },
  { admin: false, vendorId: 8 },
  { admin: false, vendorId: 8, readOnly: true }
])('passes supplier account metadata and credential expiry within the existing scope: %j', async scope => {
  const extra = {
    privacy_mode: 'training_off', openai_compact_mode: 'force_off',
    openai_compact_supported: true, openai_compact_checked_at: '2026-10-26T08:00:00Z'
  }
  const enriched = {
    ...dualAccount, plan_type: 'pro', method: 'refresh_token', auth_mode: 'agentIdentity',
    authorization_expires_at: '2099-01-01T00:00:00Z',
    rate_limit_tier: 'recorded-tier', subscription_type: 'recorded-subscription',
    billing_type: 'subscription', subscription_expires_at: '2098-01-01T00:00:00Z',
    expires_at: '2097-01-01T00:00:00Z', extra
  }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en, false, scope)
  expect(w.getComponent(AccountInfoCell).props()).toMatchObject({
    credentialExpiresAt: enriched.authorization_expires_at,
    rateLimitTier: enriched.rate_limit_tier, subscriptionType: enriched.subscription_type,
    planType: 'pro', method: 'refresh_token', authMode: 'agentIdentity', billingType: 'subscription',
    subscriptionExpiresAt: enriched.subscription_expires_at, authorizationModes: ['native', 'bps'],
    privacyMode: 'training_off',
    compact: { mode: 'force_off', supported: true, checkedAt: extra.openai_compact_checked_at }
  })
  for (const [value] of api.scope.mock.calls) expect(value).toEqual(scope)
  expect(api.accountAction).not.toHaveBeenCalled()
  expect(api.updateAccount).not.toHaveBeenCalled()
  expect(api.revokeAuthorization).not.toHaveBeenCalled()
})

it.each([undefined, null, ''])('does not replace missing supplier credential expiry with subscription or account expiry: %s', async authorizationExpiresAt => {
  const enriched = {
    ...account, authorization_expires_at: authorizationExpiresAt,
    subscription_expires_at: '2098-01-01T00:00:00Z', expires_at: '2097-01-01T00:00:00Z'
  }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en)
  expect(w.getComponent(AccountInfoCell).props()).toMatchObject({
    credentialExpiresAt: authorizationExpiresAt, rateLimitTier: undefined, subscriptionType: undefined,
    subscriptionExpiresAt: enriched.subscription_expires_at
  })
})

it('renders precise Claude family costs, profile metadata and one-line creation time', async () => {
  const enriched = {    
...account, email: 'verified@example.invalid', subscription_type: 'claude_max', rate_limit_tier: 'default_claude_max_20x', billing_type: 'apple_subscription', method: 'oauth',
    created_at: '2026-10-23T03:30:57Z', today_usage: { cost_usd: '1.23456789', fable_cost_usd: '0.23' }, all_time_usage: { cost_usd: '10.50', fable_cost_usd: '2.25' }  
}
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en)
  const row = w.get('tbody tr')
  expect(row.text()).toContain('Max 20x')
  const info = row.get('[data-account-info]')
  expect(info.text()).toContain('Max 20x')
  expect(info.text()).toContain('verified@example.invalid')
  expect(info.text()).not.toContain(en.verifiedEmail)
  expect(info.text()).not.toContain(en.rateLimitTier)
  expect(row.get('[data-account-payment]').text()).toBe('admin.accounts.identitySummary.payment.apple')
  expect(info.get('[data-account-payment-detail]').text()).toBe('admin.accounts.identitySummary.payment.apple')
  expect(info.get('[data-account-plan-detail]').text()).toContain(enriched.rate_limit_tier)
  expect(row.text()).toContain(en.nonFable)
  expect(row.text()).toContain('$1.00')
  expect(row.text()).toContain('$8.25')
  expect(row.find('[title="$1.23456789"]').exists()).toBe(true)
  expect(row.get('[data-account-created]').classes()).toContain('whitespace-nowrap')
  expect(row.get('[data-account-created] time').text()).toMatch(/2026\/10\/23 \d{2}:30:57/)
  expect(w.text()).toContain('default_claude_max_20x')
  w.unmount()
})
it.each([true, false])('changes scheduling through the correct owner endpoint: admin=%s', async (admin) => {
  const a = { ...account, platform_schedulable: true, schedulable: true }
  api.accounts.mockResolvedValue({ items: [a], total: 1 })
  api.account.mockResolvedValue(a)
  native.post.mockResolvedValue({})
  api.accountAction.mockResolvedValue({})
  const w = await openDetails('en', en, false, { admin, vendorId: 8, readOnly: admin })
  const toggle = w.getComponent({ name: 'Toggle' })
  expect(toggle.attributes('disabled')).toBeUndefined()
  toggle.vm.$emit('update:modelValue', false)
  await flushPromises()
  if (admin) expect(native.post).toHaveBeenCalledWith('/admin/accounts/12/schedulable', { schedulable: false }, { signal: expect.any(AbortSignal), params: { vendor_id: 8 } })
  else expect(api.accountAction).toHaveBeenCalledWith(12, 'pause', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  w.unmount()
})

it('uses one status column and one filter for health, supply and scheduling', async () => {
  const w = await openDetails('en', en)
  const headers = w.findAll('thead th').map(x => x.text())
  expect(headers).not.toContain(en.scheduling)
  expect(w.get('[data-account-status]').find('[role=switch]').exists()).toBe(true)
  const status = w.findAllComponents<SelectInstance>({ name: 'Select' }).find(c => c.props('ariaLabel') === en.status)!
  for (const [value, query] of [['schedulability:disabled', { schedulability: 'disabled' }], ['rate_limited', { status: 'rate_limited' }], ['cooldown:seven_day_fable', { cooldown: 'seven_day_fable' }]] as const) {
    status.vm.$emit('update:modelValue', value)
    await flushPromises()
    expect(api.accounts).toHaveBeenLastCalledWith(expect.objectContaining(query), expect.any(AbortSignal))
  }
  expect(w.findAllComponents<SelectInstance>({ name: 'Select' }).some(c => c.props('ariaLabel') === en.schedulingAvailability)).toBe(false)
  w.unmount()
})

it('shows fleet totals above the separately scoped supplier overview', async () => {
  api.accounts.mockResolvedValue({ items: [account], total: 1, overview: { total: 1, rpm: 3, available: 1, cooling: 0 }, global_overview: { total: 100, rpm: 591, tpm: 1981000, tpm_with_cache: 83200000, current_concurrency: 376, maximum_concurrency: 36395, available: 2348, cooling: 112 } })
  const w = await openDetails('en', en)
  const header = w.get('[data-global-overview]').text()
  expect(header).toContain(en.globalAccountOverview)
  for (const n of ['591', '2M', '83.2M', '376', '36,395', '2,348', '112']) expect(header).toContain(n)
  expect(w.get('[data-global-overview]').findAll('strong').map(n => n.text())).toEqual(['591', '83.2M', '2M', '376', '36,395', '2,348', '112'])
  expect(w.get('#account-overview').text()).toContain('1')
  w.unmount()
})

it.each(['verified@example.invalid', ''])('copies the full verified email or name without opening details: %s', async email => {
  api.accounts.mockResolvedValue({ items: [{ ...account, email }], total: 1 })
  const w = await openDetails('en', en)
  copyToClipboard.mockClear()
  await w.get('[data-account-copy]').trigger('click')
  expect(copyToClipboard).toHaveBeenCalledWith(email || account.name)
  expect(w.get('[data-account-details]').attributes('aria-expanded')).toBe('false')
  w.unmount()
})

it.each([
  { admin: true, vendorId: 8 },
  { admin: false, vendorId: 8 },
  { admin: false, vendorId: 8, readOnly: true }
])('keeps shared account details and usage in the supplied scope: %j', async scope => {
  const enriched = { ...account, email: 'verified@example.invalid', plan_type: 'pro', billing_type: 'subscription' }
  api.accounts.mockResolvedValue({ items: [enriched], total: 1 })
  api.account.mockResolvedValue(enriched)
  const w = await openDetails('en', en, false, scope)
  w.getComponent(BaseDialog).vm.$emit('close')
  await flushPromises()
  const cell = w.getComponent(AccountInfoCell)
  expect(cell.props()).toMatchObject({ name: account.name, id: account.id, email: enriched.email, billingType: 'subscription' })
  await cell.get('[data-account-details]').trigger('click')
  expect(cell.get('[data-account-details]').attributes('aria-expanded')).toBe('true')
  const info = cell.get('[data-account-info]')
  expect(info.get('[data-account-plan-detail]').text()).toBe('Pro')
  expect(info.get('[data-account-payment-detail]').text()).toBe('admin.accounts.identitySummary.payment.subscription')
  expect(info.get('[data-account-auth-detail]').text()).toContain('OAuth')
  expect(info.get('[data-account-expiry-detail]').text()).toBe('admin.accounts.identitySummary.unknown')
  expect(cell.get('[data-account-info]').text()).toContain(enriched.email)
  expect(cell.get('[data-account-info]').text()).not.toContain('admin.accounts.columns.proxy')
  await cell.get('[data-account-copy]').trigger('click')
  expect(copyToClipboard).toHaveBeenLastCalledWith(enriched.email)
  expect(cell.get('[data-account-details]').attributes('aria-expanded')).toBe('true')
  await cell.get('[data-account-details]').trigger('click')
  expect(cell.get('[data-account-details]').attributes('aria-expanded')).toBe('false')
  await w.get('button[aria-label="' + w.vm.$t('vendor.usage') + ' ' + account.name + '"]').trigger('click')
  expect(w.getComponent(UsageWorkbench).props()).toMatchObject({ admin: scope.admin, vendorId: 8, accountId: 12 })
  for (const [value] of api.scope.mock.calls) expect(value).toEqual(scope)
  expect(api.accountAction).not.toHaveBeenCalled()
  expect(api.updateAccount).not.toHaveBeenCalled()
  expect(api.revokeAuthorization).not.toHaveBeenCalled()
})
it('rounds quota percentages before rendering beside reset timestamps', async () => {
  api.accounts.mockResolvedValue({ items: [{ ...account, quota: { five_hour: { utilization: 55.00000000000001, resets_at: '2099-01-01T12:00:00Z' } } }], total: 1 })
  const w = await openDetails('en', en)
  expect(w.get('tbody').text()).toContain('55%'); expect(w.get('tbody').text()).not.toContain('0000000000')
  w.unmount()
})

it.each(['en', 'zh'])('shows the native error details without replacing them with a category (%s)', async locale => {
  const messages = locale === 'zh' ? zh : en
  const message = 'Token refresh failed (non-retryable): token refresh failed: status 400, body: {"error":"invalid_grant","error_description":"account_on_hold","error_uri":"https://claude.ai/restricted"}'
  api.accounts.mockResolvedValue({ items: [{ ...account, status: 'error', error_message: message, failure_category: 'authentication' }], total: 1 })
  const wrapper = await openDetails(locale, messages, false, { admin: false })
  const indicator = wrapper.get('[data-account-status]').getComponent<StatusInstance>({ name: 'AccountStatusIndicator' })
  expect(indicator.props('account').error_message).toBe(message)
  expect(indicator.get('.badge').attributes('title')).toBe(message)
  const help = indicator.findComponent({ name: 'HelpTooltip' })
  await help.trigger('mouseenter')
  await flushPromises()
  expect(help.get('[role="tooltip"]').text()).toBe(message)
  wrapper.unmount()
})

it.each([true, false])('matches the native disabled scheduling flag on errored accounts: admin=%s', async admin => {
  const errored = { ...account, status: 'error', platform_schedulable: false, schedulable: false, failure_category: 'authentication' }
  api.accounts.mockResolvedValue({ items: [errored], total: 1, overview: { total: 1, abnormal: 1, disabled: 0 } })
  const wrapper = await openDetails('en', en, false, { admin, vendorId: 8 })
  const cell = wrapper.get('[data-account-status]')
  expect(cell.getComponent({ name: 'Toggle' }).props('modelValue')).toBe(false)
  if (!admin) {
    expect(cell.getComponent({ name: 'Toggle' }).attributes('disabled')).toBeDefined()
    cell.getComponent({ name: 'Toggle' }).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(api.accountAction).not.toHaveBeenCalled()
  }
  expect(cell.getComponent<StatusInstance>({ name: 'AccountStatusIndicator' }).props('account').status).toBe('error')
  wrapper.unmount()
})

it.each([
  ['en', en, ['1K', '110M', '2.2M'], ['2', '404K', '34K']],
  ['zh', zh, ['1000', '1.1亿', '217.6万'], ['2', '40.4万', '3.4万']],
  ['zh-TW', zhTW, ['1000', '1.1億', '217.6萬'], ['2', '40.4萬', '3.4萬']],
  ['ja', ja, ['1000', '1.1億', '217.6万'], ['2', '40.4万', '3.4万']]
] as const)('steps throughput units using the current locale (%s)', async (locale, messages, global, own) => {
  api.accounts.mockResolvedValue({
    items: [account], total: 1,
    global_overview: { rpm: 1000, tpm_with_cache: 110000000, tpm: 2176000 },
    overview: { total: 1, rpm: 2, tpm_with_cache: 404000, tpm: 34000 }
  })
  const w = await openDetails(locale, messages)
  expect(w.get('[data-global-overview]').findAll('strong').slice(0, 3).map(n => n.text())).toEqual(global)
  const cards = w.get('#account-overview').findAll('.text-lg')
  expect(cards.slice(0, 3).map(n => n.text())).toEqual(own)
  const refresh = w.findAllComponents<SelectInstance>({ name: 'Select' }).find(c => c.props('ariaLabel') === messages.autoRefresh)!
  for (const value of ['60', '120', '300', '0']) {
    refresh.vm.$emit('update:modelValue', value)
    await flushPromises()
    expect(refresh.get('.select-value').text()).toBe(value === '0'
      ? messages.autoRefreshOff
      : messages.refreshMinutes.replace('{count}', String(Number(value) / 60)))
  }
  w.unmount()
})

it('switches throughput units at locale thresholds and when the language changes', async () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { vendor: en }, zh: { vendor: zh } } })
  api.accounts.mockResolvedValue({ items: [], total: 0, global_overview: { rpm: 999, tpm_with_cache: 999950, tpm: 1000000000 } })
  const w = mount(AccountWorkbench, {
    props: { scope: { admin: false } },
    global: { plugins: [i18n], stubs: { Pagination: true, TotpStepUpDialog: true, AccountForm: true, EditAccountModal: true, BulkEditAccountModal: true, AccountImport: true, UsageWorkbench: true } }
  })
  await flushPromises()
  const metrics = () => w.get('[data-global-overview]').findAll('strong').slice(0, 3).map(n => n.text())
  expect(metrics()).toEqual(['999', '1M', '1B'])
  i18n.global.locale.value = 'zh'
  await flushPromises()
  expect(metrics()).toEqual(['999', '100万', '10亿'])
  api.accounts.mockResolvedValue({ items: [], total: 0, global_overview: { rpm: 9999, tpm_with_cache: 10000, tpm: 99999500 } })
  await w.findAll('button').find(b => b.text() === zh.refresh)!.trigger('click')
  await flushPromises()
  expect(metrics()).toEqual(['9999', '1万', '1亿'])
  w.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})

vi.mock('../panels', async () => {
  const { EditAccountModal, BulkEditAccountModal } = await import('@/features/resources/panels')
  return { useVendorPortalResourcePanels: () => ({ editAccount: EditAccountModal, bulkEditAccounts: BulkEditAccountModal }) }
})

it.each([undefined, ''])('does not synthesize native error text from a supplier category (%s)', async errorMessage => {
  api.accounts.mockResolvedValue({ items: [{ ...account, status: 'error', error_message: errorMessage, failure_category: 'authentication' }], total: 1 })
  const wrapper = await openDetails('en', en, false, { admin: false })
  const indicator = wrapper.get('[data-account-status]').getComponent<StatusInstance>({ name: 'AccountStatusIndicator' })
  expect(indicator.props('account').error_message).toBe(errorMessage)
  expect(indicator.props('account').failure_category).toBe('authentication')
})

it.each([
  { role: 'super_admin', isSuperAdmin: true, expected: [5, 16] },
  { role: 'admin', isSuperAdmin: false, expected: [5] },
])('projects management group candidates from the actual $role identity', async actor => {
  (useSupplyServices().identity as Ref<SupplyIdentity>).value = { userId: 1, role: actor.role, isSuperAdmin: actor.isSuperAdmin }
  native.get.mockImplementation(async (url: string) => ({ data: url.startsWith('/admin/vendors/')
    ? { id: 8, group_ids: [5], groups: [{ id: 5, name: 'Existing grant', platform: 'openai' }] }
    : { ...account, id: 12, vendor_id: 8, credentials: {}, extra: {} } }))
  native.groups.mockResolvedValue([{ id: 5, name: 'Existing grant', platform: 'openai', status: 'active' }, { id: 16, name: 'New composite grant', platform: 'composite', status: 'active' }])
  const wrapper = await openDetails('en', en, false, { admin: true, vendorId: 8 })
  const edit = wrapper.findAll('button').find(item => item.attributes('aria-label') === en.edit || item.text() === en.edit)
  expect(edit).toBeDefined()
  await edit!.trigger('click')
  await flushPromises()
  const editor = wrapper.getComponent(EditAccountModal)
  expect(editor.props('groups').map((group: { id: number }) => group.id)).toEqual(actor.expected)
  expect(native.put).not.toHaveBeenCalled()
  if (actor.isSuperAdmin) {
    await required(editor.props('saveAccount'), 'management save callback')(12, { group_ids: [5, 16] })
    expect(native.put).toHaveBeenCalledWith('/admin/accounts/12', { group_ids: [5, 16] }, { signal: expect.any(AbortSignal), params: { vendor_id: 8, view: 'management' } })
    expect(native.post).not.toHaveBeenCalled()
  }
})

it('invalidates a pending management option read when the actor loses platform authority', async () => {
  let finish!: (value: object[]) => void
  native.groups.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  const wrapper = await openDetails('en', en, false, { admin: true, vendorId: 8 })
  const edit = wrapper.findAll('button').find(item => item.attributes('aria-label') === en.edit || item.text() === en.edit)
  expect(edit).toBeDefined()
  await edit!.trigger('click')
  await flushPromises()
  const signal = native.groups.mock.calls[0][1].signal as AbortSignal
  ;(useSupplyServices().identity as Ref<SupplyIdentity>).value = { userId: 1, role: 'admin', isSuperAdmin: false }
  finish([{ id: 16, name: 'Old actor candidate', platform: 'composite' }])
  await flushPromises()
  expect(signal.aborted).toBe(true)
  expect(wrapper.findComponent(EditAccountModal).exists()).toBe(false)
  expect(native.put).not.toHaveBeenCalled()
})

it('uses the existing native bulk management route for an explicit super-admin supplier group assignment', async () => {
  native.get.mockImplementation(async (url: string) => ({ data: url.startsWith('/admin/vendors/')
    ? { id: 8, group_ids: [5], groups: [] }
    : { ...account, id: 12, vendor_id: 8, credentials: {}, extra: {} } }))
  native.groups.mockResolvedValue([{ id: 5, name: 'Existing grant', platform: 'openai', status: 'active' }, { id: 16, name: 'Composite grant', platform: 'composite', status: 'active' }])
  native.post.mockResolvedValue({ data: { success: 1, failed: 0, results: [] } })
  const wrapper = await openDetails('en', en, false, { admin: true, vendorId: 0, readOnly: true })
  await wrapper.get('tbody input[type=checkbox]').setValue(true)
  await wrapper.get('[data-testid="vendor-bulk-edit"]').trigger('click')
  await flushPromises()
  const editor = wrapper.getComponent(BulkEditAccountModal)
  expect(editor.props('groups').map((group: { id: number }) => group.id)).toEqual([5, 16])
  expect(native.post).not.toHaveBeenCalled()
  await required(editor.props('saveBatch'), 'bulk management save callback')([12], { group_ids: [16] })
  expect(native.post).toHaveBeenCalledTimes(1)
  expect(native.post).toHaveBeenCalledWith('/admin/accounts/bulk-update', { account_ids: [12], group_ids: [16] }, { signal: expect.any(AbortSignal), params: { vendor_id: 0, view: 'management' } })
  expect(api.updateAccount).not.toHaveBeenCalled()
})
