<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { formatDateTime } from '@/utils/format'
import { vendorAPI } from '../api'
import {
  dateRange,
  formatUsageNumber,
  formatUsageCost,
  download,
  errorText,
  statementPeriod,
  usageAuthorizationModes
} from '../helpers'
import type { Query, UsageResponse, VendorGroup } from '../types'
import RequestState from './RequestState.vue'
import UsageMetrics from './UsageMetrics.vue'
import UsageTable from './UsageTable.vue'
import UsageTrend from './UsageTrend.vue'
import UsageAccountPicker from './UsageAccountPicker.vue'
const props = withDefaults(
  defineProps<{
    admin?: boolean
    readOnly?: boolean
    vendorId?: number
    accountId?: number
    groups?: VendorGroup[]
  }>(),
  { admin: false }
)
const emit = defineEmits<{ draft: [filters: Query] }>()
const { t } = useI18n()
const dimensions = ['vendor', 'account', 'date', 'group', 'account_date', 'request']
const dimensionLabel = (value: string) =>
  value === 'request'
    ? t('vendor.usageRecords')
    : t('vendor.usageBy', { dimension: t('vendor.' + value) })
const platformOptions = computed(() => [
  { value: '', label: t('vendor.allPlatforms') },
  ...['anthropic', 'openai'].map((value) => ({
    value,
    label: platformDisplayName(value)
  }))
])
const groupOptions = computed(() => [
  { value: '', label: t('vendor.all') },
  ...(props.groups || []).map((group) => ({
    value: String(group.id),
    label: group.name
  }))
])
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const filters = reactive({
  ...(props.accountId ? { start_date: '', end_date: '' } : dateRange(7)),
  platform: '',
  authorization_mode: '',
  group_id: '',
  account_id: '',
  timezone
})
const applied = ref<Query>({}),
  dimension = ref('vendor'),
  dayPage = ref(1),
  accountPage = ref(1),
  modelPage = ref(1),
  daySize = ref(20),
  accountSize = ref(20)
const summary = shallowRef<UsageResponse>(),
  days = shallowRef<UsageResponse>(),
  accounts = shallowRef<UsageResponse>()
const sorting = reactive({
  days: {
    key: props.admin && !props.accountId ? 'cost_usd' : 'date',
    order: props.admin && !props.accountId ? 'desc' : 'asc'
  } as { key: string; order: 'asc' | 'desc' },
  accounts: { key: 'cost_usd', order: 'desc' } as {
    key: string
    order: 'asc' | 'desc'
  }
})
const sortQuery = (kind: 'days' | 'accounts') => ({
  sort_by: sorting[kind].key,
  order: sorting[kind].order
})
const fixedWindows = computed(() =>
  ['5h', '7d', '30d'].filter((window) => summary.value?.fixed_windows?.[window])
)
const tokenParts = computed(() => ([
  ['input_tokens', 'inputTokens', 'bg-primary-500'],
  ['output_tokens', 'outputTokens', 'bg-blue-500'],
  ['cache_creation_tokens', 'cacheWrite', 'bg-amber-500'],
  ['cache_read_tokens', 'cacheRead', 'bg-violet-500']
] as const).map(([key, label, color]) => ({ key, label, color, value: summary.value?.summary?.[key] })))
const tokenTotal = computed(() => tokenParts.value.every(part => part.value != null)
  ? tokenParts.value.reduce((total, part) => total + part.value!, 0) : null)
const modelMaximum = computed(() => Math.max(0, ...(summary.value?.models || []).map(row => row.requests || 0)))
const hasAccountFilters = computed(() => Boolean(filters.start_date || filters.end_date || filters.platform || filters.authorization_mode || filters.group_id || filters.timezone !== timezone))
const datePickerContainer = ref<HTMLDivElement>()
const loading = ref(false),
  error = ref(''),
  exporting = ref(false),
  detailId = ref<number>(),
  detailVendorId = ref<number>(),
  trendMetric = ref<'requests' | 'total_tokens' | 'cost_usd'>('requests')
let controller: AbortController | undefined,
  epoch = 0
