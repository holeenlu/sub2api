<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/shared/ui/Pagination.vue'
import Select, { type SelectOption } from '@/shared/ui/Select.vue'
import DateRangePicker from '@/shared/ui/DateRangePicker.vue'
import ModelDistributionChart from '../components/ModelDistributionChart.vue'
import GroupDistributionChart from '../components/GroupDistributionChart.vue'
import EndpointDistributionChart from '../components/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/features/usage/components/TokenUsageTrend.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import UserErrorRequestsTable from '../components/UserErrorRequestsTable.vue'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { requestTypeToLegacyStream } from '@/features/usage/model/usageRequestType'
import type { ApiKey } from '@/features/keys/api/types';
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { UsageQueryParams, ModelStat } from '../api/types'
import { usageAPI } from '../api/usage'
import { useUsageServices } from '../services'
import { useUsageRequest } from '../queries/useUsageRequest'
import { useUsageList } from '../queries/useUsageList'
import { useUsageStatistics } from '../queries/useUsageStatistics'
import { useUsageExport } from '../queries/useUsageExport'
import { useUserUsageErrors } from '../queries/useUserUsageErrors'
import { useUserUsageColumns } from '../model/useUserUsageColumns'
import { getLast24HoursRangeDates, getGranularityForRange } from '../model/usageDates'
import UsageStatsCards from '../components/UsageStatsCards.vue'
import UsageTable from '../components/UsageTable.vue'
import APIKeyTokenRanking from '../components/APIKeyTokenRanking.vue'
const { t } = useI18n()
const services = useUsageServices()
const filterRequest = useUsageRequest(services.scopeKey)
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
const granularity = ref<'day' | 'hour'>(getGranularityForRange(startDate.value, endDate.value))

const modelDistributionMetric = ref<DistributionMetric>('tokens')
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const activeTab = ref<'usage' | 'keyRanking' | 'errors'>('usage')
const errorViewEnabled = computed(() => services.publicSettings.value?.allow_user_view_error_requests ?? false)
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  { key: 'keyRanking' as const, label: t('usage.tabs.keyRanking'), icon: 'key' as const },
  ...(errorViewEnabled.value
    ? [{ key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const }]
    : []),
])

const filters = ref<UsageQueryParams>({
  start_date: startDate.value,
  end_date: endDate.value,
  request_type: undefined,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
})

const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

const granularityOptions = computed<SelectOption[]>(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') },
])
const requestTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
])
const compactionOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allCompactionTypes') },
  { value: true, label: t('usage.compactionOnly') },
])
// 订阅功能关闭后只剩余额计费，「计费类型」筛选（余额/订阅）失去意义，整块隐藏。
const subscriptionFeatureEnabled = services.subscriptionEnabled
const billingTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') },
])
const billingModeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') },
])

const apiKeys = ref<Array<Pick<ApiKey, 'id' | 'name'>>>([])
const groups = ref<Group[]>([])
const modelOptionValues = ref<string[]>([])

const apiKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allApiKeys') },
  ...apiKeys.value.map((key) => ({ value: key.id, label: key.name })),
])
const groupOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allGroups') },
  ...groups.value.map((group) => ({ value: group.id, label: group.name })),
])
const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...modelOptionValues.value.map((model) => ({ value: model, label: model })),
])

const normalizedFilters = computed<UsageQueryParams>(() => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    ...filters.value,
    start_date: startDate.value,
    end_date: endDate.value,
    stream: legacyStream === null ? undefined : legacyStream,
  }
})

const buildUsageListParams = (page: number, pageSize: number): UsageQueryParams => ({
  page,
  page_size: pageSize,
  ...normalizedFilters.value,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order,
})

const rankingFilters = computed<Record<string, unknown>>(() => ({
  api_key_id: normalizedFilters.value.api_key_id,
  group_id: normalizedFilters.value.group_id,
  model: normalizedFilters.value.model,
  request_type: normalizedFilters.value.request_type,
  stream: normalizedFilters.value.stream,
  native_compaction_v2: normalizedFilters.value.native_compaction_v2,
  billing_type: normalizedFilters.value.billing_type,
  billing_mode: normalizedFilters.value.billing_mode,
}))

const refreshModelOptions = (models: ModelStat[]) => {
  const current = filters.value.model
  const set = new Set(modelOptionValues.value)
  models.forEach((item) => {
    if (item.model) set.add(item.model)
  })
  if (current) set.add(current)
  modelOptionValues.value = Array.from(set).sort()
}


