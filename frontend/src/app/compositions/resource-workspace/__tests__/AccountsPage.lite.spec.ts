import { uiFeedbackKey } from '@/shared/ui/feedback'
import { identityNotificationsKey } from '@/features/identity'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import { useAppStore as useTestAppStore } from '@/app/model/app'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, onMounted } from 'vue'

import AccountsView from '../AccountsPage.vue'
import AccountActionMenu from '@/features/resources/components/AccountActionMenu.vue'

const createLoader = vi.hoisted(() => vi.fn())
vi.mock('@/features/resources/panelLoaders', async original => ({
  ...await original<typeof import('@/features/resources/panelLoaders')>(),
  loadCreateAccountModal: createLoader
}))

const {
  listAccounts,
  listWithEtag,
  getById,
  getBatchTodayStats,
  getBatchUsage,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  refreshCredentials,
  duplicateAccount,
  showError,
  showWarning
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getById: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getBatchUsage: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  refreshCredentials: vi.fn(),
  duplicateAccount: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn()
}))

const accountsApiFixture = vi.hoisted(() => ({
      list: listAccounts,
      getById,
      listWithEtag,
      getBatchTodayStats,
      getBatchUsage,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
      refreshCredentials,
      duplicate: duplicateAccount
    }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: getAllProxies }))
const groupsApiFixture = vi.hoisted(() => ({ getAll: getAllGroups }))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/features/identity', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/identity')>()),
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false, isSuperAdmin: true, can: () => true })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-account-name="row.name">
        <slot name="cell-name" :row="row" :value="row.name" />
        <slot name="cell-groups" :row="row" />
        <slot name="cell-usage" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
})

const AccountGroupsCellStub = defineComponent({
  props: { groups: { type: Array, default: () => [] } },
  template: '<span data-test="account-groups">{{ groups.map(group => group.name).join(",") }}</span>'
})

const EditAccountModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="edit-account">{{ show ? account?.name : "" }}</div>'
})

const AccountTestModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="test-account">{{ show ? account?.name : "" }}</div>'
})

const AccountStatsModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="stats-account">{{ show ? account?.name : "" }}</div>'
})

function mountView(stubActionMenu = true, lazyCreate = false) {
  return mount(AccountsView, {
    attachTo: document.body,
    global: {
        provide: {
          [identityNotificationsKey as symbol]: useTestAppStore(),
          [uiFeedbackKey as symbol]: { success: useTestAppStore().showSuccess, error: useTestAppStore().showError },
          [resourceEditorServicesKey as symbol]: editorTestServices()
        },
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        AccountTableActions: { name: 'AccountTableActions', emits: ['refresh', 'create'], template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: stubActionMenu,
        CodexDiagnosticBadge: { props: ['summary'], template: '<button v-if="summary" data-test="codex-diagnostic-badge">{{ summary.status }}</button>' },
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: AccountTestModalStub,
        AccountStatsModal: AccountStatsModalStub,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: !lazyCreate,
        EditAccountModal: EditAccountModalStub,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: AccountGroupsCellStub,
        AccountUsageCell: true,
        UpstreamBillingRateCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: stubActionMenu
      }
    }
  })
}

const listRow = {
  id: 42,
  name: 'compact row',
  platform: 'openai',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  concurrency: 2,
  priority: 1,
  group_ids: [7],
  extra: {},
  credentials: {}
}

const fullAccount = {
  ...listRow,
  groups: [{ id: 7, name: 'codex', platform: 'openai' }],
  account_groups: [{ account_id: 42, group_id: 7 }],
  credentials: { api_key: 'redacted' },
  extra: { detail_only: true }
}

