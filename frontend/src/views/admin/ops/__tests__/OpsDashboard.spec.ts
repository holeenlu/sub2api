import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import OpsDashboard from '../OpsDashboard.vue'

const mocks = vi.hoisted(() => ({
  getDashboardSnapshotV2: vi.fn(), getThroughputTrend: vi.fn(), getAdvancedSettings: vi.fn(),
  getMetricThresholds: vi.fn(), getLatencyHistogram: vi.fn(), getErrorDistribution: vi.fn(),
  getDashboardOverview: vi.fn(), getErrorTrend: vi.fn(),
}))
vi.mock('@/api/admin/ops', async (importOriginal) => ({ ...await importOriginal<typeof import('@/api/admin/ops')>(), opsAPI: mocks, default: mocks }))
vi.mock('@/stores', () => ({
  useAdminSettingsStore: () => ({ fetch: vi.fn().mockResolvedValue(undefined), opsMonitoringEnabled: true, opsQueryModeDefault: 'auto' }),
  useAppStore: () => ({ showError: vi.fn() }),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSuperAdmin: true }) }))
vi.mock('vue-router', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-router')>(), useRoute: () => ({ query: {} }), useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers() })
afterEach(() => vi.useRealTimers())

describe('Ops dashboard first data display', () => {
  it('renders the core snapshot without waiting for preferences or the switch chart', async () => {
    let finishSettings!: (value: any) => void
    mocks.getAdvancedSettings.mockImplementation(() => new Promise(resolve => { finishSettings = resolve }))
    mocks.getLatencyHistogram.mockResolvedValue(null)
    mocks.getErrorDistribution.mockResolvedValue(null)
    mocks.getThroughputTrend.mockImplementation(() => new Promise(() => {}))
    mocks.getDashboardSnapshotV2.mockResolvedValue({ overview: { marker: 'ready' }, throughput_trend: { points: [] }, error_trend: { points: [] } })
    const wrapper = shallowMount(OpsDashboard, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent({ name: 'OpsDashboardSkeleton' }).exists()).toBe(false)
    const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
    expect(header.props('overview')).toEqual({ marker: 'ready' })
    expect(header.props('loading')).toBe(false)
    expect(wrapper.findComponent({ name: 'OpsSwitchRateTrendChart' }).props('loading')).toBe(true)
    const signal = mocks.getThroughputTrend.mock.lastCall![1].signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    finishSettings({ auto_refresh_enabled: true, auto_refresh_interval_seconds: 30 })
    await flushPromises()
    vi.advanceTimersByTime(60000)
    expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
