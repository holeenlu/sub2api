<script setup lang="ts">
import { ref, reactive, computed, onMounted, onScopeDispose, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { requestTypeToLegacyStream } from '@/features/usage/model/usageRequestType'
import Pagination from '@/shared/ui/Pagination.vue'
import Select from '@/shared/ui/Select.vue'
import DateRangePicker from '@/shared/ui/DateRangePicker.vue'
import ModelDistributionChart from '../components/ModelDistributionChart.vue'
import GroupDistributionChart from '../components/GroupDistributionChart.vue'
import TokenUsageTrend from '@/features/usage/components/TokenUsageTrend.vue'
import EndpointDistributionChart from '../components/EndpointDistributionChart.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import type { AdminUser } from '@/features/identity/api/profileTypes';
import { adminUsageAPI, type AdminUsageQueryParams } from '../api/adminUsage'
import { useUsageServices } from '../services'
import { useUsageRequest } from '../queries/useUsageRequest'
import { useUsageList } from '../queries/useUsageList'
import { useUsageStatistics } from '../queries/useUsageStatistics'
import { useUsageExport } from '../queries/useUsageExport'
import { useAdminUsageErrors } from '../queries/useAdminUsageErrors'
import { useAdminUsageColumns } from '../model/useAdminUsageColumns'
import { getLast24HoursRangeDates, getGranularityForRange } from '../model/usageDates'
import UsageStatsCards from '../components/UsageStatsCards.vue'
import UsageFilters from '../components/UsageFilters.vue'
import UsageTable from '../components/UsageTable.vue'
import UsageExportProgress from '../components/UsageExportProgress.vue'
import UsageCleanupDialog from '../components/UsageCleanupDialog.vue'
import UserTokenRanking from '../components/UserTokenRanking.vue'
import APIKeyTokenRanking from '../components/APIKeyTokenRanking.vue'
const { t } = useI18n()
const route = useRoute()
const services = useUsageServices()
const permissions = services.permissions
const UserBalanceHistoryModal = services.panels.balanceHistory
const OpsErrorLogTable = services.panels.adminErrorsTable
const OpsErrorDetailModal = services.panels.adminErrorDetail
const balanceRequest = useUsageRequest(services.scopeKey)
const labelRequest = useUsageRequest(services.scopeKey)
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
const granularity = ref<'day' | 'hour'>('hour')
const modelDistributionMetric = ref<DistributionMetric>('tokens')
const modelDistributionSource = ref<ModelDistributionSource>('requested')
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const cleanupDialogVisible = ref(false)
const showBalanceHistoryModal = ref(false)
const balanceHistoryUser = ref<AdminUser | null>(null)
let chartLoadTimer: ReturnType<typeof setTimeout> | undefined
const granularityOptions = computed(() => [{ value: 'day', label: t('admin.dashboard.day') }, { value: 'hour', label: t('admin.dashboard.hour') }])
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start); const endDate = ref(defaultRange.end)
const filters = ref<AdminUsageQueryParams>({ user_id: undefined, model: undefined, group_id: undefined, request_type: undefined, native_compaction_v2: null, billing_type: null, start_date: startDate.value, end_date: endDate.value })
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const getSingleQueryValue = (value: string | null | Array<string | null> | undefined): string | undefined => {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

const getNumericQueryValue = (value: string | null | Array<string | null> | undefined): number | undefined => {
  const raw = getSingleQueryValue(value)
  if (!raw) return undefined
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

const applyRouteQueryFilters = () => {
  const queryStartDate = getSingleQueryValue(route.query.start_date)
  const queryEndDate = getSingleQueryValue(route.query.end_date)
  const queryUserId = getNumericQueryValue(route.query.user_id)

  if (queryStartDate) {
    startDate.value = queryStartDate
  }
  if (queryEndDate) {
    endDate.value = queryEndDate
  }

  filters.value = {
    ...filters.value,
    user_id: queryUserId,
    start_date: startDate.value,
    end_date: endDate.value
  }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
}

const loadRouteUserFilterLabel = async () => {
  const requestedUserId = filters.value.user_id
  if (!requestedUserId) return
  const userSearchRevision = usageFiltersRef.value?.getUserSearchRevision?.()

  const routeUserFilterIsCurrent = () => (
    filters.value.user_id === requestedUserId
    && usageFiltersRef.value?.getUserSearchRevision?.() === userSearchRevision
  )

  const ticket = labelRequest.begin()
  if (!ticket.isCurrent()) return
  try {
    const user = await services.getAdminUser(requestedUserId, ticket)
    if (!ticket.isCurrent()) return
    if (!ticket.isCurrent() || !routeUserFilterIsCurrent()) return
    usageFiltersRef.value?.setUserKeyword?.(user.email || String(requestedUserId))
  } catch {
    if (!ticket.isCurrent() || !routeUserFilterIsCurrent()) return
    usageFiltersRef.value?.setUserKeyword?.(String(requestedUserId))
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value = {
    ...filters.value,
    start_date: range.startDate,
    end_date: range.endDate
  }
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const buildUsageListParams = (
  page: number,
  pageSize: number,
  exactTotal: boolean
): AdminUsageQueryParams => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    page,
    page_size: pageSize,
    exact_total: exactTotal,
    ...filters.value,
    account_id: permissions.value.isSuperAdmin ? filters.value.account_id : undefined,
    stream: legacyStream === null ? undefined : legacyStream,
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}


const { rows: usageLogs, loading, pagination, totalExact: usageTotalExact, load: loadLogs } = useUsageList({
  scopeKey: services.scopeKey, pageSize: getPersistedPageSize(), filters: () => buildUsageListParams(1, 100, false),
  fetch: adminUsageAPI.list, onError: () => services.notifyError(t('usage.failedToLoad'))
})
const { usageStats, requestedModelStats, upstreamModelStats, mappingModelStats, trendData, groupStats,
  inboundEndpointStats, upstreamEndpointStats, endpointPathStats, endpointStatsLoading, modelStatsLoading, chartsLoading,
  loadStats, loadModelStats, loadChartData, invalidateModelStatsCache } = useUsageStatistics({ role: 'admin',
  filters: () => buildUsageListParams(1, 100, false), granularity: () => granularity.value })
const { exporting, exportProgress, run: exportToExcel, cancel: cancelExport } = useUsageExport({ role: 'admin',
  params: () => buildUsageListParams(1, 100, true), total: () => pagination.total })
const { errRows, errLoading, errPage, errPageSize, errTotal, showErrorModal, selectedErrorId,
  loadAdminErrors, onErrSort, onErrPage, onErrPageSize, openError } = useAdminUsageErrors({ filters: () => filters.value })
const openCleanupDialog = () => { cleanupDialogVisible.value = true }
// Detail tabs
type DetailTab = 'usage' | 'errors' | 'ranking' | 'keyRanking'
const activeTab = ref<DetailTab>('usage')
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  { key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const },
  { key: 'ranking' as const, label: t('usage.tabs.ranking'), icon: 'chart' as const },
  { key: 'keyRanking' as const, label: t('usage.tabs.keyRanking'), icon: 'key' as const },
])
// 两个排行 tab 对筛选栏的要求一致，都走 ranking 模式
const filtersMode = computed(() => (activeTab.value === 'keyRanking' ? 'ranking' : activeTab.value))
const usageFiltersRef = ref<InstanceType<typeof UsageFilters> | null>(null)
const rankingMounted = ref(false)
const rankingRef = ref<InstanceType<typeof UserTokenRanking> | null>(null)
const keyRankingMounted = ref(false)
const keyRankingRef = ref<InstanceType<typeof APIKeyTokenRanking> | null>(null)

const switchTab = (tab: DetailTab) => {
  activeTab.value = tab
  if (tab === 'errors' && errRows.value.length === 0) loadAdminErrors()
  if (tab === 'ranking') rankingMounted.value = true
  if (tab === 'keyRanking') keyRankingMounted.value = true
}

const breakdownFilters = computed(() => {
  const f: Record<string, unknown> = {}
  if (filters.value.user_id) f.user_id = filters.value.user_id
  if (filters.value.api_key_id) f.api_key_id = filters.value.api_key_id
  if (permissions.value.isSuperAdmin && filters.value.account_id) f.account_id = filters.value.account_id
  if (filters.value.group_id) f.group_id = filters.value.group_id
  if (filters.value.request_type != null) f.request_type = filters.value.request_type
  if (filters.value.native_compaction_v2 != null) f.native_compaction_v2 = filters.value.native_compaction_v2
  if (filters.value.billing_type != null) f.billing_type = filters.value.billing_type
  return f
})

const modelNameOptions = computed(() =>
  Array.from(new Set(requestedModelStats.value.map((m) => m.model).filter(Boolean))).sort()
)

const handleUserClick = async (userId: number) => {
  const ticket = balanceRequest.begin()
  if (!ticket.isCurrent()) return
  try {
    const user = await services.getAdminUser(userId, ticket)
    if (!ticket.isCurrent()) return
    balanceHistoryUser.value = user
    showBalanceHistoryModal.value = true
  } catch {
    if (!ticket.isCurrent()) return
    services.notifyError(t('admin.usage.failedToLoadUser'))
  }
}

// Drill down from the per-user token ranking: scope the whole usage view to
// that user and jump to the usage-detail tab so the drill-down is visible.
const handleRankingSelectUser = (userId: number, email: string) => {
  filters.value = { ...filters.value, user_id: userId }
  usageFiltersRef.value?.setUserKeyword?.(email || '')
  activeTab.value = 'usage'
  applyFilters()
}

// 同上，API Key 维度：把整个用量页收敛到这把 Key 并跳到用量明细。
const handleRankingSelectApiKey = (apiKeyId: number, keyName: string) => {
  filters.value = { ...filters.value, api_key_id: apiKeyId }
  usageFiltersRef.value?.setApiKeyKeyword?.(keyName || '')
  activeTab.value = 'usage'
  applyFilters()
}


const { visibleColumns, errVisibleColumnKeys, keyRankingVisibleColumnKeys, hasColumnSettings,
  currentToggleableColumns, isCurrentColumnVisible, toggleCurrentColumn, showColumnDropdown, columnDropdownRef } = useAdminUsageColumns(activeTab, permissions)
const applyFilters = () => {
  pagination.page = 1
  invalidateModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  errPage.value = 1
  if (activeTab.value === 'errors') {
    loadAdminErrors()
  } else {
    errRows.value = []
  }
}
const refreshData = () => {
  invalidateModelStatsCache()
  loadLogs()
  loadStats(true)
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeTab.value === 'errors') loadAdminErrors()
  // 两个排行都已挂载时各自 reload；隐藏中的那个由 active 守门，只记过期不发请求
  if (rankingMounted.value) rankingRef.value?.reload()
  if (keyRankingMounted.value) keyRankingRef.value?.reload()
}
const resetFilters = () => {
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = { start_date: startDate.value, end_date: endDate.value, request_type: undefined, native_compaction_v2: null, billing_type: null, billing_mode: undefined }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
  applyFilters()
}
const handlePageChange = (p: number) => { pagination.page = p; loadLogs() }
const handlePageSizeChange = (s: number) => { pagination.page_size = s; pagination.page = 1; loadLogs() }
const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadLogs()
}