const { rows: usageLogs, loading, pagination, totalExact: usageTotalExact, load: loadLogs } = useUsageList({
  scopeKey: services.scopeKey, pageSize: getPersistedPageSize(), filters: () => ({ ...normalizedFilters.value, ...sortState }),
  fetch: usageAPI.query, onError: () => services.notifyError(t('usage.failedToLoad'))
})
const { usageStats, requestedModelStats, trendData, groupStats, inboundEndpointStats, upstreamEndpointStats,
  endpointPathStats, endpointStatsLoading, modelStatsLoading, chartsLoading, loadStats, loadModelStats, loadChartData } = useUsageStatistics({
  role: 'user', filters: () => normalizedFilters.value, granularity: () => granularity.value, onModels: refreshModelOptions
})
const { exporting, run: exportToCSV } = useUsageExport({ role: 'user', params: () => buildUsageListParams(1, 100), total: () => pagination.total })
const errors = useUserUsageErrors({ apiKeys: () => apiKeys.value, range: () => ({ start: startDate.value, end: endDate.value }) })
const { errorRows, errorLoading, errorPage, errorPageSize, errorTotal, errorFilter, errorKeyOptions, errorModelOptions,
  errorCategoryOptions, errorStatusOptions, loadErrors, applyErrorFilters, onErrorSort, onErrorPage, onErrorPageSize } = errors
const { visibleColumns, errVisibleColumnKeys, currentToggleableColumns, isCurrentColumnVisible,
  toggleCurrentColumn, showColumnDropdown, columnDropdownRef } = useUserUsageColumns(activeTab)
const fetchUserAPIKeyBreakdown = (params: import('@/features/usage/adminDashboard').UserBreakdownParams, options?: { signal?: AbortSignal }) => usageAPI.getDashboardAPIKeyBreakdown(params, options)
const keyRankingRef = ref<{ reload: () => Promise<void> } | null>(null)
function handleKeyRankingSelect(apiKeyId: number): void { filters.value.api_key_id = apiKeyId; activeTab.value = 'usage'; applyFilters() }
function resetErrorRows(): void { errorPage.value = 1; if (activeTab.value === 'errors') void loadErrors(); else errors.clear() }
function switchToErrors(): void { activeTab.value = 'errors'; if (!errorRows.value.length) void loadErrors() }
async function loadFilterOptions(): Promise<void> {
  const ticket = filterRequest.begin()
  if (!ticket.isCurrent()) return
  try {
    const [first, availableGroups] = await Promise.all([services.listUserKeys(1, 100, ticket), services.getUserGroups(ticket)])
    if (!ticket.isCurrent()) return
    const keys = [...first.items]
    for (let page = 2; page <= first.pages && keys.length > 0; page++) {
      if (!ticket.isCurrent()) return
      const response = await services.listUserKeys(page, 100, ticket)
      if (!ticket.isCurrent()) return
      if (!response.items.length) break
      keys.push(...response.items)
    }
    apiKeys.value = keys; groups.value = availableGroups
  } catch { /* Optional filters may be retried via a fresh scope; no permission fallback. */ }
}
const applyFilters = () => {
  pagination.page = 1
  void loadLogs()
  void loadStats()
  void loadModelStats('requested', true)
  void loadChartData()
  resetErrorRows()
}

const refreshData = () => {
  if (activeTab.value === 'keyRanking') {
    void keyRankingRef.value?.reload()
    return
  }
  void loadLogs()
  void loadStats()
  void loadModelStats('requested', true)
  void loadChartData()
  if (activeTab.value === 'errors') void loadErrors()
}

