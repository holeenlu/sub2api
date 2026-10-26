import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import type { OpsDashboardOverview } from '@/api/admin/ops'

vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ enabled: true, summary: null }) } }))
vi.mock('@/stores', () => ({ useAdminSettingsStore: () => ({ opsRealtimeMonitoringEnabled: true, setOpsRealtimeMonitoringEnabledLocal: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key })
}))

const overview = { tps: { avg: 242.7 } } as OpsDashboardOverview
function mountHeader(value = overview) {
  return mount(OpsDashboardHeader, {
    props: { overview: value, platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null },
    global: { stubs: { Select: true, HelpTooltip: true, BaseDialog: true, Icon: true } }
  })
}

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
  ...overview, request_count_total: 353, request_count_sla: 353, error_count_total: 3,
  error_rate: 3 / 353, upstream_error_rate: 2 / 353, sla: 350 / 353, health_score: 95,
  qps: { current: 0, peak: 1, avg: 0.1 }, ttft: { p99_ms: 21827 },
 } as OpsDashboardOverview
 const thresholds = { sla_percent_min: 99.5, ttft_p99_ms_max: 60000, request_error_rate_percent_max: 5, upstream_error_rate_percent_max: 5 }
 function report(wrapper: ReturnType<typeof mountHeader>) {
  return (wrapper.vm as unknown as { diagnosisReport: Array<{type: string; message: string}> }).diagnosisReport
 }
 it('uses configured thresholds and keeps traffic visible when current QPS is zero', async () => {
  const wrapper=mountHeader(sample)
  await wrapper.setProps({thresholds})
  expect(report(wrapper).map(item=>item.message)).toEqual([expect.stringContaining('admin.ops.diagnosis.slaLow')])
  expect(report(wrapper)[0]?.type).toBe('warning')
  expect(wrapper.text()).toContain('95')
  expect(report(wrapper).some(item=>/idle|ttftHigh|errorElevated|healthLow/.test(item.message))).toBe(false)
  await wrapper.setProps({thresholds:{...thresholds,ttft_p99_ms_max:10000}})
  expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({type:'critical',message:expect.stringContaining('ttftHigh')})]))
  wrapper.unmount()
 })
 it('retains genuine failures and avoids duplicate health-score warnings', async () => {
  const wrapper=mountHeader({...sample,error_rate:0.12,sla:0.88,health_score:40})
  await wrapper.setProps({thresholds})
  expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({type:'critical',message:expect.stringContaining('errorHigh')}),expect.objectContaining({type:'critical',message:expect.stringContaining('slaCritical')})]))
  expect(report(wrapper).some(item=>item.message.includes('healthCritical'))).toBe(false)
  wrapper.unmount()
 })
 it('does not invent SLA failures without eligible requests', async () => {
  const wrapper=mountHeader({...sample,request_count_sla:0,error_count_total:0,error_rate:0,upstream_error_rate:0,sla:0})
  await wrapper.setProps({thresholds})
  expect(report(wrapper).some(item=>item.message.includes('sla'))).toBe(false)
  wrapper.unmount()
 })
 it('reports storage and overdue jobs even without requests', async () => {
  const wrapper=mountHeader({...sample,request_count_total:0,request_count_sla:0,error_count_total:0,error_rate:0,system_metrics:{db_ok:false},health_score:49,
   job_heartbeats:[{job_name:'ops_metrics_collector',health_status:'stale',updated_at:new Date().toISOString()}]})
  await wrapper.setProps({thresholds})
  expect(report(wrapper)).toEqual(expect.arrayContaining([expect.objectContaining({type:'critical',message:'admin.ops.diagnosis.dbDown'}),expect.objectContaining({type:'warning',message:expect.stringContaining('jobStale')})]))
  expect(report(wrapper).some(item=>item.message.includes('idle'))).toBe(false)
  wrapper.unmount()
 })
})