const handleIpGeoBatchFailed = () => {
  services.notifyError(t('usage.ipGeo.batchFailed'))
}

onMounted(() => {
  applyRouteQueryFilters(); void loadRouteUserFilterLabel(); void loadLogs(); void loadStats(); void loadModelStats(modelDistributionSource.value, true)
  chartLoadTimer = setTimeout(() => { void loadChartData() }, 120)
})
onScopeDispose(() => clearTimeout(chartLoadTimer))
watch(modelDistributionSource, source => { void loadModelStats(source) })
watch(services.scopeKey, () => {
  clearTimeout(chartLoadTimer); showBalanceHistoryModal.value = false; balanceHistoryUser.value = null; cleanupDialogVisible.value = false
  if (services.scopeKey.value !== null) { void loadRouteUserFilterLabel(); void loadStats(); void loadModelStats(modelDistributionSource.value, true); void loadChartData() }
}, { flush: 'sync' })
defineExpose({ requestedModelStats, refreshData })
</script>
<template>
    <div class="space-y-6">
      <UsageStatsCards :show-account-cost="permissions.canReadBillingCost" :stats="usageStats" />
      <!-- Charts Section -->
      <div class="space-y-4">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:source="modelDistributionSource"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :upstream-model-stats="upstreamModelStats"
            :mapping-model-stats="mappingModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <!-- 明细区：tab 栏 + 筛选 + 内容收进同一张卡片，消除割裂感 -->
      <div class="card">
        <div class="flex flex-wrap items-center border-b border-gray-200 px-2 dark:border-dark-700 sm:px-4">
          <button
            v-for="tab in detailTabs"
            :key="tab.key"
            type="button"
            data-testid="usage-detail-tab"
            class="-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-3 text-sm font-medium transition-colors sm:px-4"
            :class="activeTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
            @click="switchTab(tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>

        <UsageFilters v-model="filters" ref="usageFiltersRef" flat :mode="filtersMode" class="border-b border-gray-100 dark:border-dark-700/50" :start-date="startDate" :end-date="endDate" :exporting="exporting" :model-options="modelNameOptions" @change="applyFilters" @refresh="refreshData" @reset="resetFilters" @cleanup="openCleanupDialog" @export="exportToExcel">
          <template #after-reset>
            <div v-if="hasColumnSettings" class="relative" ref="columnDropdownRef">
              <button
                data-testid="usage-column-settings"
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                </svg>
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in currentToggleableColumns"
                  :key="col.key"
                  :data-testid="`usage-column-toggle-${col.key}`"
                  @click="toggleCurrentColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isCurrentColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
          </template>
        </UsageFilters>

        <div v-show="activeTab === 'usage'" class="overflow-hidden rounded-b-2xl">
          <UsageTable :show-account-billing="permissions.canReadBillingCost"
            flat
            :data="usageLogs"
            :loading="loading"
            :columns="visibleColumns"
            :server-side-sort="true"
            :default-sort-key="'created_at'"
            :default-sort-order="'desc'"
            @sort="handleSort"
            @userClick="handleUserClick"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
          <Pagination :total-exact="usageTotalExact" :show-jump="false" v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="handlePageChange" @update:pageSize="handlePageSizeChange" />
        </div>
        <div v-show="activeTab === 'errors'" class="overflow-hidden rounded-b-2xl">
          <OpsErrorLogTable
            flat
            :rows="errRows" :total="errTotal" :loading="errLoading"
            :page="errPage" :page-size="errPageSize"
            :visible-column-keys="errVisibleColumnKeys"
            user-clickable
            @userClick="handleUserClick"
            @openErrorDetail="openError"
            @sort="onErrSort"
            @update:page="onErrPage"
            @update:pageSize="onErrPageSize"
            @ipGeoBatchFailed="handleIpGeoBatchFailed" />
        </div>
        <!-- 懒挂载：首次切到该 tab 才请求排行数据，之后随筛选自动刷新；
             active 让隐藏中的排行不发请求，切回时补一次 -->
        <div v-if="rankingMounted" v-show="activeTab === 'ranking'" class="overflow-hidden rounded-b-2xl">
          <UserTokenRanking
            ref="rankingRef"
            :active="activeTab === 'ranking'"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :model="filters.model"
            @select-user="handleRankingSelectUser"
          />
        </div>
        <!-- 同上，API Key 维度 -->
        <div v-if="keyRankingMounted" v-show="activeTab === 'keyRanking'" class="overflow-hidden rounded-b-2xl">
          <APIKeyTokenRanking
            ref="keyRankingRef"
            :active="activeTab === 'keyRanking'"
            :visible-column-keys="keyRankingVisibleColumnKeys"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :model="filters.model"
            @select-api-key="handleRankingSelectApiKey"
          />
        </div>
      </div>
      <OpsErrorDetailModal v-model:show="showErrorModal" :error-id="selectedErrorId" :error-type="'request'" />
    </div>
  <UsageExportProgress :show="exportProgress.show" :progress="exportProgress.progress" :current="exportProgress.current" :total="exportProgress.total" :estimated-time="exportProgress.estimatedTime" @cancel="cancelExport" />
  <UsageCleanupDialog
    :show="cleanupDialogVisible"
    :filters="filters"
    :start-date="startDate"
    :end-date="endDate"
    @close="cleanupDialogVisible = false"
  />
  <!-- Balance history modal triggered from usage table user click -->
  <UserBalanceHistoryModal
    :show="showBalanceHistoryModal"
    :user="balanceHistoryUser"
    :hide-actions="true"
    @close="showBalanceHistoryModal = false; balanceHistoryUser = null"
  />
</template>

