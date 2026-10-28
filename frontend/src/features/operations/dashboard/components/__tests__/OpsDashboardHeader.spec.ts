import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import type { OpsDashboardOverview, OpsNodeMetricsSnapshot } from '@/features/operations/api/ops'
import { opsAPI } from '@/features/operations/api/ops'
import { useOperationsServices } from '@/features/operations/services'

const services = useOperationsServices()

vi.mock('@/features/operations/api/ops', () => ({ opsAPI: { getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ enabled: true, summary: null }) } }))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ locale: { value: 'en' }, t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key })
}))

afterEach(() => { vi.useRealTimers() })
function freshNode(id: string): OpsNodeMetricsSnapshot {
  return { id: 0, created_at: '2026-10-26T00:00:00Z', window_minutes: 1, node_id: id, node_name: id,
    cpu_scope: 'container', memory_scope: 'container', cpu_capacity: 4, last_seen_at: '2026-10-26T00:00:00Z',
    age_seconds: 0, stale: false, cpu_usage_percent: 10, memory_usage_percent: 20, db_ok: true, redis_ok: true }
}
const overview = { tps: { avg: 242.7 } } as OpsDashboardOverview
function mountHeader(value = overview) {
  return mount(OpsDashboardHeader, {
    props: { overview: value, platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null },
    global: { stubs: { Select: true, HelpTooltip: true, BaseDialog: true, Icon: true } }
  })
}

