import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import OpsDashboard from '../OpsDashboard.vue'
import { useOperationsServices } from '@/features/operations/services'

const services = useOperationsServices()

const mocks = vi.hoisted(() => ({
  getDashboardSnapshotV2: vi.fn(), getThroughputTrend: vi.fn(), getAdvancedSettings: vi.fn(),
  getMetricThresholds: vi.fn(), getLatencyHistogram: vi.fn(), getErrorDistribution: vi.fn(),
  getDashboardOverview: vi.fn(), getErrorTrend: vi.fn(),
}))
vi.mock('@/features/operations/api/ops', async (importOriginal) => ({ ...await importOriginal<typeof import('@/features/operations/api/ops')>(), opsAPI: mocks, default: mocks }))

const { route, replace } = vi.hoisted(() => {
  const route: { query: import('vue-router').LocationQuery } = { query: {} }
  return { route, replace: vi.fn() }
})
vi.mock('vue-router', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-router')>(), useRoute: () => route, useRouter: () => ({ replace }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  route.query = {}
  services.scopeKey.value = 'session-a:admin:all'
})
afterEach(() => vi.useRealTimers())

describe('Ops dashboard first data display', () => {
  it('renders the core snapshot without waiting for preferences or the switch chart', async () => {
    let finishSettings!: (value: any) => void
    mocks.getAdvancedSettings.mockImplementation(() => new Promise(resolve => { finishSettings = resolve }))
    mocks.getLatencyHistogram.mockResolvedValue(null)
    mocks.getErrorDistribution.mockResolvedValue(null)
    mocks.getThroughputTrend.mockImplementation(() => new Promise(() => { }))
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

describe('Ops dashboard automatic refresh', () => {
  const snapshot = { overview: { health_score: 95 }, throughput_trend: { points: [] }, error_trend: { points: [] } }

  function mountDashboard(enabled = true, seconds = 15) {
    mocks.getAdvancedSettings.mockResolvedValue({ auto_refresh_enabled: enabled, auto_refresh_interval_seconds: seconds })
    mocks.getDashboardSnapshotV2.mockResolvedValue(snapshot)
    mocks.getThroughputTrend.mockResolvedValue({ points: [] })
    mocks.getLatencyHistogram.mockResolvedValue(null)
    mocks.getErrorDistribution.mockResolvedValue(null)
    return shallowMount(OpsDashboard, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
  }

  it('keeps unrelated null/array query values while syncing dashboard filters and API DTOs', async () => {
    route.query = { tag: ['first', null, 'second'], flag: null, empty: '' }
    vi.setSystemTime(new Date('2026-10-26T12:00:00Z'))
    const wrapper = mountDashboard(false)
    try {
      await flushPromises()
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      header.vm.$emit('update:platform', 'openai')
      header.vm.$emit('update:group', 7)
      header.vm.$emit('update:queryMode', 'raw')
      header.vm.$emit('update:timeRange', '6h')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(250)
      expect(replace).toHaveBeenLastCalledWith({ query: { tag: ['first', null, 'second'], flag: null, empty: '', platform: 'openai', group_id: '7', mode: 'raw', tr: '6h' } })
      expect(mocks.getDashboardSnapshotV2.mock.lastCall?.[0]).toEqual({ platform: 'openai', group_id: 7, mode: 'raw', time_range: '6h' })
      expect(mocks.getThroughputTrend.mock.lastCall?.[0]).toEqual({ platform: 'openai', group_id: 7, mode: 'raw', start_time: '2026-10-26T07:00:00.000Z', end_time: '2026-10-26T12:00:00.000Z' })
    } finally { wrapper.unmount() }
  })

  it('sends custom timestamps unchanged and falls back to 1h when the range is incomplete', async () => {
    const wrapper = mountDashboard(false)
    try {
      await flushPromises()
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      header.vm.$emit('update:timeRange', 'custom')
      await flushPromises()
      expect(mocks.getDashboardSnapshotV2.mock.lastCall?.[0]).toMatchObject({ time_range: '1h' })
      header.vm.$emit('update:customTimeRange', '2026-10-26T00:00:00+08:00', '2026-10-26T12:00:00+08:00')
      header.vm.$emit('refresh')
      await flushPromises()
      expect(mocks.getDashboardSnapshotV2.mock.lastCall?.[0]).toMatchObject({ start_time: '2026-10-26T00:00:00+08:00', end_time: '2026-10-26T12:00:00+08:00' })
      expect(mocks.getDashboardSnapshotV2.mock.lastCall?.[0]).not.toHaveProperty('time_range')
    } finally { wrapper.unmount() }
  })

  it.each([15, 30, 60])('refreshes as the %ss countdown reaches zero and stops on unmount', async (seconds) => {
    const wrapper = mountDashboard(true, seconds)
    try {
      await flushPromises()
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      const firstUpdated = header.props('lastUpdated')
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(seconds * 1000 - 1)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
      expect(header.props('autoRefreshCountdown')).toBe(1)
      await vi.advanceTimersByTimeAsync(1)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      expect(header.props('autoRefreshCountdown')).toBe(seconds)
      expect(header.props('lastUpdated')).not.toBe(firstUpdated)
      await vi.advanceTimersByTimeAsync(seconds * 1000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    } finally { wrapper.unmount() }
    await vi.advanceTimersByTimeAsync(60000)
    expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('waits while a refresh is pending and restarts the countdown when it completes', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      let finish: (value: typeof snapshot) => void = () => { throw new Error('Refresh not started') }
      mocks.getDashboardSnapshotV2.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      await vi.advanceTimersByTimeAsync(15000)
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      expect(header.props('autoRefreshCountdown')).toBe(0)
      expect(header.props('loading')).toBe(true)
      await vi.advanceTimersByTimeAsync(30000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      finish(snapshot)
      await flushPromises()
      expect(header.props('loading')).toBe(false)
      expect(header.props('autoRefreshCountdown')).toBe(15)
      await vi.advanceTimersByTimeAsync(14000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(1000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    } finally { wrapper.unmount() }
  })

  it('waits a full interval after snapshot and fallback failures before retrying', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const unavailable = new Error('temporary outage')
      mocks.getDashboardSnapshotV2.mockRejectedValueOnce(unavailable)
      mocks.getDashboardOverview.mockRejectedValueOnce(unavailable)
      mocks.getErrorTrend.mockRejectedValueOnce(unavailable)
      mocks.getThroughputTrend.mockRejectedValue(unavailable)
      await vi.advanceTimersByTimeAsync(15000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      expect(services.notifyError).toHaveBeenCalled()
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      expect(header.props('loading')).toBe(false)
      expect(header.props('autoRefreshCountdown')).toBe(15)
      await vi.advanceTimersByTimeAsync(14000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(1000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    } finally { wrapper.unmount() }
  })

  it('does not reload settings or data when the dialog is closed without saving', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      await vi.advanceTimersByTimeAsync(5000)
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      header.vm.$emit('openSettings')
      await flushPromises()
      const dialog = wrapper.findComponent({ name: 'OpsSettingsDialog' })
      expect(dialog.props('show')).toBe(true)
      dialog.vm.$emit('close')
      await flushPromises()
      expect(dialog.props('show')).toBe(false)
      expect(mocks.getAdvancedSettings).toHaveBeenCalledTimes(1)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
      expect(header.props('autoRefreshCountdown')).toBe(10)
    } finally { wrapper.unmount() }
  })

  it('keeps manual mode and applies saved settings once even when saved closes the dialog', async () => {
    const wrapper = mountDashboard(false)
    try {
      await flushPromises()
      await vi.advanceTimersByTimeAsync(30000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      header.vm.$emit('openSettings')
      await flushPromises()
      let finishSettings: (value: object) => void = () => { throw new Error('Settings not requested') }
      mocks.getAdvancedSettings.mockReturnValueOnce(new Promise(resolve => { finishSettings = resolve }))
      const dialog = wrapper.findComponent({ name: 'OpsSettingsDialog' })
      dialog.vm.$emit('saved')
      dialog.vm.$emit('close')
      await flushPromises()
      expect(mocks.getAdvancedSettings).toHaveBeenCalledTimes(2)
      finishSettings({ auto_refresh_enabled: true, auto_refresh_interval_seconds: 30 })
      await flushPromises()
      expect(header.props('autoRefreshEnabled')).toBe(true)
      expect(header.props('autoRefreshCountdown')).toBe(30)
      expect(mocks.getMetricThresholds).toHaveBeenCalledTimes(2)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(30000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)

      mocks.getAdvancedSettings.mockResolvedValue({ auto_refresh_enabled: false, auto_refresh_interval_seconds: 30 })
      dialog.vm.$emit('saved')
      await flushPromises()
      expect(header.props('autoRefreshEnabled')).toBe(false)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(4)
      await vi.advanceTimersByTimeAsync(60000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(4)
    } finally { wrapper.unmount() }
  })

  it('cancels a pending refresh and ignores late results after scope loss', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      let finish: (value: typeof snapshot) => void = () => { throw new Error('Refresh not started') }
      mocks.getDashboardSnapshotV2.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      await vi.advanceTimersByTimeAsync(15000)
      const signal: AbortSignal = mocks.getDashboardSnapshotV2.mock.lastCall?.[1].signal
      services.scopeKey.value = null
      await flushPromises()
      expect(signal.aborted).toBe(true)
      finish(snapshot)
      await flushPromises()
      const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
      expect(header.props('overview')).toBeNull()
      expect(header.props('loading')).toBe(false)
      expect(header.props('autoRefreshCountdown')).toBe(0)
      await vi.advanceTimersByTimeAsync(60000)
      expect(mocks.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
    } finally { wrapper.unmount() }
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({
    notifyError: vi.fn(),
  })
  return { ...actual, useOperationsServices: () => services }
})
