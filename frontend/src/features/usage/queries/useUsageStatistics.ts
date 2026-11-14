import { computed, reactive, ref, shallowRef, watch } from 'vue'
import { usageAPI } from '../api/usage'
import { adminUsageAPI, type AdminUsageQueryParams, type AdminUsageStatsResponse } from '../api/adminUsage'
import type { UsageStatsResponse, UsageQueryParams, TrendDataPoint, ModelStat, GroupStat } from '../api/types'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
export type UsageModelSource = 'requested' | 'upstream' | 'mapping'
export function useUsageStatistics(options: {
  role: 'user' | 'admin'; filters: () => UsageQueryParams | AdminUsageQueryParams; granularity: () => 'day' | 'hour';
  onModels?: (models: ModelStat[]) => void
}) {
  const services = useUsageServices()
  const statsRequest = useUsageRequest(services.scopeKey)
  const modelsRequest = useUsageRequest(services.scopeKey)
  const chartsRequest = useUsageRequest(services.scopeKey)
  const usageStats = shallowRef<UsageStatsResponse | AdminUsageStatsResponse | null>(null)
  const modelStats = reactive<Record<UsageModelSource, ModelStat[]>>({ requested: [], upstream: [], mapping: [] })
  const loadedModelSources = reactive<Record<UsageModelSource, boolean>>({ requested: false, upstream: false, mapping: false })
  const trendData = shallowRef<TrendDataPoint[]>([]), groupStats = shallowRef<GroupStat[]>([])
  const endpointStatsLoading = ref(false), modelStatsLoading = ref(false), chartsLoading = ref(false)
  const inboundEndpointStats = computed(() => usageStats.value?.endpoints ?? [])
  const upstreamEndpointStats = computed(() => usageStats.value && 'upstream_endpoints' in usageStats.value ? usageStats.value.upstream_endpoints ?? [] : [])
  const endpointPathStats = computed(() => usageStats.value && 'endpoint_paths' in usageStats.value ? usageStats.value.endpoint_paths ?? [] : [])
  const requestedModelStats = computed(() => modelStats.requested)
  const upstreamModelStats = computed(() => modelStats.upstream)
  const mappingModelStats = computed(() => modelStats.mapping)
  function filters(): AdminUsageQueryParams {
    const params = options.filters()
    return { ...params, billing_mode: params.billing_mode ?? undefined, ...(options.role === 'admin' && !services.permissions.value.isSuperAdmin ? { account_id: undefined } : {}) }
  }
  async function loadStats(force = false): Promise<void> {
    const ticket = statsRequest.begin()
    if (!ticket.isCurrent()) return
    endpointStatsLoading.value = true
    try {
      const params = { ...filters(), ...(force ? { nocache: 1 } : {}) }
      const response = options.role === 'admin'
        ? await adminUsageAPI.getStats(params, ticket)
        : await usageAPI.getStats(params, undefined, ticket)
      if (ticket.isCurrent()) usageStats.value = response
    } catch { if (ticket.isCurrent()) usageStats.value = null }
    finally { if (ticket.isCurrent()) endpointStatsLoading.value = false }
  }
  function invalidateModelStatsCache(): void {
    for (const source of ['requested', 'upstream', 'mapping'] as const) loadedModelSources[source] = false
  }
  async function loadModelStats(source: UsageModelSource = 'requested', force = false): Promise<void> {
    if (options.role === 'user') source = 'requested'
    if (!force && loadedModelSources[source]) return
    const ticket = modelsRequest.begin()
    if (!ticket.isCurrent()) return
    modelStatsLoading.value = true
    try {
      const params = { ...filters() }
      const response = options.role === 'admin'
        ? await services.getAdminModelStats({ ...params, model_source: source }, ticket)
        : await usageAPI.getDashboardModels({ ...params, model_source: 'requested' }, ticket)
      if (!ticket.isCurrent()) return
      modelStats[source] = response.models ?? []
      loadedModelSources[source] = true
      options.onModels?.(modelStats[source])
    } catch {
      if (ticket.isCurrent()) { modelStats[source] = []; loadedModelSources[source] = false }
    } finally { if (ticket.isCurrent()) modelStatsLoading.value = false }
  }
  async function loadChartData(): Promise<void> {
    const ticket = chartsRequest.begin()
    if (!ticket.isCurrent()) return
    chartsLoading.value = true
    try {
      const params = { ...filters(), granularity: options.granularity(), include_trend: true, include_model_stats: false, include_group_stats: true }
      const snapshot = options.role === 'admin'
        ? await services.getAdminSnapshot({ ...params, include_stats: false, include_users_trend: false }, ticket)
        : await usageAPI.getDashboardSnapshotV2(params, ticket)
      if (!ticket.isCurrent()) return
      trendData.value = snapshot.trend ?? []; groupStats.value = snapshot.groups ?? []
    } catch { if (ticket.isCurrent()) { trendData.value = []; groupStats.value = [] } }
    finally { if (ticket.isCurrent()) chartsLoading.value = false }
  }
  watch(services.scopeKey, () => {
    usageStats.value = null; trendData.value = []; groupStats.value = []
    for (const source of ['requested', 'upstream', 'mapping'] as const) modelStats[source] = []
    invalidateModelStatsCache(); endpointStatsLoading.value = false; modelStatsLoading.value = false; chartsLoading.value = false
  }, { flush: 'sync' })
  return { usageStats, requestedModelStats, upstreamModelStats, mappingModelStats, loadedModelSources,
    trendData, groupStats, inboundEndpointStats, upstreamEndpointStats, endpointPathStats,
    endpointStatsLoading, modelStatsLoading, chartsLoading, loadStats, loadModelStats, loadChartData, invalidateModelStatsCache }
}
