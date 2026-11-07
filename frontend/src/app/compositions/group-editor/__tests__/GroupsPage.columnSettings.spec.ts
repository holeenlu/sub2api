import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import GroupsView from '../GroupsPage.vue'

const {
  listGroups,
  getAllGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  showError,
  showSuccess,
  isCurrentStep,
  nextStep,
  authState,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  authState: { isSimpleMode: false, isSuperAdmin: true, can: () => true },
}))

const messages: Record<string, string> = {
  'admin.groups.columnSettings': 'Column Settings',
  'admin.groups.columns.name': 'Name',
  'admin.groups.columns.id': 'ID',
  'admin.groups.columns.platform': 'Platform',
  'admin.groups.columns.billingType': 'Billing Type',
  'admin.groups.columns.rateMultiplier': 'Rate Multiplier',
  'admin.groups.columns.type': 'Type',
  'admin.groups.columns.accounts': 'Accounts',
  'admin.groups.columns.capacity': 'Capacity',
  'admin.groups.columns.usage': 'Usage',
  'admin.groups.columns.status': 'Status',
  'admin.groups.columns.actions': 'Actions',
  'admin.groups.usageToday': 'Today',
  'admin.groups.usageYesterday': 'Yesterday',
  'admin.groups.usageTotal': 'Total',
}

const groupsApiFixture = vi.hoisted(() => ({
      list: listGroups,
      getAll: getAllGroups,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn(),
    }))
const accountsApiFixture = vi.hoisted(() => ({
      list: listAccounts,
    }))
const usersApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected users.list request in this test') }) }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn(() => { throw new Error('Unexpected proxies.getAll request in this test') }) }))
const settingsApiFixture = vi.hoisted(() => ({ getSettings: vi.fn(() => { throw new Error('Unexpected settings.getSettings request in this test') }), updateSettings: vi.fn(() => { throw new Error('Unexpected settings.updateSettings request in this test') }) }))
vi.mock('@/features/resources/accounts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/resources/accounts')>()
  return { ...actual, default: accountsApiFixture }
})

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/features/identity', () => ({
  useAuthStore: () => authState,
}))

vi.mock('@/app/onboarding/store', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 1,
  name: 'Core Anthropic',
  description: null,
  platform: 'anthropic',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 3,
  active_account_count: 2,
  rate_limited_account_count: 1,
  model_allowlist: undefined,
  sort_order: 10,
  ...overrides,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="rows">{{ data.map((row) => row.name).join(',') }}</div>
      <div v-if="data.length" data-test="usage-cell">
        <slot name="cell-usage" :row="data[0]" />
      </div>
    </div>
  `,
}

const SelectStub = {
  props: ['modelValue', 'options', 'placeholder'],
  emits: ['update:modelValue', 'change'],
  template: `
    <select
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value); $emit('change')"
    >
      <option v-for="option in options" :key="String(option.value)" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `,
}

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: IconStub,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: { template: '<div><slot /></div>' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

const columnKeys = (wrapper: ReturnType<typeof mount>) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const openColumnSettings = async (wrapper: ReturnType<typeof mount>) => {
  await wrapper.get('button[title="Column Settings"]').trigger('click')
}

const clickColumnToggle = async (wrapper: ReturnType<typeof mount>, label: string) => {
  const button = wrapper
    .findAll('button')
    .find((item) => item.text().includes(label))
  expect(button, `column toggle ${label}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

