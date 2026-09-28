import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AccountAutoBPSView from '../AccountAutoBPSView.vue'

enableAutoUnmount(afterEach)
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), update: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get: api.get, post: api.post } }))
vi.mock('@/api/admin', () => ({ adminAPI: { scheduledTests: { update: api.update }, groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const plan = { id: 3, account_id: 7, account_name: 'Synthetic account', model_id: 'gpt-6-astra', cron_expression: '*/30 * * * *', enabled: true,
  pelican_config: { question_kind: 'state_probe', reasoning_effort: 'high', quality: { action: 'enable_bps', auto_restore: true, bps: { failure_threshold: 2, all_models: true, pass_threshold: 2 } } } }
beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockImplementation(async (path: string) => {
    if (path === '/admin/account-quality-plans') return { data: [structuredClone(plan)] }
    if (path === '/admin/account-quality-results') return { data: { items: [{ id: 11, plan_id: 3, account_id: 7, status: 'success', quality_judgment: { verdict: 'correct' } }], next_cursor: 0 } }
    if (path === '/admin/scheduled-test-plans/3/results/11') return { data: { response_text: 'Synthetic probe details' } }
    throw new Error('unexpected endpoint')
  })
  api.update.mockResolvedValue(plan)
})
async function open() {
  const wrapper = mount(AccountAutoBPSView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, SmartOpsNav: true, QualityBPSSettings: true, RouterLink: { template: '<a><slot /></a>' } } } })
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
    await inputs[1]!.setValue('*/15 * * * *')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith(3, expect.objectContaining({ model_id: 'gpt-5.6-sol', cron_expression: '*/15 * * * *', auto_recover: false,
      pelican_config: expect.objectContaining({ question_kind: 'state_probe', parallel_count: 1, quality: expect.objectContaining({ action: 'enable_bps', remove_group_ids: [], bps: expect.objectContaining({ session_proxy: false, proxy_source: '' }) }) }) }))
  })
})
