import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { MonitorDisplaySettings } from '../ports'
import type { MonitorMetric, MonitorModelRow, MonitorSnapshot, MonitorUserRow } from '../../api'
import { installMonitorTestServices } from './services'
import ChannelStatusV2Page from '../pages/ChannelStatusV2Page.vue'

const { api, route, replace } = vi.hoisted(() => ({
  api: { getDimensions: vi.fn(), getSnapshot: vi.fn(), getMatrix: vi.fn(), getModels: vi.fn(), getErrors: vi.fn(), getUsers: vi.fn() },
  route: { meta: { shell: 'user' }, query: {} as Record<string, string> },
  replace: vi.fn(),
}))
vi.mock('../../api', async original => ({ ...await original<typeof import('../../api')>(), monitorAPI: api }))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace }) }))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, te: () => false, locale: ref('en') }),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}
const metrics: MonitorMetric = {
  success_requests: 1, error_requests: 0, request_count: 1, token_count: 1,
  rpm: 1, tpm: 1, error_rate: 0, cache_rate: 0,
  cache_rate_numerator: 0, cache_rate_denominator: 1,
  ttft: { sample_count: 1, p50_ms: 1, p95_ms: 1, avg_ms: 1 },
  duration: { sample_count: 1, p50_ms: 1, p95_ms: 1, avg_ms: 1 },
}
const health = { overall: 'healthy', error_rate: 'healthy', ttft: 'healthy', minimum_sample: 1 } as const
const snapshot: MonitorSnapshot = {
  config: {
    version: 2, enabled: true, refresh_interval_seconds: 60, platforms: [], group_ids: [],
    health_thresholds: { minimum_sample: 1, warning_error_rate: 0.1, critical_error_rate: 0.3,
      target_ttft_ms: 1000, warning_ttft_ms: 2000, critical_ttft_ms: 5000,
      warning_cache_rate: 0.7, critical_cache_rate: 0.4, error_weight: 0.5, ttft_weight: 0.3, cache_weight: 0.2 },
  },
  coverage: { requested_start: '', coverage_start: '', data_through: '', computed_at: '',
    aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 60 },
  metrics, health, trend: [],
}
const model = (name: string): MonitorModelRow => ({ platform: 'openai', model: name, metrics, health })
const scopeKey = ref<string | null>('actor-a:user')
const canReadAdmin = ref(false)
const displaySettings = ref<MonitorDisplaySettings>({ hideThroughput: false, hideUserRanking: false, showQuota: false })
let services: ReturnType<typeof installMonitorTestServices>['services']

enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })
beforeEach(() => {
  vi.resetAllMocks()
  scopeKey.value = 'actor-a:user'
  route.meta.shell = 'user'
  route.query = {}
  canReadAdmin.value = false
  displaySettings.value = { hideThroughput: false, hideUserRanking: false, showQuota: false }
  services = installMonitorTestServices({ scopeKey }, { canReadAdmin, displaySettings }).services
  api.getDimensions.mockResolvedValue({ platforms: [], groups: [], models: [] })
  api.getSnapshot.mockResolvedValue(snapshot)
  api.getMatrix.mockResolvedValue({ group_by: 'platform_group', coverage: snapshot.coverage, items: [] })
  api.getModels.mockResolvedValue({ items: [] })
  api.getErrors.mockResolvedValue({ items: [] })
  api.getUsers.mockResolvedValue({ items: [] })
  replace.mockResolvedValue(undefined)
})

describe('ChannelStatusV2Page request lifecycle', () => {
  it('rejects a late model response after switching actors', async () => {
    const old = deferred<{ items: MonitorModelRow[] }>()
    api.getModels.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ items: [model('new-actor-model')] })
    const wrapper = shallowMount(ChannelStatusV2Page)
    await flushPromises()
    const oldSignal = api.getModels.mock.calls[0][2] as AbortSignal
    scopeKey.value = 'actor-b:user'
    await flushPromises()
    old.resolve({ items: [model('old-actor-model')] })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('new-actor-model')
    expect(wrapper.text()).not.toContain('old-actor-model')
    expect(services.notifyError).not.toHaveBeenCalled()
  })

  it('clears private rows and stops polling on logout', async () => {
    vi.useFakeTimers()
    api.getModels.mockResolvedValue({ items: [model('private-model')] })
    const wrapper = shallowMount(ChannelStatusV2Page)
    await flushPromises()
    expect(wrapper.text()).toContain('private-model')
    scopeKey.value = null
    await flushPromises()
    const calls = api.getSnapshot.mock.calls.length
    await vi.advanceTimersByTimeAsync(180000)
    expect(api.getSnapshot).toHaveBeenCalledTimes(calls)
    expect(wrapper.text()).not.toContain('private-model')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('discards pending user ranking when the public privacy switch hides it', async () => {
    route.query = { tab: 'users' }
    const old = deferred<{ items: MonitorUserRow[] }>()
    api.getUsers.mockReturnValueOnce(old.promise)
    const wrapper = shallowMount(ChannelStatusV2Page)
    await flushPromises()
    const signal = api.getUsers.mock.calls[0][2] as AbortSignal
    displaySettings.value = { ...displaySettings.value, hideUserRanking: true }
    await flushPromises()
    old.resolve({ items: [{ rank: 1, display_label: 'hidden-user', is_self: false, can_drilldown: false, metrics }] })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('hidden-user')
    expect(wrapper.findAll('[role="tab"]').some(tab => tab.text() === 'channelMonitorV2.tabs.users')).toBe(false)
    expect(replace).toHaveBeenLastCalledWith(expect.objectContaining({ query: expect.objectContaining({ tab: 'models' }) }))
  })

  it('keeps dimensions range-only and uses the admin endpoint only with the injected permission', async () => {
    route.meta.shell = 'admin'
    route.query = { range: '24h', platform: 'openai', group: '5', model: 'gpt-model' }
    canReadAdmin.value = true
    const wrapper = shallowMount(ChannelStatusV2Page)
    await flushPromises()
    expect(api.getDimensions).toHaveBeenCalledWith({ range: '24h', platforms: [], groupIds: [], models: [] }, true, expect.any(AbortSignal))
    expect(api.getSnapshot).toHaveBeenCalledWith({ range: '24h', platforms: ['openai'], groupIds: [5], models: ['gpt-model'] }, true, expect.any(AbortSignal))
    canReadAdmin.value = false
    await flushPromises()
    expect(api.getSnapshot.mock.calls.at(-1)?.[1]).toBe(false)
    wrapper.unmount()
    expect((api.getSnapshot.mock.calls.at(-1)?.[2] as AbortSignal).aborted).toBe(true)
  })
})