describe('admin GroupsView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listGroups.mockReset()
    getAllGroups.mockReset()
    getModelAllowlistCandidates.mockReset()
    getUsageSummary.mockReset()
    getCapacitySummary.mockReset()
    getLiveCapability.mockReset()
    listAccounts.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()
    authState.isSimpleMode = false

    listGroups.mockResolvedValue({
      items: [createGroup()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getAllGroups.mockResolvedValue([])
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    isCurrentStep.mockReturnValue(false)
  })

  it('does not call advanced group APIs or expose the exclusive filter in simple mode', async () => {
    authState.isSimpleMode = true
    const wrapper = await mountView()

    expect(getLiveCapability).not.toHaveBeenCalled()
    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()
    expect(listGroups).toHaveBeenCalledWith(
      expect.any(Number),
      expect.any(Number),
      expect.objectContaining({ is_exclusive: undefined }),
      expect.anything(),
    )
    expect(wrapper.find('select').text()).not.toContain('admin.groups.allGroups')
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('hides the id column by default while keeping other group columns visible', async () => {
    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual([
      'name',
      'platform',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'account_count',
      'capacity',
      'usage',
      'status',
      'actions',
    ])
    expect(localStorage.getItem('group-hidden-columns')).toBe(JSON.stringify(['id']))
    expect(localStorage.getItem('group-column-settings-version')).toBe('2')
  })

  it('applies saved hidden columns on mount and ignores unknown keys', async () => {
    localStorage.setItem(
      'group-hidden-columns',
      JSON.stringify(['usage', 'capacity', 'removed_column', 'name', 'actions']),
    )
    localStorage.setItem('group-column-settings-version', '2')

    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual([
      'name',
      'id',
      'platform',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'account_count',
      'status',
      'actions',
    ])
  })

  it('auto-hides id for existing saved column prefs after version bump', async () => {
    localStorage.setItem('group-hidden-columns', JSON.stringify(['usage']))
    // No version key → treated as version 1, migrate to 2 and hide id.

    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual([
      'name',
      'platform',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'account_count',
      'capacity',
      'status',
      'actions',
    ])
    expect(JSON.parse(localStorage.getItem('group-hidden-columns')!)).toEqual(
      expect.arrayContaining(['usage', 'id']),
    )
    expect(localStorage.getItem('group-column-settings-version')).toBe('2')
  })

  it('toggles a column and persists hidden column keys', async () => {
    const wrapper = await mountView()

    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')

    expect(columnKeys(wrapper)).toEqual([
      'name',
      'platform',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'account_count',
      'capacity',
      'status',
      'actions',
    ])
    expect(JSON.parse(localStorage.getItem('group-hidden-columns')!)).toEqual(
      expect.arrayContaining(['id', 'usage']),
    )
  })

  it('can show the id column from column settings', async () => {
    const wrapper = await mountView()

    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'ID')

    expect(columnKeys(wrapper)).toEqual([
      'name',
      'id',
      'platform',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'account_count',
      'capacity',
      'usage',
      'status',
      'actions',
    ])
    expect(localStorage.getItem('group-hidden-columns')).toBe(JSON.stringify([]))
  })

  it('skips usage and capacity fetches until consuming columns are shown', async () => {
    localStorage.setItem(
      'group-hidden-columns',
      JSON.stringify(['billing_type', 'usage', 'capacity']),
    )

    const wrapper = await mountView()

    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()

    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getUsageSummary).toHaveBeenCalledWith()
    expect(getCapacitySummary).not.toHaveBeenCalled()

    await clickColumnToggle(wrapper, 'Capacity')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
  })

  it('renders yesterday usage between today and total', async () => {
    getUsageSummary.mockResolvedValue([
      { group_id: 1, today_cost: 1.25, yesterday_cost: 2.5, total_cost: 9.75 },
    ])

    const wrapper = await mountView()
    const text = wrapper.get('[data-test="usage-cell"]').text()

    expect(text).toContain('Today$1.25')
    expect(text).toContain('Yesterday$2.50')
    expect(text).toContain('Total$9.75')
    expect(text.indexOf('Today')).toBeLessThan(text.indexOf('Yesterday'))
    expect(text.indexOf('Yesterday')).toBeLessThan(text.indexOf('Total'))
  })
})


import { config as testConfig } from '@vue/test-utils'
import { ref as testRef, computed as testComputed } from 'vue'
import { groupPresentationKey } from '@/features/access-policy/presentation/context'
import { peakRatePresentation } from '@/features/pricing'
import { platformLabel } from '@/features/catalog'

import { useAppStore as mockedApp } from '@/app/model/app'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, accessPolicyUsersKey, riskControlProxiesKey, riskControlSettingsKey, accessPolicyModelSelectorKey } from '@/features/access-policy/services'
vi.mock('@/features/access-policy/api/groups', async () => ({ groupsAPI: groupsApiFixture }))

const permissionFallback = { isSuperAdmin: true, isSimpleMode: false, can: () => true, canEditAdminField: () => true, editableAdminFields: (_schema: string, payload: object) => payload }
const mockedPermissions = new Proxy(permissionFallback, { get(target, key) { return readPermissions()[key as keyof typeof target] ?? target[key as keyof typeof target] } })
Object.assign(testConfig.global.provide, {
 [groupPresentationKey as symbol]: { peakRate: peakRatePresentation, platformLabel, serverUtcOffset: testRef(undefined), isSimpleMode: testComputed(() => mockedPermissions.isSimpleMode) },
 [accessPolicyScopeKey as symbol]: testRef('session:admin:test'),
 [accessPolicyNotificationsKey as symbol]: new Proxy({}, { get(_target, key) { return mockedApp()[key as 'showError' | 'showSuccess'] } }),
 [accessPolicyPermissionsKey as symbol]: mockedPermissions,
 [accessPolicyAccountsKey as symbol]: { search: async (keyword: string, params: object, signal: AbortSignal) => (await accountsApiFixture.list(1, 20, { search: keyword, ...params }, { signal })).items, getById: (id: number) => accountsApiFixture.getById(id) },
 [accessPolicyUsersKey as symbol]: { search: async (keyword: string) => (await usersApiFixture.list(1, 10, { search: keyword })).items },
 [riskControlProxiesKey as symbol]: () => proxiesApiFixture.getAll(),
 [riskControlSettingsKey as symbol]: { enable: async () => { const current = await settingsApiFixture.getSettings(); await settingsApiFixture.updateSettings({ risk_control_enabled: true, revision: current.revision || '' }) } },
 [accessPolicyModelSelectorKey as symbol]: { name: 'ModelWhitelistSelector', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }
})
import { useAuthStore as testAuth } from '@/features/identity'
function readPermissions() { return testAuth() }

import { pricingScopeKey } from '@/features/pricing'
Object.assign(testConfig.global.provide, { [pricingScopeKey as symbol]: testRef('session:admin:test') })
beforeEach(() => { if (vi.isMockFunction(groupsApiFixture.getAll)) groupsApiFixture.getAll.mockResolvedValue([]) })
