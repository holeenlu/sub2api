import { uiFeedbackKey } from '@/shared/ui/feedback'
import { identityNotificationsKey } from '@/features/identity'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import { useAppStore as useTestAppStore } from '@/app/model/app'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'

import AccountsView from '../AccountsPage.vue'
import AccountActionMenu from '@/features/resources/components/AccountActionMenu.vue'

enableAutoUnmount(afterEach)

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
  showWarning,
  deleteAccount,
  createShadow,
  getModels,
  exportAccounts,
  setSchedulable,
  recoverState,
  resetQuota,
  setPrivacy,
  revertFallback,
  batchDelete,
  batchClearError,
  batchRefresh,
  bulkUpdate,
  showSuccess,
  runStepUp,
  cancelStepUp
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
  showWarning: vi.fn(),
  deleteAccount: vi.fn(),
  createShadow: vi.fn(),
  getModels: vi.fn(),
  exportAccounts: vi.fn(),
  setSchedulable: vi.fn(),
  recoverState: vi.fn(),
  resetQuota: vi.fn(),
  setPrivacy: vi.fn(),
  revertFallback: vi.fn(),
  batchDelete: vi.fn(),
  batchClearError: vi.fn(),
  batchRefresh: vi.fn(),
  bulkUpdate: vi.fn(),
  showSuccess: vi.fn(),
  runStepUp: vi.fn(),
  cancelStepUp: vi.fn()
}))

const accountsApiFixture = vi.hoisted(() => ({
      list: listAccounts,
      getById,
      listWithEtag,
      getBatchTodayStats,
      getBatchUsage,
      getUpstreamBillingProbeSettings,
      delete: deleteAccount,
      createSparkShadow: createShadow, getAvailableModels: getModels, exportData: exportAccounts,
      setSchedulable, recoverState, resetAccountQuota: resetQuota, setPrivacy, revertProxyFallback: revertFallback,
      batchDelete, bulkUpdate,
      batchClearError,
      batchRefresh,
      toggleSchedulable: vi.fn(),
      refreshCredentials,
      duplicate: duplicateAccount
    }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: getAllProxies }))
const groupsApiFixture = vi.hoisted(() => ({ getAll: getAllGroups }))

vi.mock('@/app/model/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo: vi.fn() })
}))

const actor = reactive({ token: 'test-token', identityScopeKey: 'actor-a', isSimpleMode: false, isSuperAdmin: true, user: { id: 1, role: 'super_admin', policy_version: 1 }, can: () => true })

