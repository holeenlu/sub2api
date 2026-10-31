import { uiFeedbackKey } from '@/shared/ui/feedback'
import { identityNotificationsKey } from '@/features/identity'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import { useAppStore as useTestAppStore } from '@/app/model/app'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

import { reactive } from 'vue'
import AccountPriorityCell from '@/features/resources/components/AccountPriorityCell.vue'

import AccountsView from '../AccountsPage.vue'

enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })

const { listAccounts, priorityUpdate, showPriorityError } = vi.hoisted(() => ({
  listAccounts: vi.fn(), priorityUpdate: vi.fn(), showPriorityError: vi.fn()
}))

const accountsApiFixture = vi.hoisted(() => ({
      list: listAccounts,
      listWithEtag: vi.fn(),
      getBatchTodayStats: vi.fn().mockResolvedValue({ stats: {} }),
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn().mockResolvedValue([]) }))
const groupsApiFixture = vi.hoisted(() => ({ getAll: vi.fn().mockResolvedValue([]) }))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({ showError: showPriorityError, showSuccess: vi.fn(), showInfo: vi.fn() })
}))

const actor = reactive({
  identityScopeKey: 'actor-a', isAuthenticated: true, isSuperAdmin: false, isSimpleMode: false,
  user: { id: 7, role: 'admin', policy_version: 1 },
  manage: true, priority: true,
  can(permission: string): boolean { return actor.user.role === 'admin' && actor.manage && permission === 'accounts.manage' },
  canEditAdminField(schema: string, field: string): boolean {
    return actor.user.role === 'admin' && actor.priority && schema === 'account.update' && field === 'priority'
  }
})
vi.mock('@/features/resources/accounts', async importOriginal => ({
  ...await importOriginal<typeof import('@/features/resources/accounts')>(), update: priorityUpdate
}))