describe('admin AccountsView lite account list', () => {
  beforeEach(() => {
    createLoader.mockReset()
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({ items: [listRow], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: 'compact-etag', data: null })
    getById.mockReset().mockResolvedValue(fullAccount)
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getBatchUsage.mockReset().mockResolvedValue({ usage: {}, errors: {} })
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: true })
    getAllProxies.mockReset().mockResolvedValue([])
    getAllGroups.mockReset().mockResolvedValue([{ id: 7, name: 'codex', platform: 'openai' }])
    refreshCredentials.mockReset()
    duplicateAccount.mockReset()
    showError.mockReset()
    showWarning.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('keeps lite=1 on the initial list request', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    wrapper.unmount()
  })

  it.each([false, true])('loads the create editor only on intent and rejects a late mount after disposal=%s', async dispose => {
    const mounted = vi.fn()
    const Editor = defineComponent({
      name: 'CreateAccountModal',
      emits: ['close'],
      setup() { onMounted(mounted) },
      template: '<button data-test="lazy-create" @click="$emit(\'close\')">Close</button>'
    })
    let resolve!: (component: typeof Editor) => void
    createLoader.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountView(true, true)
    await flushPromises()
    expect(createLoader).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="lazy-create"]').exists()).toBe(false)
    wrapper.getComponent({ name: 'AccountTableActions' }).vm.$emit('create')
    await flushPromises()
    expect(createLoader).toHaveBeenCalledOnce()
    expect(mounted).not.toHaveBeenCalled()
    if (dispose) wrapper.unmount()
    resolve(Editor)
    await flushPromises()
    if (dispose) {
      expect(mounted).not.toHaveBeenCalled()
    } else {
      expect(mounted).toHaveBeenCalledOnce()
      await wrapper.get('[data-test="lazy-create"]').trigger('click')
      expect(wrapper.find('[data-test="lazy-create"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('maps group_ids through the group catalog for the table cell', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="account-groups"]').text()).toBe('codex')
    wrapper.unmount()
  })

  it('keeps direct reset usage when an older desktop batch finishes later', async () => {
    vi.useFakeTimers()
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() } as unknown as MediaQueryList)
    const oldUsage = { seven_day_fable: { utilization: 50, resets_at: null, remaining_seconds: 0 } }
    const freshUsage = { seven_day_fable: { utilization: 0, resets_at: null, remaining_seconds: 0 } }
    let finishBatch!: (value: { usage: Record<string, typeof oldUsage>; errors: Record<string, string> }) => void
    getBatchUsage.mockReturnValueOnce(new Promise(resolve => { finishBatch = resolve }))
    const row = { ...listRow, platform: 'anthropic' }
    listAccounts.mockResolvedValue({ items: [row], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    const cell = wrapper.getComponent({ name: 'AccountUsageCell' })
    cell.props('requestBatchedUsage')(row)
    await vi.advanceTimersByTimeAsync(0)
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    cell.vm.$emit('usage-loaded', freshUsage)
    await flushPromises()
    finishBatch({ usage: { '42': oldUsage }, errors: {} })
    await flushPromises()
    expect(cell.props('batchedUsage')).toEqual(freshUsage)
    expect(cell.props('batchedUsageLoading')).toBe(false)
    cell.props('requestBatchedUsage')(row)
    await flushPromises()
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    expect(cell.props('batchedUsage')).toEqual(freshUsage)
    wrapper.unmount()
  })

  it('renders the latest diagnostic conclusion beneath the account name', async () => {
    listAccounts.mockResolvedValue({
      items: [{ ...listRow, codex_diagnostic: { status: 'degraded', checked_at: '2026-10-05T02:00:00Z' } }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="codex-diagnostic-badge"]').text()).toBe('degraded')
    wrapper.unmount()
  })

  it('keeps the action menu open during internal scrolling but closes it on table scrolling', async () => {
    const wrapper = mountView(false)
    await flushPromises()

    const trigger = wrapper.findAll('button').find(button => button.text() === 'common.more')!
    await trigger.trigger('click')
    const menu = new DOMWrapper(document.body.querySelector('.action-menu-content')!)
    menu.element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    menu.get('button').element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    wrapper.getComponent(DataTableStub).element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('keeps lite=1 on automatic ETag refreshes', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const wrapper = mountView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()

    expect(listWithEtag).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ etag: null })
    )
    wrapper.unmount()
  })

  it.each([
    ['state', { state: 'failed' }],
    ['cycle flag', { last_result_current_cycle: true }],
    ['next probe', { next_probe_at: '2099-01-01T00:00:00Z' }],
    ['attempt time', { last_result: { attempted_at: '2026-10-28T01:00:00Z' } }],
    ['request id', { last_result: { upstream_request_id: 'req-next' } }],
    ['error code', { last_result: { error_code: 'account_deactivated' } }],
    ['error type', { last_result: { error_type: 'permission_error' } }],
    ['removed projection', null],
  ])('refreshes the actual list on BPS %s changes with unchanged updated_at', async (_name, change) => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const recovery = { state: 'pending', interval_minutes: 60, last_result_current_cycle: false,
      last_result: { status: 'failed', stage: 'request', reason: 'http_rejected', duration_ms: 42, completed_at: '2026-10-27T00:00:00Z' } }
    const row = { ...listRow, updated_at: '2026-10-28T00:00:00Z', excel_bps_403_recovery: recovery }
    const next = { ...row, excel_bps_403_recovery: change === null ? null : {
      ...recovery, ...change,
      last_result: { ...recovery.last_result, ...('last_result' in change ? change.last_result : {}) },
    } }
    listAccounts.mockResolvedValue({ items: [row], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockResolvedValueOnce({ notModified: false, etag: 'bps-updated', data: { items: [next], total: 1, pages: 1 } })
    const wrapper = mountView()
    try {
      await flushPromises()
      const before = wrapper.getComponent(DataTableStub).props('data')[0]
      await vi.advanceTimersByTimeAsync(6000)
      await flushPromises()
      expect(listWithEtag).toHaveBeenCalledOnce()
      const after = wrapper.getComponent(DataTableStub).props('data')[0]
      expect(after.updated_at).toBe(before.updated_at)
      expect(after).not.toBe(before)
      expect(after.excel_bps_403_recovery).toEqual(next.excel_bps_403_recovery)
      if (_name === 'state') expect(wrapper.get('[data-test="bps-failure-badge"]').text()).toBe('admin.accounts.openai.excelBPSRecoveryFailed')
    } finally {
      wrapper.unmount()
    }
  })

  it('retains the current row for an identical BPS recovery projection', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const row = { ...listRow, updated_at: '2026-10-28T00:00:00Z', excel_bps_403_recovery: {
      state: 'pending', interval_minutes: 60, last_result_current_cycle: false,
    } }
    listAccounts.mockResolvedValue({ items: [row], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockResolvedValueOnce({ notModified: false, etag: 'same-bps', data: { items: [JSON.parse(JSON.stringify(row))], total: 1, pages: 1 } })
    const wrapper = mountView()
    try {
      await flushPromises()
      const before = wrapper.getComponent(DataTableStub).props('data')[0]
      await vi.advanceTimersByTimeAsync(6000)
      await flushPromises()
      expect(listWithEtag).toHaveBeenCalledOnce()
      expect(wrapper.getComponent(DataTableStub).props('data')[0]).toBe(before)
    } finally {
      wrapper.unmount()
    }
  })

  it('shows a failed list read and clears it after an explicit refresh', async () => {
    listAccounts.mockRejectedValueOnce({ message: 'List is temporarily unavailable' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('List is temporarily unavailable')
    wrapper.getComponent({ name: 'AccountTableActions' }).vm.$emit('refresh')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[data-account-name]').attributes('data-account-name')).toBe(listRow.name)
    wrapper.unmount()
  })

  it('loads the full account by id before opening edit, test, and stats actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    expect(getById).toHaveBeenCalledWith(42, { signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-test="edit-account"]').text()).toBe('compact row')

    const menu = wrapper.findComponent(AccountActionMenu)
    menu.vm.$emit('test', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="test-account"]').text()).toBe('compact row')

    menu.vm.$emit('stats', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-test="stats-account"]').text()).toBe('compact row')
    wrapper.unmount()
  })

  it('cancels a pending ETag refresh when filters change and ignores its late response', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    let resolveRefresh!: (value: unknown) => void
    listWithEtag.mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    const signal = listWithEtag.mock.calls[0][3].signal as AbortSignal
    expect(signal.aborted).toBe(false)
    listAccounts.mockResolvedValueOnce({ items: [{ ...listRow, name: 'new filter row' }], total: 1, pages: 1 })
    const panel = wrapper.getComponent({ name: 'AccountTableFilters' })
    panel.vm.$emit('update:searchQuery', 'new filter')
    panel.vm.$emit('change')
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    resolveRefresh({ etag: 'old-query', notModified: false, data: { items: [{ ...listRow, name: 'old filter row' }], total: 99, pages: 5 } })
    await flushPromises()
    expect(wrapper.get('[data-account-name]').attributes('data-account-name')).toBe('new filter row')
    wrapper.unmount()
  })

  it('stops a refresh on unmount even if its transport ignores abort', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    let resolveRefresh!: (value: unknown) => void
    listWithEtag.mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    const signal = listWithEtag.mock.calls[0][3].signal as AbortSignal
    wrapper.unmount()
    const statisticsCalls = getBatchTodayStats.mock.calls.length
    expect(signal.aborted).toBe(true)
    resolveRefresh({ notModified: false, etag: 'disposed', data: { items: [listRow], total: 1, pages: 1 } })
    await flushPromises()
    expect(getBatchTodayStats).toHaveBeenCalledTimes(statisticsCalls)
  })

  it('shows the warning and patches the account after a partial Antigravity refresh', async () => {
    refreshCredentials.mockResolvedValue({
      account: { ...fullAccount, name: 'refreshed account' },
      message: 'Token refreshed, but project_id is temporarily unavailable',
      warning: 'missing_project_id_temporary'
    })
    const wrapper = mountView(false)
    await flushPromises()

    wrapper.findComponent(AccountActionMenu).vm.$emit('refresh-token', listRow)
    await flushPromises()

    expect(refreshCredentials).toHaveBeenCalledWith(42, { signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-account-name]').attributes('data-account-name')).toBe('refreshed account')
    expect(showWarning).toHaveBeenCalledWith('Token refreshed, but project_id is temporarily unavailable')
    wrapper.unmount()
  })

  it('shows an error and keeps the modal closed when detail loading fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getById.mockRejectedValueOnce(new Error('detail failed'))
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    await editButton!.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('detail failed')
    expect(wrapper.find('[data-test="edit-account"]').exists()).toBe(false)
    consoleError.mockRestore()
    wrapper.unmount()
  })

  it.each(['42', 'ungrouped', '', null])('projects concrete filters and preserves group=%s in the existing query', async group => {
    vi.useFakeTimers()
    const wrapper = mountView()
    await flushPromises()
    const panel = wrapper.getComponent({ name: 'AccountTableFilters' })
    expect(panel.props('filters')).toEqual({ platform: '', type: '', status: '', privacy_mode: '', group: '' })
    panel.vm.$emit('update:searchQuery', 'needle')
    panel.vm.$emit('update:filters', { platform: 'openai', type: 'oauth', status: '', privacy_mode: '__unset__', group })
    panel.vm.$emit('change')
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(listAccounts).toHaveBeenLastCalledWith(1, 20,
      expect.objectContaining({ group, search: 'needle', lite: '1', privacy_mode: '__unset__' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(Object.keys(panel.props('filters')).sort()).toEqual(['group', 'platform', 'privacy_mode', 'status', 'type'])
    expect(panel.props('searchQuery')).toBe('needle')
    wrapper.unmount()
  })

  it.each([
    { error: new Error('known failure'), message: 'known failure' },
    { error: { message: 'flat API failure' }, message: 'flat API failure' },
    { error: { message: { unsafe: true } }, message: 'admin.accounts.duplicateFailed' },
    { error: { message: 403 }, message: 'admin.accounts.duplicateFailed' },
    { error: null, message: 'admin.accounts.duplicateFailed' }
  ])('narrows duplicate error display without replacing the original error ($message)', async ({ error, message }) => {
    duplicateAccount.mockRejectedValueOnce(error)
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = mountView()
    await flushPromises()
    wrapper.findComponent(AccountActionMenu).vm.$emit('duplicate', listRow)
    await flushPromises()
    expect(duplicateAccount).toHaveBeenCalledWith(listRow.id, { signal: expect.any(AbortSignal) })
    expect(showError).toHaveBeenCalledWith(message)
    expect(logged).toHaveBeenCalledWith('Failed to duplicate account:', error)
    wrapper.unmount()
  })

})

vi.mock('@/features/resources', async importOriginal => {
  const actual = await importOriginal<typeof import('@/features/resources')>()
  
  return { ...actual, accountsAPI: accountsApiFixture, proxiesAPI: proxiesApiFixture }
})
vi.mock('@/features/access-policy', async importOriginal => {
  const actual = await importOriginal<typeof import('@/features/access-policy')>()
  
  return { ...actual, groupsAPI: groupsApiFixture }

})