describe('OpsDashboardHeader refresh ownership', () => {
  const request = vi.mocked(opsAPI.getRealtimeTrafficSummary)
  beforeEach(() => {
    request.mockReset().mockResolvedValue({ enabled: true, summary: null })
    services.scopeKey.value = 'session-a:admin:all'
    services.monitoring.value = { enabled: true, realtimeEnabled: true, queryModeDefault: 'auto' }
  })

  it('loads once initially and once per completed automatic or manual snapshot', async () => {
    const wrapper = mountHeader()
    try {
      await flushPromises()
      expect(request).toHaveBeenCalledTimes(1)
      await wrapper.setProps({ autoRefreshEnabled: true, autoRefreshCountdown: 1 })
      await wrapper.setProps({ autoRefreshCountdown: 0 })
      await wrapper.setProps({ loading: true })
      expect(request).toHaveBeenCalledTimes(1)
      await wrapper.setProps({ lastUpdated: new Date('2026-10-26T08:00:00Z'), loading: false, autoRefreshCountdown: 15 })
      await flushPromises()
      expect(request).toHaveBeenCalledTimes(2)
      await wrapper.setProps({ autoRefreshEnabled: false, autoRefreshCountdown: 0 })
      await wrapper.get('button[title="common.refresh"]').trigger('click')
      expect(wrapper.emitted('refresh')).toHaveLength(1)
      expect(request).toHaveBeenCalledTimes(2)
      await wrapper.setProps({ loading: true })
      await wrapper.setProps({ lastUpdated: new Date('2026-10-26T08:00:30Z'), loading: false })
      await flushPromises()
      expect(request).toHaveBeenCalledTimes(3)
      await wrapper.setProps({ loading: true, autoRefreshEnabled: true, autoRefreshCountdown: 0 })
      await wrapper.setProps({ loading: false, autoRefreshCountdown: 15 })
      expect(request).toHaveBeenCalledTimes(3)
    } finally { wrapper.unmount() }
  })

  it('coalesces filter and realtime-window changes while preserving request cancellation', async () => {
    const wrapper = mountHeader()
    try {
      await flushPromises()
      let finish: (value: { enabled: boolean; summary: null }) => void = () => { throw new Error('Request not started') }
      request.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      await wrapper.setProps({ platform: 'openai', groupId: 7 })
      expect(request).toHaveBeenCalledTimes(2)
      expect(request).toHaveBeenLastCalledWith('1min', 'openai', 7, expect.objectContaining({ signal: expect.any(AbortSignal) }))
      const signal = request.mock.lastCall?.[3]?.signal
      await wrapper.setProps({ timeRange: '5m', lastUpdated: new Date('2026-10-26T08:01:00Z') })
      await flushPromises()
      expect(signal?.aborted).toBe(true)
      expect(request).toHaveBeenCalledTimes(3)
      finish({ enabled: false, summary: null })
      await flushPromises()
      expect(services.disableRealtimeMonitoring).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('stops pending summary work when realtime monitoring is disabled and loads once on re-enable', async () => {
    const wrapper = mountHeader()
    try {
      await flushPromises()
      request.mockReturnValueOnce(new Promise(() => {}))
      await wrapper.setProps({ lastUpdated: new Date('2026-10-26T08:01:00Z') })
      const signal = request.mock.lastCall?.[3]?.signal
      services.monitoring.value.realtimeEnabled = false
      await flushPromises()
      expect(signal?.aborted).toBe(true)
      expect(request).toHaveBeenCalledTimes(2)
      await wrapper.setProps({ lastUpdated: new Date('2026-10-26T08:01:30Z') })
      expect(request).toHaveBeenCalledTimes(2)
      services.monitoring.value.realtimeEnabled = true
      await flushPromises()
      expect(request).toHaveBeenCalledTimes(3)
    } finally { wrapper.unmount() }
  })

  it.each(['scope loss', 'unmount'])('ignores late completion after %s', async (reason) => {
    vi.useFakeTimers()
    let finish: (value: { enabled: boolean; summary: null }) => void = () => { throw new Error('Request not started') }
    request.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mountHeader()
    try {
      await flushPromises()
      const signal = request.mock.lastCall?.[3]?.signal
      if (reason === 'scope loss') services.scopeKey.value = null
      else wrapper.unmount()
      expect(signal?.aborted).toBe(true)
      finish({ enabled: false, summary: null })
      await flushPromises()
      expect(services.disableRealtimeMonitoring).not.toHaveBeenCalled()
      expect(request).toHaveBeenCalledTimes(1)
    } finally { wrapper.unmount() }
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('OpsDashboardHeader output TPS', () => {

  it('shows per-request P50, slower-tail percentiles and sample count separately from traffic TPS', async () => {
    const wrapper = mountHeader({ ...overview, output_tps: { p5: 2.35, p10: 3.7, p50: 55, avg: 277.75, sample_count: 4 } })
    await flushPromises()
    const card = wrapper.get('[data-testid="output-tps-card"]')
    expect(card.get('[data-testid="output-tps-p50"]').text()).toBe('55.0')
    expect(card.get('[data-testid="output-tps-p5"]').text()).toBe('2.4')
    expect(card.get('[data-testid="output-tps-p10"]').text()).toBe('3.7')
    expect(card.get('[data-testid="output-tps-avg"]').text()).toBe('277.8')
    expect(card.get('[data-testid="output-tps-samples"]').text()).toContain('4')
    expect(card.text()).toContain('tok/s (P50)')
    expect(card.text()).not.toContain('242.7')
    await wrapper.setProps({ fullscreen: true, overview: { ...overview, output_tps: { p5: 0.01, p10: 0.02, p50: 0.04, avg: 0.05, sample_count: 1 } } })
    expect(card.get('[data-testid="output-tps-p50"]').text()).toBe('0.0')
    expect(card.get('[data-testid="output-tps-samples"]').text()).toContain('1')
    wrapper.unmount()
  })

  it.each([undefined, null, { p5: null, p10: null, p50: null, avg: null, sample_count: 0 }])('shows missing output TPS as unavailable, not zero: %s', async (output_tps) => {
    const wrapper = mountHeader({ ...overview, output_tps })
    await flushPromises()
    for (const metric of ['p5', 'p10', 'p50', 'avg']) {
      expect(wrapper.get(`[data-testid="output-tps-${metric}"]`).text()).toBe('—')
    }
    expect(wrapper.get('[data-testid="output-tps-samples"]').text()).toContain(output_tps == null ? '—' : '0')
    wrapper.unmount()
  })

})

describe('OpsDashboardHeader health diagnosis', () => {
  const sample = {
    ...overview, system_metrics_unavailable: false, system_metrics_nodes: [freshNode('A')], request_count_total: 353, request_count_sla: 353, error_count_total: 3,
    error_rate: 3 / 353, upstream_error_rate: 2 / 353, sla: 350 / 353, health_score: 95,
    qps: { current: 0, peak: 1, avg: 0.1 }, ttft: { p99_ms: 21827 },
  } as OpsDashboardOverview
  const thresholds = { sla_percent_min: 99.5, ttft_p99_ms_max: 60000, request_error_rate_percent_max: 5, upstream_error_rate_percent_max: 5 }
  function report(wrapper: ReturnType<typeof mountHeader>) {
    return (wrapper.vm as unknown as { diagnosisReport: Array<{ type: string; message: string }> }).diagnosisReport
  }
  it('uses configured thresholds and keeps traffic visible when current QPS is zero', async () => {
    const wrapper = mountHeader(sample)
    await wrapper.setProps({ thresholds })
    expect(report(wrapper).map(item => item.message)).toEqual([expect.stringContaining('admin.ops.diagnosis.slaLow')])
    expect(report(wrapper)[0]?.type).toBe('warning')
    expect(wrapper.text()).toContain('95')
    expect(report(wrapper).some(item => /idle|ttftHigh|errorElevated|healthLow/.test(item.message))).toBe(false)
    await wrapper.setProps({ thresholds: { ...thresholds, ttft_p99_ms_max: 10000 } })
    expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({ type: 'critical', message: expect.stringContaining('ttftHigh') })]))
    wrapper.unmount()
  })
  it('retains genuine failures and avoids duplicate health-score warnings', async () => {
    const wrapper = mountHeader({ ...sample, error_rate: 0.12, sla: 0.88, health_score: 40 })
    await wrapper.setProps({ thresholds })
    expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({ type: 'critical', message: expect.stringContaining('errorHigh') }), expect.objectContaining({ type: 'critical', message: expect.stringContaining('slaCritical') })]))
    expect(report(wrapper).some(item => item.message.includes('healthCritical'))).toBe(false)
    wrapper.unmount()
  })
  it('does not invent SLA failures without eligible requests', async () => {
    const wrapper = mountHeader({ ...sample, request_count_sla: 0, error_count_total: 0, error_rate: 0, upstream_error_rate: 0, sla: 0 })
    await wrapper.setProps({ thresholds })
    expect(report(wrapper).some(item => item.message.includes('sla'))).toBe(false)
    wrapper.unmount()
  })
  it('reports storage and overdue jobs even without requests', async () => {
    const wrapper = mountHeader({      
...sample, request_count_total: 0, request_count_sla: 0, error_count_total: 0, error_rate: 0, system_metrics: { db_ok: false }, health_score: 49,
      job_heartbeats: [{ job_name: 'ops_metrics_collector', health_status: 'stale', updated_at: new Date().toISOString() }]    
})
    await wrapper.setProps({ thresholds })
    expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({ type: 'critical', message: 'admin.ops.diagnosis.dbDown' }), expect.objectContaining({ type: 'warning', message: expect.stringContaining('jobStale') })]))
    expect(report(wrapper).some(item => item.message.includes('idle'))).toBe(false)
    wrapper.unmount()
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({
    listGroups: vi.fn().mockResolvedValue([]),
  })
  return { ...actual, useOperationsServices: () => services }
})

