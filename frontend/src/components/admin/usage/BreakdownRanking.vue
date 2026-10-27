<template>
  <!-- 用量页排行 tab 的公共骨架：无卡片外观，依赖父级统一卡片；筛选/时间范围复用页面级筛选栏。
       身份列由调用方通过 identityColumns + cell-<key> 插槽提供，指标列与排序在这里统一处理 -->
  <div>
    <!-- Toolbar -->
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700/50 sm:px-6">
      <p class="text-xs text-gray-400 dark:text-gray-500">{{ t(subtitle) }}</p>
      <div class="flex items-center gap-3">
        <span v-if="!loading && items.length > 0" class="text-xs text-gray-400 dark:text-gray-500">
          {{ t(countLabel, { count: items.length }) }}
        </span>
        <div class="w-28">
          <Select v-model="limit" :options="limitOptions" @change="load" />
        </div>
      </div>
    </div>

    <!-- Table -->
    <div class="overflow-x-auto">
      <table class="w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="w-16 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400 sm:px-6">#</th>
            <th
              v-for="col in visibleIdentityColumns"
              :key="col.key"
              class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400"
            >
              {{ t(col.label) }}
            </th>
            <th
              v-for="col in visibleSortableColumns"
              :key="col.key"
              class="cursor-pointer select-none whitespace-nowrap px-4 py-3 text-right text-xs font-medium uppercase tracking-wider transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
              :class="sortBy === col.key ? 'text-primary-600 dark:text-primary-400' : 'text-gray-500 dark:text-dark-400'"
              @click="setSort(col.key)"
            >
              {{ t(col.label) }}
              <span v-if="sortBy === col.key" aria-hidden="true">↓</span>
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-if="loading">
            <td :colspan="columnCount" class="py-12 text-center">
              <LoadingSpinner />
            </td>
          </tr>
          <tr v-else-if="items.length === 0">
            <td :colspan="columnCount" class="py-12 text-center text-sm text-gray-400">
              {{ t('admin.dashboard.noDataAvailable') }}
            </td>
          </tr>
          <tr
            v-for="(item, index) in items"
            v-else
            :key="rowKey(item)"
            class="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/40"
            :title="t(rowHint)"
            @click="$emit('select', item)"
          >
            <td class="px-4 py-3 sm:px-6">
              <span
                v-if="index < 3"
                class="inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold"
                :class="RANK_BADGE_CLASSES[index]"
              >{{ index + 1 }}</span>
              <span v-else class="inline-block w-6 text-center text-sm tabular-nums text-gray-400">{{ index + 1 }}</span>
            </td>
            <!-- 身份列与指标列都按可见列定义循环渲染，保证 thead 与 td 一一对应 -->
            <td
              v-for="col in visibleIdentityColumns"
              :key="col.key"
              class="truncate px-4 py-3 text-sm"
              :class="col.cellClass ?? 'max-w-[260px] font-medium text-gray-700 dark:text-gray-200'"
            >
              <slot :name="`cell-${col.key}`" :item="item" />
            </td>
            <td
              v-for="col in visibleSortableColumns"
              :key="col.key"
              class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums"
              :class="metricCellClass(col.key)"
            >{{ metricText(item, col.key) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts" generic="T extends BreakdownRow">
import { computed, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserBreakdownParams } from '@/api/admin/dashboard'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

/** 排行行必须带的指标字段；user-breakdown / api-key-breakdown 的返回项都满足 */
export interface BreakdownRow {
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

/** 身份列（如用户、密钥）：表头由 label 渲染，单元格由调用方的 cell-<key> 插槽渲染 */
export interface BreakdownIdentityColumn {
  key: string
  /** i18n key */
  label: string
  cellClass?: string
}

// 指标列对所有排行一致：后端各排行端点共用同一份 ORDER BY allowlist。
type SortKey = NonNullable<UserBreakdownParams['sort_by']>
const sortableColumns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'admin.usage.tokenRanking.columns.requests' },
  { key: 'input_tokens', label: 'admin.usage.tokenRanking.columns.inputTokens' },
  { key: 'output_tokens', label: 'admin.usage.tokenRanking.columns.outputTokens' },
  { key: 'cache_tokens', label: 'admin.usage.tokenRanking.columns.cacheTokens' },
  { key: 'total_tokens', label: 'admin.usage.tokenRanking.columns.totalTokens' },
  { key: 'actual_cost', label: 'admin.usage.tokenRanking.columns.cost' },
]

const props = withDefaults(defineProps<{
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
  /** 拉取一页排行数据；筛选参数与 user-breakdown 完全一致 */
  fetch: (params: UserBreakdownParams) => Promise<T[]>
  rowKey: (item: T) => number | string
  identityColumns: BreakdownIdentityColumn[]
  /** i18n keys */
  subtitle: string
  countLabel: string
  rowHint: string
  /** 由父级列设置下拉控制；未传时全部显示 */
  visibleColumnKeys?: string[]
  /**
   * 所在 tab 是否可见。父级用 v-show 切 tab 时组件仍活着，筛选一变就会请求；
   * 置 false 时不请求，只记下"过期"，切回来补一次。
   */
  active?: boolean
}>(), {
  active: true,
})

defineEmits<{ (e: 'select', item: T): void }>()

defineSlots<Record<`cell-${string}`, (scope: { item: T }) => unknown>>()

const { t } = useI18n()

const limitOptions = [
  { value: 20, label: 'Top 20' },
  { value: 50, label: 'Top 50' },
  { value: 100, label: 'Top 100' },
  { value: 200, label: 'Top 200' },
]

// 前三名金/银/铜徽章
const RANK_BADGE_CLASSES = [
  'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-400',
  'bg-gray-200 text-gray-600 dark:bg-gray-500/20 dark:text-gray-300',
  'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-400',
]

// 列显隐由父级统一管理。未传 prop 时全部显示，这样组件单独使用也不会空表。
// "不可隐藏"的约束由父级列设置负责，这里只忠实按 visibleColumnKeys 渲染。
const isColumnVisible = (key: string) =>
  !props.visibleColumnKeys || props.visibleColumnKeys.includes(key)

const visibleIdentityColumns = computed(() =>
  props.identityColumns.filter((col) => isColumnVisible(col.key))
)

// 指标列受列设置控制；排序仍按 sortBy 生效，即便对应列被隐藏也不影响后端排序。
const visibleSortableColumns = computed(() =>
  sortableColumns.filter((col) => isColumnVisible(col.key))
)

// 空状态/加载态的 colspan：# + 可见身份列 + 可见指标列
const columnCount = computed(
  () => 1 + visibleIdentityColumns.value.length + visibleSortableColumns.value.length
)

const items = ref<T[]>([]) as Ref<T[]>
const loading = ref(false)
const sortBy = ref<SortKey>('total_tokens')
const limit = ref(50)
let reqSeq = 0
// active=false 期间被跳过的加载，切回时补一次
let stale = false

const fmtTokens = (v: number) => formatCompactNumber(v)
const fmtCost = (v: number) => formatCostFixed(v, 4)

// 总 Token 与费用是这张表的重点，用强调色。
const metricCellClass = (key: SortKey) => {
  if (key === 'total_tokens') return 'font-medium text-gray-900 dark:text-gray-100'
  if (key === 'actual_cost') return 'font-medium text-green-600 dark:text-green-400'
  return 'text-gray-500 dark:text-gray-400'
}

const metricText = (item: T, key: SortKey) => {
  if (key === 'requests') return item.requests.toLocaleString()
  if (key === 'cost' || key === 'actual_cost') return `$${fmtCost(item[key])}`
  return fmtTokens(item[key])
}

const setSort = (key: SortKey) => {
  if (sortBy.value === key) return
  sortBy.value = key
  load()
}

const load = async () => {
  if (!props.active) {
    stale = true
    return
  }
  stale = false
  const seq = ++reqSeq
  loading.value = true
  try {
    const params: UserBreakdownParams = {
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      sort_by: sortBy.value,
      limit: limit.value,
    }
    if (props.model) params.model = props.model
    const res = await props.fetch(params)
    if (seq !== reqSeq) return
    items.value = res || []
  } catch {
    if (seq !== reqSeq) return
    items.value = []
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

// Reload when the shared filters / date range / model change.
watch(
  () => [props.startDate, props.endDate, props.model, JSON.stringify(props.filters)],
  () => load(),
  { immediate: true }
)

watch(
  () => props.active,
  (active) => {
    if (active && stale) load()
  }
)

defineExpose({ reload: load })
</script>