vi.mock('@/features/identity', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/identity')>()),
  useAuthStore: () => actor,
  useStepUp: () => ({ run: runStepUp, onCancel: cancelStepUp })
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
        <slot name="cell-select" :row="row" />
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

function mountView(stubActionMenu = true) {
  return mount(AccountsView, {
    attachTo: document.body,
    global: {
        provide: {
          [identityNotificationsKey as symbol]: useTestAppStore(),
          [uiFeedbackKey as symbol]: { success: useTestAppStore().showSuccess, error: useTestAppStore().showError },
          [resourceEditorServicesKey as symbol]: editorTestServices()
        },
      stubs: {
        TotpStepUpDialog: true,
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        AccountTableActions: { name: 'AccountTableActions', emits: ['refresh'], template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: { name: 'AccountBulkActionsBar', props: ['selectedIds'], emits: ['delete', 'reset-status', 'refresh-token', 'enable', 'disable', 'select-all-results'], template: '<div />' },
        Pagination: true,
        ConfirmDialog: { name: 'ConfirmDialog', props: ['show', 'title'], emits: ['confirm', 'cancel'], template: '<section v-if="show"><slot /></section>' },
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
        CreateAccountModal: true,
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


vi.mock('@/features/resources', async importOriginal => {
  const actual = await importOriginal<typeof import('@/features/resources')>()
  
  return { ...actual, accountsAPI: accountsApiFixture, proxiesAPI: proxiesApiFixture }
})
vi.mock('@/features/access-policy', async importOriginal => {
  const actual = await importOriginal<typeof import('@/features/access-policy')>()
  
  return { ...actual, groupsAPI: groupsApiFixture }

})


function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Promise not initialized') }
  let reject: (reason: unknown) => void = () => { throw new Error('Promise not initialized') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const secondAccount = { ...fullAccount, id: 43, name: 'second account' }
type View = ReturnType<typeof mountView>
function button(view: View, text: string) {
  const found = view.findAll('button').find(item => item.text().includes(text))
  if (!found) throw new Error('Missing action: ' + text)
  return found
}
function dialog(view: View, title: string) {
  const found = view.findAllComponents({ name: 'ConfirmDialog' }).find(item => item.props('title') === title)
  if (!found) throw new Error('Missing confirmation: ' + title)
  return found
}
async function openExport(view: View) {
  await view.get('button[title="admin.accounts.moreActions"]').trigger('click')
  const selected = view.findAll('button').find(item => item.text().includes('admin.accounts.dataExportSelected'))
  if (selected) await selected.trigger('click')
  else await button(view, 'admin.accounts.dataExport').trigger('click')
  return dialog(view, 'admin.accounts.dataExport')
}
async function selectRow(view: View, name: string, checked: boolean) {
  await view.get('[data-account-name="' + name + '"] input[type="checkbox"]').setValue(checked)
}
function switchActor() { actor.identityScopeKey = 'actor-b'; actor.user.id = 2 }

beforeEach(() => {
  vi.resetAllMocks()
  Object.assign(actor, { identityScopeKey: 'actor-a', isSuperAdmin: true })
  actor.user.id = 1
  actor.user.policy_version = 1
  actor.user.policy_version = 1
  localStorage.clear()
  listAccounts.mockResolvedValue({ items: [fullAccount, secondAccount], total: 2, page: 1, page_size: 20, pages: 1 })
  listWithEtag.mockResolvedValue({ notModified: true, etag: 'test-etag', data: null })
  getById.mockResolvedValue(fullAccount)
  getBatchTodayStats.mockResolvedValue({ stats: {} })
  getBatchUsage.mockResolvedValue({ usage: {}, errors: {} })
  getUpstreamBillingProbeSettings.mockResolvedValue({ enabled: false })
  getAllProxies.mockResolvedValue([])
  getAllGroups.mockResolvedValue([])
  getModels.mockResolvedValue([])
  exportAccounts.mockResolvedValue({ accounts: [], proxies: [] })
  runStepUp.mockImplementation((work: () => Promise<unknown>) => work())
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  vi.spyOn(console, 'error').mockImplementation(() => {})
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:synthetic-export') })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
})
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('AccountsPage command ownership', () => {
  it('freezes selected export IDs and includeProxies across step-up retries', async () => {
    const gate = deferred<void>()
    exportAccounts.mockRejectedValueOnce({ code: 'STEP_UP_REQUIRED' })
    runStepUp.mockImplementationOnce(async (work: () => Promise<unknown>) => {
      try { return await work() } catch { await gate.promise; return work() }
    })
    const view = mountView()
    await flushPromises()
    await selectRow(view, fullAccount.name, true)
    const confirmation = await openExport(view)
    confirmation.vm.$emit('confirm')
    await flushPromises()
    expect(exportAccounts).toHaveBeenCalledWith({ ids: [42], includeProxies: true }, { signal: expect.any(AbortSignal) })
    await selectRow(view, fullAccount.name, false)
    await selectRow(view, secondAccount.name, true)
    const next = await openExport(view)
    await next.get('input[type="checkbox"]').setValue(false)
    gate.resolve()
    await flushPromises()
    expect(exportAccounts).toHaveBeenCalledTimes(2)
    expect(exportAccounts.mock.calls[1][0]).toEqual({ ids: [42], includeProxies: true })
    expect(next.props('show')).toBe(true)
    view.unmount()
  })

  it('freezes all-results export filters while step-up is pending', async () => {
    const gate = deferred<void>()
    runStepUp.mockImplementationOnce(async (work: () => Promise<unknown>) => { await gate.promise; return work() })
    const view = mountView()
    await flushPromises()
    const filters = view.getComponent({ name: 'AccountTableFilters' })
    filters.vm.$emit('update:searchQuery', 'original-filter')
    const confirmation = await openExport(view)
    confirmation.vm.$emit('confirm')
    await flushPromises()
    filters.vm.$emit('update:searchQuery', 'later-filter')
    gate.resolve()
    await flushPromises()
    expect(exportAccounts).toHaveBeenCalledWith(expect.objectContaining({ filters: expect.objectContaining({ search: 'original-filter' }) }), { signal: expect.any(AbortSignal) })
    view.unmount()
  })

  it.each(['actor', 'unmount'] as const)('prevents a later step-up attempt after %s', async boundary => {
    const gate = deferred<void>()
    runStepUp.mockImplementationOnce(async (work: () => Promise<unknown>) => { await gate.promise; return work() })
    const view = mountView()
    await flushPromises()
    ;(await openExport(view)).vm.$emit('confirm')
    await flushPromises()
    if (boundary === 'actor') switchActor()
    else view.unmount()
    gate.resolve()
    await flushPromises()
    expect(exportAccounts).not.toHaveBeenCalled()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    if (boundary !== 'unmount') view.unmount()
  })

  it.each(['test', 'stats'] as const)('keeps the latest %s detail when old results arrive last', async event => {
    const old = deferred<typeof fullAccount>()
    getById.mockReturnValueOnce(old.promise).mockResolvedValueOnce(secondAccount)
    const view = mountView()
    await flushPromises()
    const menu = view.getComponent(AccountActionMenu)
    menu.vm.$emit(event, fullAccount)
    menu.vm.$emit(event, secondAccount)
    await flushPromises()
    old.resolve(fullAccount)
    await flushPromises()
    expect(view.get('[data-test="' + event + '-account"]').text()).toBe(secondAccount.name)
    const signal = getById.mock.calls[0][1].signal
    expect(signal.aborted).toBe(true)
    view.unmount()
  })

  it('gives the latest opening intent ownership across test and stats panels', async () => {
    const old = deferred<typeof fullAccount>()
    getById.mockReturnValueOnce(old.promise).mockResolvedValueOnce(secondAccount)
    const view = mountView()
    await flushPromises()
    const menu = view.getComponent(AccountActionMenu)
    menu.vm.$emit('test', fullAccount)
    menu.vm.$emit('stats', secondAccount)
    await flushPromises()
    old.resolve(fullAccount)
    await flushPromises()
    expect(view.get('[data-test="test-account"]').text()).toBe('')
    expect(view.get('[data-test="stats-account"]').text()).toBe(secondAccount.name)
    view.unmount()
  })

  it.each(['actor', 'unmount'] as const)('discards details without notifications after %s', async boundary => {
    const pending = deferred<typeof fullAccount>()
    getById.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    view.getComponent(AccountActionMenu).vm.$emit('test', fullAccount)
    if (boundary === 'actor') switchActor()
    else view.unmount()
    pending.resolve(fullAccount)
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    if (boundary === 'actor') {
      expect(view.get('[data-test="test-account"]').text()).toBe('')
      view.unmount()
    }
  })

  it('does not replace a new schedule target with an old model response', async () => {
    const old = deferred<Array<{ id: string; display_name: string }>>()
    getModels.mockReturnValueOnce(old.promise).mockResolvedValueOnce([{ id: 'model-b', display_name: 'Model B' }])
    const view = mountView()
    await flushPromises()
    const menu = view.getComponent(AccountActionMenu)
    menu.vm.$emit('schedule', fullAccount)
    menu.vm.$emit('schedule', secondAccount)
    await flushPromises()
    old.resolve([{ id: 'model-a', display_name: 'Model A' }])
    await flushPromises()
    const panel = view.getComponent({ name: 'ScheduledTestsPanel' })
    expect(panel.props('accountId')).toBe(43)
    expect(panel.props('modelOptions')).toEqual([{ value: 'model-b', label: 'Model B' }])
    view.unmount()
  })

  it('consumes shadow confirmation once and preserves the next target dialog', async () => {
    const pending = deferred<typeof fullAccount>()
    createShadow.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    const menu = view.getComponent(AccountActionMenu)
    menu.vm.$emit('create-spark-shadow', fullAccount)
    await view.vm.$nextTick()
    const confirm = dialog(view, 'admin.accounts.createSparkShadow')
    confirm.vm.$emit('confirm'); confirm.vm.$emit('confirm')
    await flushPromises()
    expect(createShadow).toHaveBeenCalledOnce()
    expect(createShadow).toHaveBeenCalledWith(42, { name: fullAccount.name + ' (Spark)' }, { signal: expect.any(AbortSignal) })
    menu.vm.$emit('create-spark-shadow', secondAccount)
    await view.vm.$nextTick()
    pending.resolve(fullAccount)
    await flushPromises()
    expect(confirm.props('show')).toBe(true)
    view.unmount()
  })

  it('does not automatically replay an unknown shadow result', async () => {
    createShadow.mockRejectedValueOnce(new Error('response unknown'))
    const view = mountView()
    await flushPromises()
    view.getComponent(AccountActionMenu).vm.$emit('create-spark-shadow', fullAccount)
    await view.vm.$nextTick()
    const confirm = dialog(view, 'admin.accounts.createSparkShadow')
    confirm.vm.$emit('confirm')
    await flushPromises()
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(createShadow).toHaveBeenCalledOnce()
    expect(showSuccess).not.toHaveBeenCalled()
    view.unmount()
  })

  it('allows different row refreshes to finish independently', async () => {
    const first = deferred<{ account: typeof fullAccount }>()
    const second = deferred<{ account: typeof fullAccount }>()
    refreshCredentials.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const view = mountView()
    await flushPromises()
    const menu = view.getComponent(AccountActionMenu)
    menu.vm.$emit('refresh-token', fullAccount)
    menu.vm.$emit('refresh-token', secondAccount)
    await flushPromises()
    expect(refreshCredentials.mock.calls.map(call => call[0])).toEqual([42, 43])
    for (const call of refreshCredentials.mock.calls) expect(call[1].signal.aborted).toBe(false)
    second.resolve({ account: { ...secondAccount, name: 'Refreshed B' } })
    first.resolve({ account: { ...fullAccount, name: 'Refreshed A' } })
    await flushPromises()
    expect(view.find('[data-account-name="Refreshed A"]').exists()).toBe(true)
    expect(view.find('[data-account-name="Refreshed B"]').exists()).toBe(true)
    view.unmount()
  })

  it.each(['actor', 'permission', 'unmount'] as const)('prevents a late row command toast and follow-up reload after %s', async boundary => {
    const pending = deferred<typeof fullAccount>()
    duplicateAccount.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    view.getComponent(AccountActionMenu).vm.$emit('duplicate', fullAccount)
    await flushPromises()
    const reads = listAccounts.mock.calls.length
    const signal = duplicateAccount.mock.calls[0][1].signal
    if (boundary === 'actor') switchActor()
    else if (boundary === 'permission') actor.user.policy_version++
    else view.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve({ ...fullAccount, id: 44 })
    await flushPromises()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(listAccounts).toHaveBeenCalledTimes(reads)
    if (boundary !== 'unmount') view.unmount()
  })

  it('preserves a newer selection when an old bulk command completes', async () => {
    const pending = deferred<{ success: number; failed: number }>()
    batchClearError.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    await selectRow(view, fullAccount.name, true)
    view.getComponent({ name: 'AccountBulkActionsBar' }).vm.$emit('reset-status')
    await flushPromises()
    await selectRow(view, fullAccount.name, false)
    await selectRow(view, secondAccount.name, true)
    pending.resolve({ success: 1, failed: 0 })
    await flushPromises()
    expect(batchClearError).toHaveBeenCalledWith([42], { signal: expect.any(AbortSignal) })
    expect(view.getComponent({ name: 'AccountBulkActionsBar' }).props('selectedIds')).toEqual([43])
    view.unmount()
  })
  it('consumes delete confirmation once and preserves a later target', async () => {
    const pending = deferred<{ message: string }>()
    deleteAccount.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    async function openDelete(name: string) {
      const row = view.get('[data-account-name="' + name + '"]')
      const remove = row.findAll('button').find(item => item.text() === 'common.delete')
      if (!remove) throw new Error('Missing row delete action')
      await remove.trigger('click')
    }
    await openDelete(fullAccount.name)
    const confirm = dialog(view, 'admin.accounts.deleteAccount')
    confirm.vm.$emit('confirm'); confirm.vm.$emit('confirm')
    await flushPromises()
    expect(deleteAccount).toHaveBeenCalledOnce()
    expect(deleteAccount).toHaveBeenCalledWith(42, { signal: expect.any(AbortSignal) })
    await openDelete(secondAccount.name)
    pending.resolve({ message: 'deleted' })
    await flushPromises()
    expect(confirm.props('show')).toBe(true)
    deleteAccount.mockResolvedValueOnce({ message: 'deleted' })
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(deleteAccount.mock.calls.map(call => call[0])).toEqual([42, 43])
    view.unmount()
  })

  it('keeps the latest edit target after a superseded full-detail read', async () => {
    const old = deferred<typeof fullAccount>()
    getById.mockReturnValueOnce(old.promise).mockResolvedValueOnce(secondAccount)
    const view = mountView()
    await flushPromises()
    for (const name of [fullAccount.name, secondAccount.name]) {
      const edit = view.get('[data-account-name="' + name + '"]').findAll('button').find(item => item.text() === 'common.edit')
      if (!edit) throw new Error('Missing row edit action')
      await edit.trigger('click')
    }
    old.resolve(fullAccount)
    await flushPromises()
    expect(view.get('[data-test="edit-account"]').text()).toBe(secondAccount.name)
    expect(getById.mock.calls[0][1].signal.aborted).toBe(true)
    view.unmount()
  })

  it.each(['close', 'actor', 'unmount'] as const)('aborts model reads after schedule %s', async boundary => {
    const pending = deferred<Array<{ id: string }>>()
    getModels.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    view.getComponent(AccountActionMenu).vm.$emit('schedule', fullAccount)
    await flushPromises()
    const signal = getModels.mock.calls[0][1].signal
    if (boundary === 'close') view.getComponent({ name: 'ScheduledTestsPanel' }).vm.$emit('close')
    else if (boundary === 'actor') switchActor()
    else view.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve([{ id: 'late-model' }])
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    if (boundary !== 'unmount') {
      expect(view.getComponent({ name: 'ScheduledTestsPanel' }).props('modelOptions')).toEqual([])
      view.unmount()
    }
  })

  it.each(['actor', 'permission', 'unmount'] as const)('stops bulk follow-up work after %s', async boundary => {
    const pending = deferred<{ success: number; failed: number }>()
    batchRefresh.mockReturnValueOnce(pending.promise)
    const view = mountView()
    await flushPromises()
    await selectRow(view, fullAccount.name, true)
    const bulk = view.getComponent({ name: 'AccountBulkActionsBar' })
    bulk.vm.$emit('refresh-token'); bulk.vm.$emit('refresh-token')
    await flushPromises()
    expect(batchRefresh).toHaveBeenCalledOnce()
    const reads = listAccounts.mock.calls.length
    const signal = batchRefresh.mock.calls[0][1].signal
    if (boundary === 'actor') switchActor()
    else if (boundary === 'permission') actor.user.policy_version++
    else view.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve({ success: 1, failed: 0 })
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledTimes(reads)
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    if (boundary !== 'unmount') view.unmount()
  })

  it('does not start another all-results page after workspace disposal', async () => {
    const page = deferred<{ items: typeof fullAccount[]; total: number; pages: number; page: number; page_size: number }>()
    const view = mountView()
    await flushPromises()
    listAccounts.mockReturnValueOnce(page.promise)
    view.getComponent({ name: 'AccountBulkActionsBar' }).vm.$emit('select-all-results')
    await flushPromises()
    const reads = listAccounts.mock.calls.length
    const signal = listAccounts.mock.calls[reads - 1][3].signal
    view.unmount()
    expect(signal.aborted).toBe(true)
    page.resolve({ items: [fullAccount], total: 501, pages: 2, page: 1, page_size: 500 })
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledTimes(reads)
    expect(showError).not.toHaveBeenCalled()
  })

})