const resetFilters = () => {
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = {
    start_date: range.start,
    end_date: range.end,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    billing_mode: null,
  }
  granularity.value = getGranularityForRange(range.start, range.end)
  applyFilters()
  if (activeTab.value === 'errors') {
    errorFilter.value = { model: '', category: '', api_key_id: null, status_code: null }
    applyErrorFilters()
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value.start_date = range.startDate
  filters.value.end_date = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  void loadLogs()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadLogs()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  void loadLogs()
}

const handleIpGeoBatchFailed = () => {
  services.notifyError(t('usage.ipGeo.batchFailed'))
}


onMounted(() => { void loadFilterOptions(); refreshData() })
watch(services.scopeKey, () => {
  apiKeys.value = []; groups.value = []; modelOptionValues.value = []
  if (services.scopeKey.value !== null) { void loadFilterOptions(); void loadStats(); void loadModelStats('requested', true); void loadChartData() }
}, { flush: 'sync' })
</script>
<template>
    <div class="space-y-6">
      <UsageStatsCards :stats="usageStats" :show-account-cost="false" :strike-standard-cost="true" />

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
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
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
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>

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
            @click="tab.key === 'errors' ? switchToErrors() : activeTab = tab.key"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>

        <div class="flex flex-wrap items-end justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700/50">
          <div v-if="activeTab === 'errors'" class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.keyName') }}</label>
              <Select v-model="errorFilter.api_key_id" :options="errorKeyOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.model') }}</label>
              <Select
                v-model="errorFilter.model"
                :options="errorModelOptions"
                searchable
                creatable
                clearable
                :placeholder="t('usage.errors.modelPlaceholder')"
                @change="applyErrorFilters"
              />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('usage.errors.category') }}</label>
              <Select v-model="errorFilter.category" :options="errorCategoryOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.errors.status') }}</label>
              <Select v-model="errorFilter.status_code" :options="errorStatusOptions" @change="applyErrorFilters" />
            </div>
          </div>
          <div v-else class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
              <Select v-model="filters.api_key_id" :options="apiKeyOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.model') }}</label>
              <Select v-model="filters.model" :options="modelOptions" searchable @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.group') }}</label>
              <Select v-model="filters.group_id" :options="groupOptions" searchable @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.type') }}</label>
              <Select v-model="filters.request_type" :options="requestTypeOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.compactionFilter') }}</label>
              <Select v-model="filters.native_compaction_v2" :options="compactionOptions" @change="applyFilters" />
            </div>
            <div v-if="subscriptionFeatureEnabled" class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingType') }}</label>
              <Select v-model="filters.billing_type" :options="billingTypeOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
              <Select v-model="filters.billing_mode" :options="billingModeOptions" @change="applyFilters" />
            </div>
          </div>

          <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
            <button type="button" @click="refreshData" :disabled="activeTab === 'errors' ? errorLoading : loading" class="btn btn-secondary">
              {{ t('common.refresh') }}
            </button>
            <button type="button" @click="resetFilters" class="btn btn-secondary">
              {{ t('common.reset') }}
            </button>
            <div class="relative" ref="columnDropdownRef">
              <button
                v-if="activeTab !== 'keyRanking'"
                type="button"
                data-testid="usage-column-settings"
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <Icon name="grid" size="sm" />
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in currentToggleableColumns"
                  :key="col.key"
                  type="button"
                  :data-testid="`usage-column-toggle-${col.key}`"
                  @click="toggleCurrentColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon v-if="isCurrentColumnVisible(col.key)" name="check" size="sm" class="text-primary-500" />
                </button>
              </div>
            </div>
            <button v-if="activeTab === 'usage'" type="button" @click="exportToCSV" :disabled="exporting" class="btn btn-primary">
              {{ exporting ? t('usage.exporting') : t('usage.exportCsv') }}
            </button>
          </div>
        </div>

      <template v-if="activeTab === 'usage'">
        <UsageTable
          :data="usageLogs"
          :loading="loading"
          :columns="visibleColumns"
          :server-side-sort="true"
          :show-account-billing="false"
          :show-upstream-endpoint="false"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
          @ipGeoBatchFailed="handleIpGeoBatchFailed"
        />

        <Pagination :total-exact="usageTotalExact" :show-jump="false"
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>

      <APIKeyTokenRanking
        ref="keyRankingRef"
        v-else-if="activeTab === 'keyRanking'"
        :show-user="false"
        :start-date="startDate"
        :end-date="endDate"
        :filters="rankingFilters"
        :fetch="fetchUserAPIKeyBreakdown"
        :active="activeTab === 'keyRanking'"
        @select-api-key="handleKeyRankingSelect"
      />

      <UserErrorRequestsTable
        v-else-if="errorViewEnabled"
        :rows="errorRows"
        :total="errorTotal"
        :loading="errorLoading"
        :page="errorPage"
        :page-size="errorPageSize"
        :visible-column-keys="errVisibleColumnKeys"
        @sort="onErrorSort"
        @update:page="onErrorPage"
        @update:pageSize="onErrorPageSize"
        @ipGeoBatchFailed="handleIpGeoBatchFailed"
      />
      </div>
    </div>

</template>

