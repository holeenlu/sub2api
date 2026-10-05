import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import CodexDiagnosticModal from '../CodexDiagnosticModal.vue'
import CodexDiagnosticBadge from '../CodexDiagnosticBadge.vue'
import Select from '@/components/common/Select.vue'
import type { Account } from '@/types'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'
const api = vi.hoisted(() => ({
  diagnosticPlan: vi.fn(), ownKeys: vi.fn(), diagnosticModels: vi.fn(),
  saveDiagnosticPlan: vi.fn(), startDiagnosticRun: vi.fn(), diagnosticRuns: vi.fn(),
  diagnosticRun: vi.fn(), cancelDiagnosticRun: vi.fn(), refreshFingerprint: vi.fn()
}))
vi.mock('@/api/admin/codexTickets', () => api)
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string) => key.split('.').reduce<unknown>((v, k) => v && typeof v === 'object' ? (v as Record<string, unknown>)[k] : undefined, { admin: zhAccounts }) || key
}) }))
const plan = { interval_minutes: 60, account_id: 19, owner_id: 7, api_key_id: 4, models: ['gpt-5.5'], enabled: true, revision: 1, next_run_at: '2026-10-05T03:00:00Z', updated_at: '2026-10-05T02:00:00Z' }
const run = { id: 31, account_id: 19, owner_id: 7, api_key_id: 4, api_key_name: 'Billing key', plan_revision: 1, models: ['gpt-5.5'], source: 'scheduled', status: 'normal', created_at: '2026-10-05T01:00:00Z', finished_at: '2026-10-05T01:01:00Z', items: [{ model: 'gpt-5.5', status: 'normal', probability: .99, predicted_model: 'gpt-5.5', fingerprint_commit: 'abc123', expected_count: 300, parsed_number_count: 300, duration_ms: 1200, http_status: 200 }] }
const summary = { interval_minutes: 60, run_id: 31, status: 'normal', enabled: true, checked_at: run.finished_at, next_run_at: plan.next_run_at }
function mountModal() {
  return mount(CodexDiagnosticModal, {
    props: { show: true, account: { id: 19, name: 'Test account', platform: 'openai', type: 'oauth' } as Account },
    global: { stubs: {
      BaseDialog: { template: '<div><slot name="header" title-id="test-title"/><slot/><slot name="footer"/></div>' },
      Select: true, Icon: true, CodexDiagnosticModelPicker: true
    } }
  })
}
describe('Persistent Codex diagnostic monitor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-05T02:00:00Z'))
    vi.resetAllMocks()
    api.diagnosticPlan.mockResolvedValue({ plan: { ...plan }, summary: { ...summary }, interval_minutes: 60, confidence_threshold: .8 })
    api.ownKeys.mockResolvedValue([{ id: 4, name: 'Billing key', status: 'active', quota: 0, quota_used: 0 }])
    api.diagnosticModels.mockResolvedValue({ models: ['gpt-5.5', 'gpt-5.4'], commit: 'abc123' })
    api.diagnosticRuns.mockResolvedValue({ items: [{ ...run }], next_before_id: 0 })
    api.saveDiagnosticPlan.mockResolvedValue({ ...plan })
    api.startDiagnosticRun.mockResolvedValue({ ...run, id: 32, source: 'manual', status: 'queued', items: [], finished_at: null })
  })
  afterEach(() => { vi.useRealTimers() })
  it('restores saved key, model, hourly setting and persistent history', async () => {
    const w = mountModal(); await flushPromises()
    expect(w.findComponent(Select).props('modelValue')).toBe(4)
    expect((w.get('[data-testid="diagnostic-hourly"]').element as HTMLInputElement).checked).toBe(true)
    expect(api.diagnosticModels).toHaveBeenCalledWith(19, 4)
    const history = w.findAll('[role="tab"]').find(b => b.text() === '历史记录')!
    await history.trigger('click')
    expect(w.text()).toContain('#31')
    await w.get('summary').trigger('click')
    expect(w.text()).toContain('99.0%')
    expect(w.text()).toContain('300 / 300')
    expect(w.text()).toContain('abc123')
    w.unmount()
  })
  it('saves before starting and closing does not cancel server work', async () => {
    const w = mountModal(); await flushPromises()
    await w.get('[data-action="start"]').trigger('click'); await flushPromises()
    expect(api.saveDiagnosticPlan).toHaveBeenCalledWith(19, { api_key_id: 4, models: ['gpt-5.5'], enabled: true, interval_minutes: 60 })
    expect(api.startDiagnosticRun).toHaveBeenCalledWith(19)
    expect(api.saveDiagnosticPlan.mock.invocationCallOrder[0]).toBeLessThan(api.startDiagnosticRun.mock.invocationCallOrder[0])
    expect(w.text()).toContain('#32')
    w.unmount()
    await vi.advanceTimersByTimeAsync(8000)
    expect(api.cancelDiagnosticRun).not.toHaveBeenCalled()
  })
  it('saves a custom interval in hours and rejects invalid values', async () => {
    const w = mountModal(); await flushPromises()
    await w.get('#diagnostic-interval').setValue(5)
    await w.findAll('button').find(b => b.text() === '保存设置')!.trigger('click')
    await flushPromises()
    expect(api.saveDiagnosticPlan).toHaveBeenCalledWith(19, expect.objectContaining({ interval_minutes: 300 }))
    await w.get('#diagnostic-interval').setValue(0)
    expect(w.get('[data-action="start"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('1～168')
    w.unmount()
  })
  it('never starts a billed run if settings could not be saved', async () => {
    api.saveDiagnosticPlan.mockRejectedValue(new Error('save failed'))
    const w = mountModal(); await flushPromises()
    await w.get('[data-action="start"]').trigger('click'); await flushPromises()
    expect(api.startDiagnosticRun).not.toHaveBeenCalled()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    w.unmount()
  })
  it('polls an existing run after reopening, without starting a new one', async () => {
    api.diagnosticRuns.mockResolvedValue({ items: [{ ...run, status: 'running', items: [], finished_at: null }], next_before_id: 0 })
    const w = mountModal(); await flushPromises()
    expect(w.get('[data-action="start"]').attributes('disabled')).toBeDefined()
    api.diagnosticRuns.mockResolvedValue({ items: [{ ...run, status: 'failed', reason: 'gateway_request_failed' }], next_before_id: 0 })
    api.diagnosticPlan.mockResolvedValue({ plan, summary: { ...summary, status: 'failed' } })
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(w.text()).toContain('检测失败')
    expect(api.startDiagnosticRun).not.toHaveBeenCalled()
    expect(w.emitted('completed')).toBeTruthy()
    w.unmount()
  })
  it('can disable the schedule after its saved key disappeared', async () => {
    api.ownKeys.mockResolvedValue([])
    const w = mountModal(); await flushPromises()
    expect(w.text()).toContain('原计费 Key 已不可用')
    expect(w.get('[data-action="start"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="diagnostic-hourly"]').setValue(false); await nextTick()
    const save = w.findAll('button').find(b => b.text() === '保存设置')!
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click'); await flushPromises()
    expect(api.saveDiagnosticPlan).toHaveBeenCalledWith(19, expect.objectContaining({ enabled: false }))
    w.unmount()
  })
  it('shows at most ten runs without an older-page control', async () => {
    api.diagnosticRuns.mockResolvedValue({ items: Array.from({ length: 12 }, (_, index) => ({ ...run, id: 100 - index })), next_before_id: 88 })
    const w = mountModal(); await flushPromises()
    await w.findAll('[role="tab"]').find(b => b.text() === '历史记录')!.trigger('click')
    expect(w.findAll('details')).toHaveLength(10)
    expect(w.text()).not.toContain('#90')
    expect(w.text()).not.toContain('加载更早记录')
    expect(w.text()).toContain('最近 10 次')
    w.unmount()
  })
  it('shows the key group whitelist and explicit eligibility reasons', async () => {
    api.diagnosticModels.mockResolvedValue({
      models: ['gpt-5.5'], group_name: 'Selected group', whitelist_enabled: true, commit: 'bank',
      items: [{ id: 'gpt-5.5', eligible: true }, { id: 'gpt-image-test', eligible: false, reason: 'fingerprint_unavailable' }]
    })
    const w = mountModal(); await flushPromises()
    const picker = w.findComponent({ name: 'CodexDiagnosticModelPicker' })
    expect(picker.props('models')).toEqual(['gpt-5.5', 'gpt-image-test'])
    expect(picker.props('sourceLabel')).toContain('Selected group')
    expect(picker.props('disabledReasons')['gpt-image-test']).toContain('暂无指纹数据')
    w.unmount()
  })
  it('ignores slow results from a different account', async () => {
    let resolve!: (value: unknown) => void
    api.diagnosticPlan.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const w = mountModal()
    await w.setProps({ account: { id: 20, name: 'Other' } as Account })
    await flushPromises()
    resolve({ plan: { ...plan, api_key_id: 99 }, summary })
    await flushPromises()
    expect(w.findComponent(Select).props('modelValue')).toBe(4)
    expect(w.text()).not.toContain('#99')
    w.unmount()
  })
})
describe('Account diagnostic badge', () => {
  it('does not classify request failure as degraded', () => {
    const w = mount(CodexDiagnosticBadge, { props: { summary: { ...summary, status: 'failed' } } })
    expect(w.text()).toContain('检测失败'); expect(w.text()).not.toContain('疑似降智')
    w.unmount()
  })
  it('marks an old normal result as stale and opens history on click', async () => {
    const w = mount(CodexDiagnosticBadge, { props: { summary: { ...summary, checked_at: '2020-01-01T00:00:00Z' } } })
    expect(w.text()).toContain('结果较旧')
    await w.get('button').trigger('click'); expect(w.emitted('open')).toBeTruthy()
    w.unmount()
  })
})
