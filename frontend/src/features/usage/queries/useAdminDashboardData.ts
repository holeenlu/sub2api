import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { dashboardAPI } from '../adminDashboard'
import type { AdminDashboardStats } from '../api/dashboardTypes'
import type { TrendDataPoint, ModelStat, UserUsageTrendPoint, UserSpendingRankingItem } from '../api/types'
import { getLast24HoursRangeDates } from '../model/usageDates'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
export function useAdminDashboardData() {
  const services = useUsageServices(), { t } = useI18n()
  const snapshot = useUsageRequest(services.scopeKey), users = useUsageRequest(services.scopeKey), ranking = useUsageRequest(services.scopeKey)
  const stats = ref<AdminDashboardStats | null>(null), loading = ref(false), chartsLoading = ref(false), userTrendLoading = ref(false), rankingLoading = ref(false), rankingError = ref(false)
  const trendData = ref<TrendDataPoint[]>([]), modelStats = ref<ModelStat[]>([]), userTrend = ref<UserUsageTrendPoint[]>([]), userTrendMetric = ref<'tokens' | 'actual_cost'>('tokens')
  const rankingItems = ref<UserSpendingRankingItem[]>([]), rankingTotalActualCost = ref<number>(), rankingTotalRequests = ref<number>(), rankingTotalTokens = ref<number>()
  const range = getLast24HoursRangeDates(), startDate = ref(range.start), endDate = ref(range.end), granularity = ref<'day' | 'hour'>('hour')
  async function loadDashboardSnapshot(includeStats: boolean): Promise<void> {
    const ticket = snapshot.begin(); if (!ticket.isCurrent()) return
    const include = includeStats || stats.value === null
    const params = { start_date: startDate.value, end_date: endDate.value, granularity: granularity.value, include_stats: include,
      include_trend: true, include_model_stats: true, include_group_stats: false, include_users_trend: false }
    if (include && !stats.value) loading.value = true
    chartsLoading.value = true
    try {
      const result = await dashboardAPI.getSnapshotV2(params, ticket)
      if (!ticket.isCurrent()) return
      if (include && result.stats) stats.value = result.stats
      trendData.value = result.trend ?? []; modelStats.value = result.models ?? []
    } catch { if (ticket.isCurrent()) services.notifyError(t('admin.dashboard.failedToLoad')) }
    finally { if (ticket.isCurrent()) { loading.value = false; chartsLoading.value = false } }
  }
  async function loadUsersTrend(): Promise<void> {
    const ticket = users.begin(); if (!ticket.isCurrent()) return
    const params = { start_date: startDate.value, end_date: endDate.value, granularity: granularity.value, limit: 12, metric: userTrendMetric.value }
    userTrendLoading.value = true
    try { const result = await dashboardAPI.getUserUsageTrend(params, ticket); if (ticket.isCurrent()) userTrend.value = result.trend ?? [] }
    catch { if (ticket.isCurrent()) userTrend.value = [] }
    finally { if (ticket.isCurrent()) userTrendLoading.value = false }
  }
  async function loadUserSpendingRanking(): Promise<void> {
    const ticket = ranking.begin(); if (!ticket.isCurrent()) return
    const params = { start_date: startDate.value, end_date: endDate.value, limit: 12 }
    rankingLoading.value = true; rankingError.value = false
    try {
      const result = await dashboardAPI.getUserSpendingRanking(params, ticket)
      if (!ticket.isCurrent()) return
      rankingItems.value = result.ranking ?? []; rankingTotalActualCost.value = result.total_actual_cost; rankingTotalRequests.value = result.total_requests; rankingTotalTokens.value = result.total_tokens
    } catch {
      if (ticket.isCurrent()) { rankingItems.value = []; rankingTotalActualCost.value = undefined; rankingTotalRequests.value = undefined; rankingTotalTokens.value = undefined; rankingError.value = true }
    } finally { if (ticket.isCurrent()) rankingLoading.value = false }
  }
  async function loadDashboardStats(): Promise<void> { await Promise.all([loadDashboardSnapshot(true), loadUsersTrend(), loadUserSpendingRanking()]) }
  async function loadChartData(): Promise<void> { await Promise.all([loadDashboardSnapshot(false), loadUsersTrend(), loadUserSpendingRanking()]) }
  function setUserTrendMetric(metric: 'tokens' | 'actual_cost'): void { if (userTrendMetric.value !== metric) { userTrendMetric.value = metric; userTrend.value = []; void loadUsersTrend() } }
  function onDateRangeChange(range: { startDate: string; endDate: string; preset: string | null }): void {
    startDate.value = range.startDate; endDate.value = range.endDate
    granularity.value = Math.ceil((new Date(range.endDate).getTime() - new Date(range.startDate).getTime()) / 86400000) <= 1 ? 'hour' : 'day'
    void loadChartData()
  }
  watch(services.scopeKey, () => {
    stats.value = null; trendData.value = []; modelStats.value = []; userTrend.value = []; rankingItems.value = []
    rankingTotalActualCost.value = undefined; rankingTotalRequests.value = undefined; rankingTotalTokens.value = undefined
    loading.value = false; chartsLoading.value = false; userTrendLoading.value = false; rankingLoading.value = false; rankingError.value = false
    if (services.scopeKey.value !== null) void loadDashboardStats()
  }, { flush: 'sync' })
  return { stats, loading, chartsLoading, userTrendLoading, rankingLoading, rankingError, trendData, modelStats, userTrend, userTrendMetric,
    rankingItems, rankingTotalActualCost, rankingTotalRequests, rankingTotalTokens, startDate, endDate, granularity, loadDashboardStats, loadChartData, setUserTrendMetric, onDateRangeChange }
}
