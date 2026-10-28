import { ref, watch } from 'vue'
import { usageAPI, type UserDashboardStats } from '../api/usage'
import { getMyPlatformQuotas } from '../api/userRead'
import type { PlatformQuotaItem } from '@/features/access-policy/api/platformQuotaTypes';
import type { UsageLog, TrendDataPoint, ModelStat } from '../api/types'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
import { formatDateLocalInput } from '@/shared/lib/format'
export function useUserDashboardData() {
  const services = useUsageServices()
  const statsRequest = useUsageRequest(services.scopeKey), chartsRequest = useUsageRequest(services.scopeKey), recentRequest = useUsageRequest(services.scopeKey), quotasRequest = useUsageRequest(services.scopeKey)
  const stats = ref<UserDashboardStats | null>(null), loading = ref(false), loadingUsage = ref(false), loadingCharts = ref(false)
  const trendData = ref<TrendDataPoint[]>([]), modelStats = ref<ModelStat[]>([]), recentUsage = ref<UsageLog[]>([]), platformQuotas = ref<PlatformQuotaItem[] | null>(null)
  const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000))), endDate = ref(formatDateLocalInput(new Date())), granularity = ref<'day' | 'hour'>('day')
  async function loadStats(): Promise<void> {
    const ticket = statsRequest.begin(); if (!ticket.isCurrent()) return
    loading.value = true
    try { const value = await usageAPI.getDashboardStats(ticket); if (ticket.isCurrent()) stats.value = value }
    catch { /* Keep known statistics; never synthesize zeros after a failed read. */ }
    finally { if (ticket.isCurrent()) loading.value = false }
  }
  async function loadCharts(): Promise<void> {
    const ticket = chartsRequest.begin(); if (!ticket.isCurrent()) return
    const params = { start_date: startDate.value, end_date: endDate.value, granularity: granularity.value }
    loadingCharts.value = true
    try {
      const [trend, models] = await Promise.all([usageAPI.getDashboardTrend(params, ticket), usageAPI.getDashboardModels({ start_date: params.start_date, end_date: params.end_date }, ticket)])
      if (ticket.isCurrent()) { trendData.value = trend.trend ?? []; modelStats.value = models.models ?? [] }
    } catch { /* Retain visible data until refresh succeeds or the scope changes. */ }
    finally { if (ticket.isCurrent()) loadingCharts.value = false }
  }
  async function loadRecent(): Promise<void> {
    const ticket = recentRequest.begin(); if (!ticket.isCurrent()) return
    const params = { start_date: startDate.value, end_date: endDate.value }
    loadingUsage.value = true
    try { const result = await usageAPI.getByDateRange(params.start_date, params.end_date, undefined, ticket); if (ticket.isCurrent()) recentUsage.value = result.items.slice(0, 5) }
    catch { /* Recent activity is not inferred from a failed read. */ }
    finally { if (ticket.isCurrent()) loadingUsage.value = false }
  }
  async function loadPlatformQuotas(): Promise<void> {
    const ticket = quotasRequest.begin(); if (!ticket.isCurrent()) return
    try { const result = await getMyPlatformQuotas(ticket); if (ticket.isCurrent()) platformQuotas.value = result.platform_quotas ?? [] }
    catch { if (ticket.isCurrent()) platformQuotas.value = null }
  }
  watch(services.scopeKey, () => { stats.value = null; trendData.value = []; modelStats.value = []; recentUsage.value = []; platformQuotas.value = null; loading.value = false; loadingUsage.value = false; loadingCharts.value = false }, { flush: 'sync' })
  return { stats, loading, loadingUsage, loadingCharts, trendData, modelStats, recentUsage, platformQuotas, startDate, endDate, granularity, loadStats, loadCharts, loadRecent, loadPlatformQuotas }
}
