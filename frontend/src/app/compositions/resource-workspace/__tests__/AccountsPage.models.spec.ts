import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import AccountsPage from '../AccountsPage.vue'
import AccountModelReferencesDialog from '../AccountModelReferencesDialog.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import { apiClient } from '@/shared/http'

enableAutoUnmount(afterEach)
const { accounts, proxies, groups } = vi.hoisted(() => ({
  accounts: { list: vi.fn(), listWithEtag: vi.fn(), getById: vi.fn(), getBatchTodayStats: vi.fn(), getBatchUsage: vi.fn(), getUpstreamBillingProbeSettings: vi.fn() },
  proxies: { getAll: vi.fn() }, groups: { getAll: vi.fn() }
}))
const actor = reactive({ identityScopeKey: 'account-session', isAuthenticated: true, isSimpleMode: false, isSuperAdmin: false, user: { id: 1, role: 'admin', policy_version: 1 }, permissions: ['accounts.read', 'accounts.manage'], can: (permission: string) => actor.permissions.includes(permission) })
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => actor, useStepUp: () => ({ onCancel: vi.fn() }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showWarning: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('@/features/resources', async original => ({ ...await original<typeof import('@/features/resources')>(), accountsAPI: accounts, proxiesAPI: proxies }))
vi.mock('@/features/resources/accounts', () => ({ accountsAPI: accounts }))
vi.mock('@/features/access-policy', async original => ({ ...await original<typeof import('@/features/access-policy')>(), groupsAPI: groups }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const row = { id: 42, name: 'first account', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, concurrency: 2, priority: 1, group_ids: [], extra: {}, credentials: {} }
const full = { ...row, credentials: { model_allowlist: ['gpt-a'] }, groups: [], account_groups: [] }
const other = { ...row, id: 43, name: 'other account', platform: 'anthropic' }
const legacy = { ...row, id: 44, name: 'legacy account', platform: 'gemini' }
async function settle() { await vi.dynamicImportSettled(); await flushPromises() }
function setup() {
  const services = editorTestServices()
  services.managedModelCatalog = vi.fn().mockResolvedValue(['gpt-a', 'gpt-b'].map(id => ({ id, display_name: id, enabled: true })))
  return mount(AccountsPage, { attachTo: document.body, global: {
    provide: { [resourceEditorServicesKey as symbol]: services, [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } },
    stubs: {
      TablePageLayout: { template: '<main><slot name="filters" /><slot name="table" /></main>' },
      DataTable: defineComponent({ props: ['data'], template: '<div><section v-for="row in data" :key="row.id" :data-row="row.id"><span>{{ row.name }}</span><slot name="cell-actions" :row="row" /></section></div>' }),
      AccountTableActions: true, AccountTableFilters: true, AccountBulkActionsBar: true,
      Pagination: true, ConfirmDialog: true, TotpStepUpDialog: true, AccountActionMenu: true,
      ImportDataModal: true, ReAuthAccountModal: true, AccountTestModal: true,
      AccountStatsModal: true, ScheduledTestsPanel: true, SyncFromCrsModal: true,
      TempUnschedStatusModal: true, ErrorPassthroughRulesModal: true, TLSFingerprintProfilesModal: true,
      CreateAccountModal: true, EditAccountModal: true, BulkEditAccountModal: true,
      CodexDiagnosticModal: true
    }
  } })
}
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  Object.assign(actor, { identityScopeKey: 'account-session', permissions: ['accounts.read', 'accounts.manage'] })
  accounts.list.mockResolvedValue({ items: [row, other, legacy], total: 3, page: 1, page_size: 20, pages: 1 })
  accounts.listWithEtag.mockResolvedValue({ notModified: true, etag: 'models-etag', data: null })
  accounts.getById.mockResolvedValue(full)
  accounts.getBatchTodayStats.mockResolvedValue({ stats: {} })
  accounts.getBatchUsage.mockResolvedValue({ usage: {}, errors: {} })
  accounts.getUpstreamBillingProbeSettings.mockResolvedValue({ enabled: false })
  proxies.getAll.mockResolvedValue([])
  groups.getAll.mockResolvedValue([])
  vi.spyOn(apiClient, 'put').mockResolvedValue({ data: { models: ['gpt-a', 'gpt-b'] } })
})
afterEach(() => vi.restoreAllMocks())

describe('AccountsPage independent model PUT entry', () => {
  it('opens from a real account action, saves via the real selector, and refreshes only that row', async () => {
    const wrapper = setup()
    await settle()
    expect(wrapper.findAll('[data-testid="account-models"]')).toHaveLength(2)
    expect(accounts.getById).not.toHaveBeenCalled()
    expect(wrapper.findComponent(AccountModelReferencesDialog).exists()).toBe(false)
    await wrapper.get('[data-row="42"] [data-testid="account-models"]').trigger('click')
    await settle()
    expect(wrapper.getComponent(ModelWhitelistSelector).props('saveMode')).toBe('immediate')
    accounts.getById.mockResolvedValue({ ...full, name: 'updated first account', credentials: { model_allowlist: ['gpt-a', 'gpt-b'] } })
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('[role="button"]').trigger('click')
    await selector.findAll('[data-testid="select-model"]')[1].trigger('click')
    await settle()
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/models/accounts/42/allowlist', { expected: ['gpt-a'], models: ['gpt-a', 'gpt-b'] }, { signal: expect.any(AbortSignal) })
    expect(accounts.getById.mock.calls.map(args => args[0])).toEqual([42, 42])
    expect(wrapper.get('[data-row="42"]').text()).toContain('updated first account')
    expect(wrapper.get('[data-row="43"]').text()).toContain('other account')
    expect(accounts.list).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(AccountModelReferencesDialog).exists()).toBe(true)
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await settle()
    expect(wrapper.findComponent(AccountModelReferencesDialog).exists()).toBe(false)
    expect(accounts.list).toHaveBeenCalledTimes(1)
  })

  it.each([['accounts.read'], ['risk.read', 'risk.manage'], ['models.read', 'models.manage']])('does not expose an account model write under disjoint permissions: %j', async (...permissions) => {
    actor.permissions = permissions
    const wrapper = setup()
    await settle()
    expect(wrapper.find('[data-testid="account-models"]').exists()).toBe(false)
    expect(accounts.getById).not.toHaveBeenCalled()
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it('does not apply a late row reload after permissions are revoked', async () => {
    let complete!: (value: typeof full) => void
    const pending = new Promise<typeof full>(resolve => { complete = resolve })
    const wrapper = setup()
    await settle()
    await wrapper.get('[data-row="42"] [data-testid="account-models"]').trigger('click')
    await settle()
    accounts.getById.mockReturnValueOnce(pending)
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('[role="button"]').trigger('click')
    await selector.findAll('[data-testid="select-model"]')[1].trigger('click')
    await settle()
    const signal = accounts.getById.mock.calls[1][1].signal as AbortSignal
    actor.permissions = ['accounts.read']
    await settle()
    expect(signal.aborted).toBe(true)
    complete({ ...full, name: 'stale row overwrite' })
    await settle()
    expect(wrapper.get('[data-row="42"]').text()).toContain('first account')
    expect(wrapper.text()).not.toContain('stale row overwrite')
    expect(wrapper.findComponent(AccountModelReferencesDialog).exists()).toBe(false)
  })
})