vi.mock('@/features/identity', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/identity')>()),
  useAuthStore: () => actor
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div data-test="data-table">
      <span v-for="column in columns" :key="column.key" :data-column="column.key">
        {{ column.sortable ? 'sortable' : 'fixed' }}
      </span>
      <div v-for="row in data" :key="row.id" :data-account-id="row.id">
        <slot name="cell-priority" :row="row" />
      </div>
      <button data-test="sort-priority" @click="$emit('sort', 'priority', 'desc')" />
    </div>
  `
}

function mountView() {
  return mount(AccountsView, {
    global: {
        provide: {
          [identityNotificationsKey as symbol]: useTestAppStore(),
          [uiFeedbackKey as symbol]: { success: useTestAppStore().showSuccess, error: useTestAppStore().showError },
          [resourceEditorServicesKey as symbol]: editorTestServices()
        },
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        AccountTableActions: { template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

describe('admin AccountsView priority column preferences', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.assign(actor, { identityScopeKey: 'actor-a', isAuthenticated: true, manage: true, priority: true })
    Object.assign(actor.user, { id: 7, role: 'admin', policy_version: 1 })
    showPriorityError.mockReset()
    priorityUpdate.mockReset().mockImplementation(async (id: number, payload: { priority: number }) => ({
      id, name: 'Account ' + id, platform: 'anthropic', type: 'oauth', priority: payload.priority,
      group_ids: [], extra: {}, credentials: {}
    }))
    listAccounts.mockReset().mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
  })

  it('shows priority as a sortable column for fresh preferences', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-column="priority"]').text()).toBe('sortable')

    await wrapper.get('[data-test="sort-priority"]').trigger('click')
    await flushPromises()

    expect(listAccounts).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ sort_by: 'priority', sort_order: 'desc' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('preserves an existing preference that explicitly hides priority', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['priority', 'today_stats']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-column="priority"]').exists()).toBe(false)
    expect(JSON.parse(localStorage.getItem('account-hidden-columns') || '[]')).toEqual([
      'priority',
      'today_stats'
    ])
  })

  it('keeps priority visible while migrating older saved preferences', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['today_stats']))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-column="priority"]').text()).toBe('sortable')
    expect(JSON.parse(localStorage.getItem('account-hidden-columns') || '[]')).toEqual(
      expect.arrayContaining(['today_stats', 'scheduler_score'])
    )
    expect(JSON.parse(localStorage.getItem('account-hidden-columns') || '[]')).not.toContain('priority')
  })
  async function mountPriorityRows() {
    vi.useFakeTimers()
    listAccounts.mockResolvedValue({
      items: [7, 8].map(id => ({ id, name: 'Account ' + id, platform: 'anthropic', type: 'oauth', priority: 3, group_ids: [], extra: {} })),
      total: 2, page: 1, page_size: 20, pages: 1
    })
    const wrapper = mountView()
    await flushPromises()
    return wrapper
  }

  it.each(['role', 'manage', 'field'] as const)('keeps real priority cells read-only when %s denies editing', async denied => {
    if (denied === 'role') actor.user.role = 'user'
    else if (denied === 'manage') actor.manage = false
    else actor.priority = false
    const wrapper = await mountPriorityRows()
    const cell = wrapper.findAllComponents(AccountPriorityCell)[0]
    expect(cell.get('[data-testid="account-priority-value"]').text()).toBe('3')
    const increment = cell.get('[data-testid="account-priority-increment"]')
    expect(increment.attributes('disabled')).toBeDefined()
    await increment.trigger('click')
    await vi.advanceTimersByTimeAsync(500)
    expect(priorityUpdate).not.toHaveBeenCalled()
  })

  it.each(['actor', 'manage', 'field', 'unmount'] as const)('cancels a real table cell pending intent on %s', async boundary => {
    const wrapper = await mountPriorityRows()
    await wrapper.findAllComponents(AccountPriorityCell)[0].get('[data-testid="account-priority-increment"]').trigger('click')
    if (boundary === 'actor') actor.identityScopeKey = 'actor-b'
    else if (boundary === 'manage') actor.manage = false
    else if (boundary === 'field') actor.priority = false
    else wrapper.unmount()
    await vi.advanceTimersByTimeAsync(500)
    expect(priorityUpdate).not.toHaveBeenCalled()
    expect(showPriorityError).not.toHaveBeenCalled()
  })

  it('updates different priority rows independently through the real cell and page patch consumer', async () => {
    const wrapper = await mountPriorityRows()
    for (const cell of wrapper.findAllComponents(AccountPriorityCell)) {
      await cell.get('[data-testid="account-priority-increment"]').trigger('click')
    }
    await vi.advanceTimersByTimeAsync(450)
    await flushPromises()
    expect(priorityUpdate.mock.calls.map(call => call[0])).toEqual([7, 8])
    expect(priorityUpdate).toHaveBeenCalledWith(7, { priority: 4 }, { signal: expect.any(AbortSignal) })
    expect(wrapper.findAllComponents(AccountPriorityCell).map(cell => cell.props('account').priority)).toEqual([4, 4])
  })

  it('blocks an in-flight cell response from patching the table after permission revocation', async () => {
    let finish: (value: { id: number; priority: number }) => void = () => { throw new Error('Not initialized') }
    priorityUpdate.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = await mountPriorityRows()
    await wrapper.findAllComponents(AccountPriorityCell)[0].get('[data-testid="account-priority-increment"]').trigger('click')
    await vi.advanceTimersByTimeAsync(450)
    const signal = priorityUpdate.mock.calls[0][2].signal
    actor.priority = false
    // The live predicate is observed synchronously, before parent rendering.
    expect(signal.aborted).toBe(true)
    finish({ id: 7, priority: 50 })
    await flushPromises()
    expect(wrapper.findAllComponents(AccountPriorityCell)[0].props('account').priority).toBe(3)
    expect(showPriorityError).not.toHaveBeenCalled()
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
