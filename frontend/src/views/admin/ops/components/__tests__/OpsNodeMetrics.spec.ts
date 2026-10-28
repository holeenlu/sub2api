import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsNodeMetrics from '../OpsNodeMetrics.vue'
import type { OpsNodeMetricsSnapshot } from '@/api/admin/ops'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: "en" }, t: (key: string, params?: Record<string, unknown>) => params ? key + JSON.stringify(params) : key }) }))
vi.mock('@/i18n/localeUtils', () => ({ getIntlLocale: () => 'en-US' }))
afterEach(() => vi.useRealTimers())
function node(id: string, overrides: Partial<OpsNodeMetricsSnapshot> = {}): OpsNodeMetricsSnapshot {
 return { id: 0, window_minutes: 1, node_id: id, node_name: 'Node ' + id, created_at: '2026-10-26T02:51:00Z', last_seen_at: '2026-10-26T02:51:00Z', age_seconds: 0, stale: false, cpu_scope: 'host', memory_scope: 'host', cpu_capacity: 4, cpu_usage_percent: 10, memory_usage_percent: 20, memory_used_mb: 1024, memory_total_mb: 4096, db_ok: true, redis_ok: true, db_conn_active: 1, db_conn_idle: 9, db_max_open_conns: 60, redis_conn_total: 8, redis_conn_idle: 7, redis_pool_size: 32, ...overrides }
}
describe('per-node resource metrics', () => {
 it('renders every node with its own capacities, scopes and pressure', () => {
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:[node('A'),node('B',{cpu_usage_percent:95,db_max_open_conns:20,redis_pool_size:16})],unavailable:false}})
  expect(wrapper.findAll('article')).toHaveLength(2)
  expect(wrapper.get('[data-testid="node-A"] [data-testid="metric-database"]').text()).toContain('17%')
  expect(wrapper.get('[data-testid="node-B"] [data-testid="metric-database"]').text()).toContain('50%')
  expect(wrapper.get('[data-testid="node-A"] [data-testid="metric-redis"]').text()).toContain('25%')
  expect(wrapper.get('[data-testid="node-B"] [data-testid="metric-redis"]').text()).toContain('50%')
  expect(wrapper.get('[data-testid="node-B"] [data-testid="metric-cpu"]').text()).toContain('95.0%')
  expect(wrapper.get('[data-testid="node-A"] [data-testid="metric-memory"]').text()).toContain('1.0 GiB / 4.0 GiB')
  expect(wrapper.findAll('[data-testid="node-metric-row"]')).toHaveLength(2)
  expect(wrapper.get('[data-testid="node-metric-row"]').classes()).toContain('md:grid-cols-6')
  expect(wrapper.get('[data-testid="metric-cpu"]').text()).toContain('80%')
  wrapper.unmount()
 })
 it('does not display missing metrics as zero or a healthy cluster on Redis failure', () => {
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:[node('A',{cpu_usage_percent:null,db_conn_idle:null,redis_pool_size:null})],unavailable:true}})
  expect(wrapper.get('[data-testid="metric-cpu"]').text()).toContain('—')
  expect(wrapper.get('[data-testid="metric-database"]').text()).not.toContain('0.0%')
  expect(wrapper.get('[data-testid="metric-redis"]').text()).toContain('—')
  expect(wrapper.find('[data-testid="node-metrics-unavailable"]').exists()).toBe(true)
  wrapper.unmount()
 })
 it('ages stale rows without relying on browser versus server clock alignment', async () => {
  vi.useFakeTimers();vi.setSystemTime(new Date('2030-01-01T00:00:00Z'))
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:[node('A')],unavailable:false}})
  expect(wrapper.get('[data-testid="node-status"]').text()).toBe('admin.ops.nodes.online')
  await vi.advanceTimersByTimeAsync(31000)
  expect(wrapper.get('[data-testid="node-status"]').text()).toBe('admin.ops.nodes.stale')
  expect(wrapper.text()).toContain('admin.ops.nodes.lastKnown')
  await wrapper.setProps({nodes:[node('A')]})
  expect(wrapper.get('[data-testid="node-status"]').text()).toBe('admin.ops.nodes.online')
  wrapper.unmount()
 })
 it('keeps offline nodes visible and renders connection failures distinctly', () => {
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:[node('A'),node('B',{stale:true,db_ok:false})],unavailable:false}})
  expect(wrapper.findAll('article')).toHaveLength(2)
  expect(wrapper.get('[data-testid="node-B"] [data-testid="node-status"]').text()).toBe('admin.ops.nodes.stale')
  expect(wrapper.get('[data-testid="node-B"] [data-testid="metric-database"]').text()).toContain('admin.ops.nodes.failed')
  wrapper.unmount()
 })
 it('adds exactly one six-card row for each additional node in the existing order', async () => {
  const jobs = [{job_name:'collector',health_status:'healthy',updated_at:new Date().toISOString()}]
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:[node('A'),node('B')],unavailable:false,jobHeartbeats:jobs}})
  const keys = ['metric-cpu','metric-memory','metric-database','metric-redis','metric-goroutines','metric-jobs']
  for (const row of wrapper.findAll('[data-testid="node-metric-row"]')) {
   expect(row.findAll(':scope > div').map(card=>card.attributes('data-testid'))).toEqual(keys)
  }
  await wrapper.setProps({nodes:[node('A'),node('B'),node('C')]})
  expect(wrapper.findAll('article')).toHaveLength(3)
  expect(wrapper.findAll('[data-testid="metric-jobs"]')).toHaveLength(3)
  expect(wrapper.get('[data-testid="node-C"] [data-testid="metric-goroutines"]').text()).toContain('admin.ops.noData')
  expect(wrapper.get('[data-testid="metric-jobs"]').text()).toContain('admin.ops.nodes.cluster')
  await wrapper.get('[data-testid="metric-jobs"] button').trigger('click')
  expect(wrapper.emitted('jobsDetails')).toHaveLength(1)
  wrapper.unmount()
 })

 it('limits the dashboard to three nodes and opens every node in a single dialog', async () => {
  const nodes=['A','B','C','D','E'].map(id=>node(id))
  const wrapper=mount(OpsNodeMetrics,{props:{nodes,unavailable:false},global:{stubs:{Teleport:true}}})
  expect(wrapper.findAll('article')).toHaveLength(3)
  expect(wrapper.find('[data-testid="node-D"]').exists()).toBe(false)
  expect(wrapper.get('[data-testid="node-overflow"]').text()).toContain('"total":5')
  await wrapper.get('[data-testid="view-all-nodes"]').trigger('click');await flushPromises()
  const dialog=wrapper.get('[role="dialog"]')
  expect(dialog.findAll('article')).toHaveLength(5)
  expect(dialog.find('[data-testid="node-E"]').exists()).toBe(true)
  expect(dialog.find('[data-testid="view-all-nodes"]').exists()).toBe(false)
  await dialog.get('button[aria-label="ui.closeModal"]').trigger('click');await flushPromises()
  expect(wrapper.findAll('article')).toHaveLength(3)
  wrapper.unmount()
 })
 it('omits the all-servers action for three or fewer nodes', () => {
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:['A','B','C'].map(id=>node(id)),unavailable:false}})
  expect(wrapper.find('[data-testid="view-all-nodes"]').exists()).toBe(false)
  wrapper.unmount()
 })

 it('does not refresh an old sample merely by opening the complete-cluster dialog', async () => {
  vi.useFakeTimers();vi.setSystemTime(new Date('2030-01-01T00:00:00Z'))
  const wrapper=mount(OpsNodeMetrics,{props:{nodes:['A','B','C','D'].map(id=>node(id)),unavailable:false},global:{stubs:{Teleport:true}}})
  await vi.advanceTimersByTimeAsync(31000)
  await wrapper.get('[data-testid="view-all-nodes"]').trigger('click');await flushPromises()
  expect(wrapper.get('[role="dialog"] [data-testid="node-D"] [data-testid="node-status"]').text()).toBe('admin.ops.nodes.stale')
  wrapper.unmount()
 })

})
