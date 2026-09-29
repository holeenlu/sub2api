import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AccountAutoBPSView from '../AccountAutoBPSView.vue'
import { reactive } from 'vue'

enableAutoUnmount(afterEach)
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), update: vi.fn(), delete: vi.fn() }))
const state = vi.hoisted(() => ({ auth: null as any }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/api/client', () => ({ apiClient: { get: api.get, post: api.post } }))
vi.mock('@/api/admin', () => ({ adminAPI: { scheduledTests: { update: api.update, delete: api.delete }, groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => key + (values ? ' ' + JSON.stringify(values) : '') }) }))
const plan = { id: 3, account_id: 7, account_name: 'Synthetic account', model_id: 'gpt-6-astra', cron_expression: '*/30 * * * *', enabled: true,
  pelican_config: { question_kind: 'state_probe', reasoning_effort: 'high', quality: { action: 'enable_bps', auto_restore: true, bps: { failure_threshold: 2, all_models: true, pass_threshold: 2 } } } }
beforeEach(() => {
  vi.clearAllMocks()
  api.delete.mockReset()
  state.auth = reactive({ user: { id: 1, role: 'admin' } })
  api.delete.mockResolvedValue(undefined)
  api.get.mockImplementation(async (path: string) => {
    if (path === '/admin/account-quality-plans') return { data: [structuredClone(plan)] }
    if (path === '/admin/account-quality-results') return { data: { items: [{ id: 11, plan_id: 3, account_id: 7, status: 'success', quality_judgment: { verdict: 'correct' } }], next_cursor: 0 } }
    if (path === '/admin/scheduled-test-plans/3/results/11') return { data: { response_text: 'Synthetic probe details' } }
    throw new Error('unexpected endpoint')
  })
  api.update.mockResolvedValue(plan)
})
async function open() {
  const wrapper = mount(AccountAutoBPSView, { global: { stubs: { BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' }, AppLayout: { template: '<main><slot /></main>' }, SmartOpsNav: true, QualityBPSSettings: true, RouterLink: { template: '<a><slot /></a>' } } } })
  await flushPromises(); return wrapper
}
describe('Automatic BPS operations', () => {
  it('pauses only the rule without mutating account options', async () => {
    const wrapper = await open()
    await wrapper.findAll('button').find(b => b.text() === 'autoBPSOps.pause')!.trigger('click')
    await flushPromises()
    expect(api.update).toHaveBeenCalledTimes(1)
    expect(api.update).toHaveBeenCalledWith(3, { enabled: false })
    expect(api.post).not.toHaveBeenCalled()
  })
  it('loads result details from the account-independent history using the correct plan and result IDs', async () => {
    const wrapper = await open()
    const details = wrapper.get('details')
    ;(details.element as HTMLDetailsElement).open = true
    await details.trigger('toggle'); await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/admin/scheduled-test-plans/3/results/11')
    expect(wrapper.text()).toContain('Synthetic probe details')
  })
  it('preserves supported policy options and forces state-probe-only configuration on save', async () => {
    const wrapper = await open()
    await wrapper.findAll('button').find(b => b.text() === 'autoBPSOps.edit')!.trigger('click')
    const inputs = wrapper.findAll('form input')
    await inputs[0]!.setValue('gpt-5.6-sol')
    await wrapper.get('[data-testid="quality-probe-interval"]').setValue('*/15 * * * *')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith(3, expect.objectContaining({ model_id: 'gpt-5.6-sol', cron_expression: '*/15 * * * *', auto_recover: false,
      pelican_config: expect.objectContaining({ question_kind: 'state_probe', parallel_count: 1, quality: expect.objectContaining({ action: 'enable_bps', remove_group_ids: [], bps: expect.objectContaining({ session_proxy: false, proxy_source: '' }) }) }) }))
  })
})

const rules = () => [
  structuredClone(plan),
  { ...structuredClone(plan), id: 4, account_id: 8, account_name: 'Second account' },
  { ...structuredClone(plan), id: 5, account_id: 9, account_name: 'Third account' }
]
function loadRules() {
  api.get.mockImplementation(async (path: string) => {
    if (path === '/admin/account-quality-plans') return { data: rules() }
    if (path === '/admin/account-quality-results') return { data: { items: rules().map(rule => ({ id: rule.id + 10, plan_id: rule.id, account_id: rule.account_id, account_name: rule.account_name, status: 'success' })), next_cursor: 0 } }
    throw new Error('unexpected endpoint')
  })
}

describe('Automatic BPS rule deletion', () => {
  beforeEach(loadRules)

  it('requires confirmation and deletes only the snapshotted rules without changing accounts', async () => {
    const wrapper = await open()
    expect(wrapper.get('[data-testid="bps-bulk-delete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-plan-id="3"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-plan-id="4"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    const targets = wrapper.get('[data-testid="bps-delete-targets"]')
    expect(targets.text()).toContain('Synthetic account')
    expect(targets.text()).toContain('Second account')
    expect(targets.text()).not.toContain('Third account')
    expect(api.delete).not.toHaveBeenCalled()
    // A later selection cannot expand the scope the operator confirmed.
    ;(wrapper.vm as any).selectedRuleIds = [5]
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3], [4]])
    expect(api.update).not.toHaveBeenCalled()
    expect(api.post).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-plan-id]').map(row => row.attributes('data-plan-id'))).toEqual(['5'])
    expect((wrapper.vm as any).selectedRuleIds).toEqual([5])
    expect((wrapper.vm as any).history.map((item: any) => item.plan_id)).toEqual([5])
    expect(wrapper.find('[data-testid="bps-delete-targets"]').exists()).toBe(false)
  })

  it('cancels without sending deletes and supports single-rule deletion from the editor', async () => {
    const wrapper = await open()
    await wrapper.get('[data-testid="bps-select-all"]').setValue(true)
    expect((wrapper.vm as any).selectedRuleIds).toEqual([3, 4, 5])
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    await wrapper.get('[data-testid="bps-cancel-delete"]').trigger('click')
    expect(api.delete).not.toHaveBeenCalled()
    await wrapper.get('[data-plan-id="3"] button').trigger('click')
    await wrapper.get('[data-testid="bps-editor-delete"]').trigger('click')
    expect(wrapper.get('[data-testid="bps-delete-targets"]').findAll('li')).toHaveLength(1)
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3]])
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('continues after a failed delete and retries only failures', async () => {
    api.delete.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('delete denied')).mockResolvedValueOnce(undefined)
    const wrapper = await open()
    await wrapper.get('[data-testid="bps-select-all"]').setValue(true)
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3], [4], [5]])
    expect(wrapper.get('[data-testid="bps-delete-error"]').text()).toContain('#4: delete denied')
    expect(wrapper.get('[data-testid="bps-delete-targets"]').findAll('li')).toHaveLength(1)
    expect((wrapper.vm as any).selectedRuleIds).toEqual([4])
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3], [4], [5], [4]])
    expect(wrapper.find('[data-testid="bps-delete-targets"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-plan-id]')).toHaveLength(0)
  })

  it('keeps successful deletions removed when refreshing fails', async () => {
    const wrapper = await open()
    await wrapper.get('[data-testid="bps-select-all"]').setValue(true)
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    api.get.mockRejectedValue(new Error('refresh failed'))
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3], [4], [5]])
    expect(wrapper.findAll('[data-plan-id]')).toHaveLength(0)
    expect((wrapper.vm as any).history).toEqual([])
    expect((wrapper.vm as any).selectedRuleIds).toEqual([])
    expect(wrapper.get('[role="alert"]').text()).toContain('refresh failed')
    expect(wrapper.find('[data-testid="bps-delete-targets"]').exists()).toBe(false)
  })

  it.each(['unmount', 'identity', 'logout'])('prevents duplicate submissions and stops the batch after %s', async change => {
    let finish!: () => void
    api.delete.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = await open()
    const vm = wrapper.vm as any
    await wrapper.get('[data-testid="bps-select-all"]').setValue(true)
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="bps-confirm-delete"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="bps-cancel-delete"]').attributes('disabled')).toBeDefined()
    await vm.confirmDelete()
    vm.cancelDelete()
    expect(vm.deleteTargets).toHaveLength(3)
    expect(api.delete).toHaveBeenCalledTimes(1)
    if (change === 'unmount') wrapper.unmount()
    else if (change === 'identity') state.auth.user.id = 2
    else state.auth.user = null
    await flushPromises()
    finish(); await flushPromises()
    expect(api.delete.mock.calls).toEqual([[3]])
    expect(api.get.mock.calls.filter(([path]) => path === '/admin/account-quality-plans')).toHaveLength(1)
    if (change !== 'unmount') {
      expect(wrapper.find('[data-testid="bps-delete-targets"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="bps-delete-error"]').exists()).toBe(false)
      expect(wrapper.findAll('[data-plan-id]')).toHaveLength(0)
    }
  })

  it.each(['success', 'failure'])('ignores an earlier detail request after deleting its rule (%s)', async outcome => {
    let finish!: (value: unknown) => void
    let fail!: (error: Error) => void
    api.get.mockImplementationOnce(async () => ({ data: rules() }))
    const wrapper = await open()
    api.get.mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    const details = wrapper.findAll('details')[0]!
    ;(details.element as HTMLDetailsElement).open = true
    await details.trigger('toggle'); await flushPromises()
    await wrapper.get('[data-plan-id="3"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="bps-bulk-delete"]').trigger('click')
    await wrapper.get('[data-testid="bps-confirm-delete"]').trigger('click'); await flushPromises()
    if (outcome === 'success') finish({ data: { response_text: 'Deleted probe result' } })
    else fail(new Error('Deleted result no longer exists'))
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect((wrapper.vm as any).detailText[13]).toBeUndefined()
    expect((wrapper.vm as any).history.map((item: any) => item.plan_id)).toEqual([4, 5])
  })
})