describe('real header node integration', () => {
 it('renders the new node panel and keeps compact metric cards wired to detail events', async () => {
  const wrapper = mountHeader({ ...overview, system_metrics_nodes: [freshNode('A'), freshNode('B')], system_metrics_unavailable: false })
  await flushPromises()
  expect(wrapper.findAll('[data-testid="node-metric-row"]')).toHaveLength(2)
  expect(wrapper.find('.ops-overview-grid').exists()).toBe(true)
  expect(wrapper.get('.ops-realtime-card circle').attributes('r')).toBe('33.5')
  await wrapper.get('.ops-requests-card button').trigger('click')
  expect(wrapper.emitted('openRequestDetails')).toHaveLength(1)
  await wrapper.get('[data-testid="metric-jobs"] button').trigger('click')
  expect((wrapper.vm as unknown as { showJobsDetails: boolean }).showJobsDetails).toBe(true)
  wrapper.unmount()
 })
 it.each([undefined, true])('does not publish healthy/idle cluster facts when unavailable=%s', async unavailable => {
  const wrapper = mountHeader({ ...overview, health_score: 100, request_count_total: 0, request_count_sla: 0, error_count_total: 0,
    system_metrics: { db_ok: true, redis_ok: true, cpu_usage_percent: 0, memory_usage_percent: 0 } as OpsDashboardOverview['system_metrics'],
    system_metrics_unavailable: unavailable })
  await flushPromises()
  expect(wrapper.findAll('[data-testid="node-metric-row"]')).toHaveLength(0)
  const state = wrapper.vm as unknown as { isSystemIdle: boolean; healthScoreValue: number | null }
  expect(state.isSystemIdle).toBe(false); expect(state.healthScoreValue).toBeNull()
  expect(wrapper.text()).toContain('admin.ops.nodes.unavailable')
  wrapper.unmount()
 })
 it('ages both the header health and node row without claiming an old sample is current', async () => {
  vi.useFakeTimers()
  const wrapper = mountHeader({ ...overview, health_score: 95, system_metrics_nodes: [freshNode('A')], system_metrics_unavailable: false })
  await flushPromises()
  expect((wrapper.vm as unknown as { healthScoreValue: number | null }).healthScoreValue).toBe(95)
  await vi.advanceTimersByTimeAsync(31000)
  expect((wrapper.vm as unknown as { healthScoreValue: number | null }).healthScoreValue).toBeNull()
  expect(wrapper.get('[data-testid="node-status"]').text()).toBe('admin.ops.nodes.stale')
  wrapper.unmount(); expect(vi.getTimerCount()).toBe(0)
 })
})