const scope = computed(() => ({
  ...((props.admin || props.readOnly) && props.vendorId ? { vendor_id: props.vendorId } : {}),
  ...(props.accountId ? { account_id: props.accountId } : {})
}))
const stable = computed(() => ({
  ...applied.value,
  ...scope.value,
  ...(summary.value
    ? {
        snapshot_at: summary.value.snapshot_at,
        watermark: summary.value.watermark
      }
    : {})
}))
const pendingFilters = computed(() =>
  Object.entries(filters).some(([key, value]) => value !== applied.value[key])
)
function openAccount(id: number, vendorId?: number) {
  detailVendorId.value = vendorId ?? props.vendorId
  if (props.admin && !detailVendorId.value) {
    error.value = t('vendor.selectVendor')
    return
  }
  detailId.value = id
}
async function fetchSummary(q: Query, signal: AbortSignal) {
  if (props.accountId)
    return vendorAPI.accountUsage(
      props.accountId,
      {
        ...q,
        ...sortQuery('days'),
        page: dayPage.value,
        page_size: daySize.value,
        model_page: modelPage.value
      },
      signal,
      { admin: props.admin, vendorId: props.vendorId, readOnly: props.readOnly }
    )
  if (props.admin)
    return vendorAPI.adminUsage(
      {
        ...q,
        ...sortQuery('days'),
        dimension: dimension.value,
        page: dayPage.value,
        page_size: daySize.value
      },
      signal
    )
  return vendorAPI.usage(
    'trend',
    {
      ...q,
      ...sortQuery('days'),
      page: dayPage.value,
      page_size: daySize.value
    },
    signal
  )
}
async function load(reset = true) {
  if (reset && filters.start_date && filters.end_date && filters.start_date > filters.end_date) {
    error.value = t('vendor.invalidDates')
    return
  }
  controller?.abort()
  controller = new AbortController()
  const signal = controller.signal,
    current = ++epoch
  if (reset) {
    applied.value = { ...filters }
    dayPage.value = 1
    accountPage.value = 1
    modelPage.value = 1
    summary.value = undefined
    days.value = undefined
    accounts.value = undefined
  }
  loading.value = true
  error.value = ''
  try {
    const q = reset ? { ...applied.value, ...scope.value } : stable.value
    const first = await fetchSummary(q, signal)
    if (current !== epoch) return
    summary.value = first
    days.value = first
    if (!props.admin && !props.accountId) {
      const shared = {
        ...q,
        snapshot_at: first.snapshot_at,
        watermark: first.watermark
      }
      const accountResult = await vendorAPI.usage(
        'accounts',
        {
          ...shared,
          ...sortQuery('accounts'),
          page: accountPage.value,
          page_size: accountSize.value
        },
        signal
      )
      if (current !== epoch) return
      accounts.value = accountResult
    }
  } catch (e) {
    if (current === epoch && !signal.aborted) error.value = errorText(e)
  } finally {
    if (current === epoch) loading.value = false
  }
}
async function page(kind: 'days' | 'accounts', value: number, size?: number) {
  if (kind === 'days') {
    dayPage.value = value
    if (size) daySize.value = Math.min(size, 100)
  } else {
    accountPage.value = value
    if (size) accountSize.value = Math.min(size, 100)
  }
  if (props.admin || props.accountId) {
    await load(false)
    return
  }
  controller?.abort()
  controller = new AbortController()
  const current = ++epoch
  error.value = ''
  loading.value = true
  try {
    const result = await vendorAPI.usage(
      kind === 'days' ? 'trend' : 'accounts',
      {
        ...stable.value,
        ...sortQuery(kind),
        page: value,
        page_size: kind === 'days' ? daySize.value : accountSize.value
      },
      controller?.signal
    )
    if (current === epoch) {
      if (kind === 'days') days.value = result
      else accounts.value = result
    }
  } catch (e) {
    if (current === epoch && !controller?.signal.aborted) error.value = errorText(e)
  } finally {
    if (current === epoch) loading.value = false
  }
}
async function sortRows(kind: 'days' | 'accounts', key: string, order: 'asc' | 'desc') {
  sorting[kind] = { key, order }
  await page(kind, 1)
}
async function pageModels(value: number) {
  modelPage.value = value
  await load(false)
}
function applyFilters() {
  const pending = load(true)
  if (props.accountId) void nextTick(() => datePickerContainer.value?.querySelector<HTMLButtonElement>('button')?.focus({ preventScroll: true }))
  return pending
}
function allHistory() {
  Object.assign(filters, { start_date: '', end_date: '' })
  void applyFilters()
}
function resetFilters() {
  Object.assign(filters, {
    ...(props.accountId ? { start_date: '', end_date: '' } : dateRange(7)),
    platform: '', authorization_mode: '', group_id: '', account_id: '', timezone
  })
  if (props.accountId) void applyFilters()
}
async function exportRows() {
  const current = epoch
  exporting.value = true
  error.value = ''
  try {
    const file = await vendorAPI.exportUsage({
      ...stable.value,
      dimension: dimension.value
    })
    if (current === epoch) download(file, 'supplier-usage.csv')
  } catch (e) {
    if (current === epoch) error.value = errorText(e)
  } finally {
    exporting.value = false
  }
}
watch(
  () => [props.admin, props.vendorId, props.accountId, props.readOnly],
  () => {
    // Clear the previous owner even when the draft dates fail validation.
    controller?.abort()
    epoch++
    summary.value = undefined
    days.value = undefined
    accounts.value = undefined
    applied.value = {}
    loading.value = false
    filters.account_id = ''
    filters.group_id = ''
    detailId.value = undefined
    detailVendorId.value = undefined
    load(true)
  }
)
watch(dimension, () => {
  dayPage.value = 1
  sorting.days =
    dimension.value === 'date'
      ? { key: 'date', order: 'asc' }
      : dimension.value === 'request'
        ? { key: 'created_at', order: 'desc' }
        : { key: 'cost_usd', order: 'desc' }
  load(!summary.value)
})
onMounted(() => load(true))
onUnmounted(() => {
  epoch++
  controller?.abort()
})
</script>
<template>
  <section class="space-y-4" :aria-busy="loading">
    <div v-if="!admin && !accountId" class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <h1 class="text-2xl font-semibold">{{ t('vendor.usage') }}</h1>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.usageDescription') }}</p>
    </div>
    <form
      class="vendor-filter-bar flex flex-wrap items-end gap-3"
      :class="accountId ? '' : 'card p-4'"
      @submit.prevent="applyFilters"
    >
      <p v-if="accountId" class="mr-auto max-w-md self-center text-sm text-gray-500 dark:text-gray-400">{{ t('vendor.accountUsageDescription') }}</p>
      <UsageAccountPicker
        v-if="!accountId"
        class="w-48"
        v-model="filters.account_id"
        :admin="admin"
        :vendor-id="vendorId"
      />

      <div ref="datePickerContainer" class="max-w-full">
        <p v-if="!accountId" class="mb-1 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.period') }}</p>
        <DateRangePicker
          v-model:start-date="filters.start_date"
          v-model:end-date="filters.end_date"
          @change="accountId && applyFilters()"
        />
      </div>
      <details :open="admin && !accountId" class="max-w-full" :class="{ 'account-usage-filters': accountId }">
        <summary
          class="cursor-pointer rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-500 dark:text-gray-400 dark:border-dark-600"
        >
          {{ t('vendor.advancedFilters') }}
        </summary>
        <div class="mt-3 flex flex-wrap items-end gap-3" :class="{ 'rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800': accountId }">
          <div class="w-36">
            <p class="mb-1 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.platform') }}</p>
            <Select
              v-model="filters.platform"
              :options="platformOptions"
              :aria-label="t('vendor.platform')"
              @update:model-value="() => { if (filters.platform === 'anthropic') filters.authorization_mode = '' }"
            />
          </div>

          <div v-if="filters.platform !== 'anthropic'" class="w-40">
            <p class="mb-1 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.authorization.executionMode') }}</p>
            <Select v-model="filters.authorization_mode" :options="[{ value: '', label: t('vendor.authorization.allModes') }, ...usageAuthorizationModes.map(value => ({ value, label: t('vendor.authorization.' + value) }))]" :aria-label="t('vendor.authorization.executionMode')" />
          </div>
          <div class="w-36">
            <p class="mb-1 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.group') }}</p>
            <Select
              v-model="filters.group_id"
              :options="groupOptions"
              :aria-label="t('vendor.group')"
            />
          </div>
          <label class="w-40 text-xs text-gray-500 dark:text-gray-400"
            >{{ t('vendor.timezone')
            }}<input v-model="filters.timezone" required class="input mt-1"
          /></label>
        </div>
      </details>
      <div v-if="!accountId || hasAccountFilters || pendingFilters || admin" class="flex flex-wrap items-end gap-2" :class="{ 'ml-auto': !accountId }">
        <button v-if="accountId && (filters.start_date || filters.end_date)" type="button" class="btn btn-secondary btn-sm" @click="allHistory">
          {{ t('vendor.allHistory') }}</button
        ><button
          type="button"
          class="btn btn-secondary btn-sm"
          v-if="!accountId || filters.platform || filters.authorization_mode || filters.group_id || filters.timezone !== timezone"
          @click="resetFilters"
        >
          {{ t('vendor.clear') }}</button
        ><button
          v-if="!accountId || pendingFilters"
          type="button"
          class="btn btn-secondary btn-sm"
          @click="Object.assign(filters, applied)"
        >
          {{ t('vendor.cancel') }}</button
        ><button v-if="!accountId || pendingFilters" class="btn btn-primary btn-sm" :disabled="loading">
          {{ t('vendor.apply') }}</button
        ><button
          v-if="admin"
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="exporting || loading || !summary"
          @click="exportRows"
        >
          {{ t('vendor.exportAll') }}</button
        ><button
          v-if="admin && !accountId"
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="!summary || loading"
          @click="emit('draft', { ...applied, ...scope })"
        >
          {{ t('vendor.newStatement') }}
        </button>
      </div>
    </form>
    <p v-if="loading && summary" role="status" class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('vendor.loading') }}
    </p>
    <RequestState :loading="loading && !summary" :error="error" @retry="load(true)">
      <template v-if="summary">
        <p v-if="pendingFilters" role="status" class="text-sm text-amber-700 dark:text-amber-400">
          {{ t('vendor.pendingFilters') }}
        </p>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('vendor.period') }}:
          {{ accountId && !applied.start_date && !applied.end_date ? t('vendor.allHistory') : statementPeriod(summary.start_at, summary.end_at, summary.timezone) }}
          · {{ summary.timezone }}
          <span v-if="applied.platform" class="ml-2 badge badge-gray">{{ platformDisplayName(String(applied.platform)) }}</span>
          <span v-if="applied.authorization_mode" class="ml-2 badge badge-gray">{{ t('vendor.authorization.' + applied.authorization_mode) }}</span>
          <span v-if="applied.group_id" class="ml-2 badge badge-gray">{{ groupOptions.find(group => group.value === String(applied.group_id))?.label || '#' + applied.group_id }}</span>
          <template v-if="!accountId"> · {{ t('vendor.costNotice') }} · {{ t('vendor.snapshot') }}: {{ formatDateTime(summary.snapshot_at) || t('vendor.unknown') }}</template>
        </p>
        <UsageMetrics :metrics="summary.summary" :compact="!accountId" />
      </template>
      <div
        v-if="admin && !accountId"
        role="group"
        :aria-label="t('vendor.dimension')"
        class="flex max-w-full overflow-x-auto border-b border-gray-200 dark:border-dark-700"
      >
        <button
          v-for="d in dimensions"
          :key="d"
          type="button"
          :aria-pressed="dimension === d"
          class="-mb-px inline-flex items-center gap-1.5 whitespace-nowrap border-b-2 px-4 py-3 text-sm font-medium transition-colors"
          :class="
            dimension === d
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 dark:text-gray-400 hover:border-gray-300 hover:text-gray-700 dark:hover:text-gray-200'
          "
          @click="dimension = d"
        >
          <Icon
            :name="
              d === 'request'
                ? 'document'
                : d === 'vendor'
                  ? 'users'
                  : d === 'date'
                    ? 'calendar'
                    : 'chartBar'
            "
            size="sm"
          />
          {{ dimensionLabel(d) }}
        </button>
      </div>
      <section v-if="fixedWindows.length" class="card overflow-hidden">
        <h3 class="border-b border-gray-100 px-4 py-3 text-sm font-semibold dark:border-dark-700">
          {{ t('vendor.fixedWindows') }}
        </h3>
        <div class="overflow-x-auto">
          <table class="table text-sm">
            <thead>
              <tr>
                <th>{{ t('vendor.period') }}</th>
                <th class="!text-right">{{ t('vendor.requests') }}</th>
                <th class="!text-right">{{ t('vendor.tokensWithCache') }}</th>
                <th class="!text-right">{{ t('vendor.cost') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="window in fixedWindows" :key="window">
                <td>{{ t('monitorCommon.quota.windows.' + window) }}</td>
                <td class="text-right tabular-nums">
                  {{ formatUsageNumber(summary?.fixed_windows?.[window]?.requests) }}
                </td>
                <td class="text-right tabular-nums">
                  {{
                    formatUsageNumber(
                      summary?.fixed_windows?.[window]?.total_tokens != null &&
                        summary?.fixed_windows?.[window]?.cache_tokens != null
                        ? summary.fixed_windows[window].total_tokens! +
                            summary.fixed_windows[window].cache_tokens!
                        : null
                    )
                  }}
                </td>
                <td class="text-right tabular-nums">
                  {{ formatUsageCost(summary?.fixed_windows?.[window]?.cost_usd) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <details v-if="admin && !accountId && summary?.recent_minute" class="card p-4">
        <summary class="cursor-pointer text-sm font-medium">{{ t('vendor.recentMinute') }}</summary>
        <UsageMetrics class="mt-3" :metrics="summary.recent_minute" />
      </details>
      <section v-if="summary && (accountId || (admin && summary.trends?.length))" data-usage-chart class="card p-4">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <h3 class="text-sm font-semibold">{{ t('vendor.trendChart') }}</h3>
          <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
            <button
              v-for="[value, label] in [
                ['requests', 'requests'],
                ['total_tokens', 'tokens'],
                ['cost_usd', 'cost']
              ] as const"
              :key="value"
              class="rounded-md px-3 py-1.5 text-xs"
              :class="
                trendMetric === value
                  ? 'bg-white font-medium shadow-sm dark:bg-dark-800'
                  : 'text-gray-500 dark:text-gray-400'
              "
              :aria-pressed="trendMetric === value"
              @click="trendMetric = value"
            >
              {{ t('vendor.' + label) }}
            </button>
          </div>
        </div>
        <UsageTrend
          v-if="summary.trends?.length"
          :rows="summary.trends"
          :metric="trendMetric"
          :label="
            t(
              'vendor.' +
                { requests: 'requests', total_tokens: 'tokens', cost_usd: 'cost' }[trendMetric]
            )
          "
        />
        <div v-else class="flex h-48 flex-col items-center justify-center gap-3 text-sm text-gray-500 dark:text-gray-400">
          <Icon name="chart" size="lg" class="text-gray-300 dark:text-dark-500" />
          <p>{{ t('vendor.noUsageInPeriod') }}</p>
        </div>
      </section>
      <div v-if="accountId && summary" class="grid min-w-0 gap-4 md:grid-cols-2">
        <section class="card min-w-0 overflow-hidden" data-usage-models>
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <h3 class="text-sm font-semibold">{{ t('vendor.models') }}</h3>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.requests') }}</span>
          </div>
          <div v-if="summary.models?.length" class="max-h-80 space-y-5 overflow-y-auto p-4">
            <div v-for="(row, index) in summary.models" :key="index" class="space-y-2">
              <div class="flex items-start justify-between gap-3 text-xs">
                <span class="min-w-0 break-words font-medium">{{ row.requested_model || row.upstream_model || row.model || t('vendor.unknown') }}</span>
                <span class="shrink-0 tabular-nums">{{ formatUsageNumber(row.requests) }}</span>
              </div>
              <div class="h-3 overflow-hidden rounded bg-gray-100 dark:bg-dark-700" aria-hidden="true">
                <div class="h-full rounded bg-primary-500" :style="{ width: (modelMaximum && row.requests != null ? Math.min(100, row.requests / modelMaximum * 100) : 0) + '%' }" />
              </div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ formatUsageNumber(row.total_tokens) }} {{ t('vendor.tokens') }} · {{ formatUsageCost(row.cost_usd) }}</p>
              <p v-if="row.upstream_model && row.upstream_model !== row.requested_model" class="break-words text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.upstreamModel') }}: {{ row.upstream_model }}</p>
            </div>
          </div>
          <p v-else class="flex min-h-40 items-center justify-center p-4 text-sm text-gray-500 dark:text-gray-400">{{ t('vendor.noUsageInPeriod') }}</p>
          <Pagination v-if="(summary.model_total || 0) > (summary.model_page_size || 100)"
            :page="summary.model_page || 1" :page-size="summary.model_page_size || 100" :total="summary.model_total!"
            :show-page-size-selector="false" @update:page="pageModels" />
        </section>
        <section class="card min-w-0 overflow-hidden" data-usage-tokens>
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <h3 class="text-sm font-semibold">{{ t('vendor.tokenDistribution') }}</h3>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.tokensWithCache') }}</span>
          </div>
          <div class="space-y-5 p-4">
            <div v-for="part in tokenParts" :key="part.key" class="space-y-2">
              <div class="flex flex-wrap items-baseline justify-between gap-2 text-xs">
                <span>{{ t('vendor.' + part.label) }}</span>
                <span class="tabular-nums">{{ formatUsageNumber(part.value) }}<span v-if="tokenTotal && part.value != null" class="ml-2 text-gray-500 dark:text-gray-400">{{ (part.value / tokenTotal * 100).toFixed(1) }}%</span></span>
              </div>
              <div class="h-3 overflow-hidden rounded bg-gray-100 dark:bg-dark-700" aria-hidden="true">
                <div class="h-full rounded" :class="part.color" :style="{ width: (tokenTotal && part.value != null ? part.value / tokenTotal * 100 : 0) + '%' }" />
              </div>
            </div>
          </div>
        </section>
      </div>
      <component :is="accountId ? 'details' : 'section'" class="card overflow-hidden" data-usage-daily>
        <component :is="accountId ? 'summary' : 'div'" class="space-y-1 p-4" :class="{ 'cursor-pointer': accountId }">
          <h3 class="inline text-sm font-semibold">
            {{ admin && !accountId ? dimensionLabel(dimension) : t(accountId ? 'vendor.dailyDetails' : 'vendor.daily') }}
          </h3>
          <p
            v-if="admin && !accountId && ['account', 'account_date'].includes(dimension)"
            class="text-xs text-gray-500 dark:text-gray-400"
          >
            {{ t('vendor.modelCostsHelp') }}
          </p>
          <p v-if="admin && !accountId && dimension === 'request'" class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('vendor.usageRecordsHelp') }}
          </p>
        </component>
        <RequestState :empty="!days?.items?.length"
          ><UsageTable
            :key="dimension"
            :rows="days?.items || []"
            :supplier-layout="!admin"
            :dimension="admin && !accountId ? dimension : 'date'"
            sortable
            :sort-by="sorting.days.key"
            :order="sorting.days.order"
            @sort="(key, order) => sortRows('days', key, order)"
            @account="openAccount" /></RequestState
        ><Pagination
          v-if="days?.total"
          :page="dayPage"
          :page-size="daySize"
          :total="days.total"
          @update:page="page('days', $event)"
          @update:page-size="page('days', 1, $event)"
        />
      </component>
      <section v-if="!admin && !accountId" class="card overflow-hidden">
        <div class="space-y-1 p-4">
          <h3 class="font-semibold">{{ t('vendor.byAccount') }}</h3>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.usageByAccountHelp') }}</p>
        </div>
        <RequestState :empty="!accounts?.items?.length"
          ><UsageTable
            :rows="accounts?.items || []"
            supplier-layout
            dimension="account"
            sortable
            :sort-by="sorting.accounts.key"
            :order="sorting.accounts.order"
            @sort="(key, order) => sortRows('accounts', key, order)"
            @account="openAccount" /></RequestState
        ><Pagination
          v-if="accounts?.total"
          :page="accountPage"
          :page-size="accountSize"
          :total="accounts.total"
          @update:page="page('accounts', $event)"
          @update:page-size="page('accounts', 1, $event)"
        />
      </section>
      <div v-if="admin && !accountId" class="grid min-w-0 gap-4 xl:grid-cols-2">
        <section
          v-for="key in ['models', 'results'] as const"
          v-show="summary?.[key]?.length || (key === 'models' && summary?.model_total)"
          :key="key"
          class="card min-w-0 overflow-hidden"
        >
          <h3 class="px-4 py-3 text-sm font-semibold">{{ t('vendor.' + key) }}</h3>
          <UsageTable
            :rows="summary?.[key] || []"
            :dimension="key === 'models' ? 'model' : 'result'"
          />
          <Pagination
            v-if="key === 'models' && summary?.model_total"
            :page="summary.model_page || 1"
            :page-size="summary.model_page_size || 100"
            :total="summary.model_total"
            :show-page-size-selector="false"
            @update:page="pageModels"
          />
        </section>
      </div>
      <p v-if="accountId && summary" class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">
        {{ t('vendor.costNotice') }}
        <span class="block">{{ t('vendor.snapshot') }}: {{ formatDateTime(summary.snapshot_at) || t('vendor.unknown') }} · {{ summary.timezone }}</span>
      </p>
    </RequestState>
  </section>
  <BaseDialog
    v-if="detailId && detailId !== accountId"
    show
    :title="t('vendor.history') + ' #' + detailId"
    width="extra-wide"
    body-class="bg-gray-50 dark:bg-dark-900"
    @close="detailId = undefined"
    ><UsageWorkbench
      :admin="admin"
      :vendor-id="detailVendorId"
      :account-id="detailId"
      :groups="groups"
  /></BaseDialog>
</template>

<style scoped>
.account-usage-filters[open] {
  flex-basis: 100%;
  order: 1;
}
.account-usage-filters > summary {
  width: fit-content;
}
</style>
