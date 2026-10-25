<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { platformDisplayName } from '@/constants/platformCatalog'
import type { Column } from '@/components/common/types'
import { formatDateTime } from '@/utils/format'
import { formatUsageCost, formatUsageNumber, displayPercent, otherModelCost } from '../helpers'
import type { UsageRow } from '../types'
const props = withDefaults(
  defineProps<{
    rows: UsageRow[]
    dimension?: string
    interactive?: boolean
    sortable?: boolean
    supplierLayout?: boolean
    sortBy?: string
    order?: 'asc' | 'desc'
  }>(),
  { interactive: true }
)
defineEmits<{
  account: [id: number, vendorId?: number]
  sort: [key: string, order: 'asc' | 'desc']
}>()
const { t } = useI18n()
const identitySlot = computed(() => (props.dimension === 'date' ? 'cell-date' : 'cell-identity'))
const accountColumns = computed(
  () => props.interactive && ['account', 'account_date'].includes(props.dimension || '')
)
const columns = computed<Column[]>(() => {
  const metric = (key: string, label: string, sortable = false): Column => ({
    key,
    label: t('vendor.' + label),
    sortable: props.sortable && sortable,
    class: 'text-right tabular-nums'
  })
  const identity =
    props.dimension === 'model'
      ? 'models'
      : props.dimension === 'result'
        ? 'results'
        : props.dimension === 'request'
          ? 'account'
          : props.dimension || 'name'
  return [
    {
      key: props.dimension === 'date' ? 'date' : 'identity',
      label: t('vendor.' + identity),
      sortable: props.sortable && props.dimension === 'date'
    },
    ...(props.dimension === 'request'
      ? [
          {
            key: 'created_at',
            label: t('vendor.requestTime'),
            sortable: props.sortable
          },
          { key: 'requested_model', label: t('dashboard.model') }
        ]
      : []),
    ...(accountColumns.value && props.supplierLayout
      ? [{ key: 'account_status', label: t('vendor.status') }]
      : []),
    ...(accountColumns.value && !props.supplierLayout
      ? [
          metric('opus_cost_usd', 'opusCost', true),
          metric('fable_cost_usd', 'fableCost', true),
          metric('other_cost_usd', 'otherModelCost')
        ]
      : []),
    ...(props.dimension !== 'request' ? [metric('requests', 'requests', true)] : []),
    metric('total_tokens', 'tokenConsumption', true),
    metric('cache_tokens', 'cacheTokens'),
    ...(!props.supplierLayout || accountColumns.value
      ? [metric('cache_hit_rate', 'cacheHit')]
      : []),
    metric('cost_usd', props.supplierLayout ? 'costShort' : 'cost', true),
    ...(props.dimension === 'request'
      ? [metric('avg_duration_ms', 'latency'), ...(props.rows.some(row => ['success', 'failed'].includes(row.result || '')) ? [{ key: 'result', label: t('vendor.results') }] : [])]
      : [])
  ]
})
const rowKey = (row: UsageRow) =>
  [
    row.usage_log_id,
    row.vendor_id,
    row.account_id,
    row.group_id,
    row.date,
    row.requested_model,
    row.upstream_model,
    row.result
  ].join(':')
function accountStatus(row: UsageRow) {
  if (row.account_deleted) return t('vendor.deleted')
  const status = row.account_status || row.status
  return t(
    'vendor.' +
      (['active', 'inactive', 'disabled', 'error'].includes(status || '') ? status : 'unknown')
  )
}
function rowDimension(row: UsageRow) {
  return (
    props.dimension ||
    (row.account_id
      ? 'account'
      : row.date
        ? 'date'
        : row.group_id || row.group_name
          ? 'group'
          : row.requested_model || row.upstream_model || row.model
            ? 'model'
            : row.result
              ? 'result'
              : 'vendor')
  )
}
function vendorLabel(row: UsageRow) {
  return [
    row.vendor_name === 'unknown' ? '' : row.vendor_name,
    row.vendor_id ? '#' + row.vendor_id : t('vendor.unknown')
  ].filter(Boolean).join(' · ')
}
function rowLabel(row: UsageRow) {
  switch (rowDimension(row)) {
    case 'account':
    case 'account_date':
    case 'request':
      return row.account_name || (row.account_id ? '#' + row.account_id : '—')
    case 'date':
      return row.date || '—'
    case 'group':
      return row.group_name || (row.group_id ? '#' + row.group_id : t('vendor.unknown'))
    case 'model':
      return row.requested_model || row.upstream_model || row.model || '—'
    case 'result':
      return t(
        'vendor.' + (['success', 'failed'].includes(row.result || '') ? row.result : 'unknown')
      )
    default:
      return vendorLabel(row)
  }
}
</script>
<template>
  <DataTable
    :class="{ 'supplier-usage-table': supplierLayout }"
    :data="rows"
    :columns="columns"
    :row-key="rowKey"
    :server-side-sort="true"
    :default-sort-key="sortBy"
    :default-sort-order="order || 'desc'"
    @sort="(key, direction) => $emit('sort', key, direction)"
  >
    <template #[identitySlot]="{ row }">
      <div class="min-w-40">
        <button
          v-if="
            interactive &&
            row.account_id &&
            ['account', 'account_date', 'request'].includes(rowDimension(row))
          "
          class="text-left font-medium text-primary-600 hover:underline dark:text-primary-400"
          @click="$emit('account', row.account_id, row.vendor_id)"
        >
          {{ rowLabel(row) }}
        </button>
        <span v-else data-usage-label class="font-medium">{{ rowLabel(row) }}</span>
        <p v-if="row.account_id" class="mt-1 flex flex-wrap items-center gap-1 text-xs text-gray-400">
          #{{ row.account_id }}
          <span v-if="row.account_archived" class="badge badge-gray">{{ t('vendor.archived') }}</span>
        </p>
        <p v-if="dimension === 'request'" class="mt-1 text-xs text-gray-500">
          {{ t('vendor.group') }}: {{ row.group_name || t('vendor.unknown') }}
        </p>
        <StatusBadge
          v-if="
            (!supplierLayout && accountColumns) ||
            (dimension === 'request' && (row.account_deleted || row.account_archived))
          "
          :status="row.account_deleted ? 'unknown' : row.account_status || row.status || 'unknown'"
          :label="accountStatus(row)"
          class="mt-1"
        />
        <p
          v-if="
            !supplierLayout && rowDimension(row) !== 'vendor' &&
            (accountColumns || row.account_id || row.vendor_id || row.vendor_name)
          "
          data-usage-scope
          class="mt-1 text-xs text-gray-500"
        >
          {{ t('vendor.usageSupplier') }}: {{ vendorLabel(row) }}
        </p>
        <p
          v-if="rowDimension(row) === 'model' && row.requested_model && row.upstream_model"
          class="mt-1 text-xs text-gray-500"
        >
          {{ t('vendor.upstreamModel') }}:
          {{ row.upstream_model === 'unknown' ? t('vendor.unknown') : row.upstream_model }}
        </p>
        <p v-if="row.created_at && dimension !== 'request'" class="mt-1 text-xs text-gray-500">
          {{ formatDateTime(row.created_at) }} · {{ row.requested_model }}
        </p>
        <p v-if="row.account_id && row.date" class="mt-1 text-xs text-gray-500">
          {{ row.date }}
        </p>
      </div>
    </template>
    <template #cell-account_status="{ row }">
      <StatusBadge
        :status="row.account_deleted ? 'unknown' : row.account_status || row.status || 'unknown'"
        :label="accountStatus(row)"
      />
    </template>
    <template #cell-created_at="{ row }">
      <span class="whitespace-nowrap tabular-nums">{{ formatDateTime(row.created_at) }}</span>
      <p class="mt-1 text-xs text-gray-400">#{{ row.usage_log_id }}</p>
    </template>
    <template #cell-requested_model="{ row }">
      <div class="inline-flex items-center gap-1.5 rounded px-2 py-1" :class="platformBadgeLightClass(row.platform)">
        <PlatformIcon v-if="row.platform" :platform="row.platform" size="sm" /><span>{{
          row.requested_model || t('vendor.unknown')
        }}</span>
      </div>
      <p class="mt-1 text-xs text-gray-500">
        {{ platformDisplayName(row.platform) }}
      </p>
      <p class="mt-1 text-xs text-gray-500">
        {{ t('vendor.upstreamModel') }}:
        {{
          row.upstream_model === 'unknown'
            ? t('vendor.unknown')
            : row.upstream_model || t('vendor.unknown')
        }}
      </p>
    </template>
    <template
      v-for="key in ['opus_cost_usd', 'fable_cost_usd', 'other_cost_usd', 'cost_usd'] as const"
      :key="key"
      #[`cell-${key}`]="{ row }"
      ><span class="whitespace-nowrap font-medium tabular-nums">{{
        formatUsageCost(key === 'other_cost_usd' ? otherModelCost(row) : row[key])
      }}</span></template
    >
    <template #cell-requests="{ row }">{{ formatUsageNumber(row.requests) }}</template>
    <template #cell-total_tokens="{ row }">
      <p v-if="!supplierLayout" class="font-medium tabular-nums">
        {{
          formatUsageNumber(
            row.total_tokens ??
              (row.input_tokens != null && row.output_tokens != null
                ? row.input_tokens + row.output_tokens
                : null)
          )
        }}
      </p>
      <p class="mt-1 whitespace-nowrap text-xs text-gray-500">
        {{ t('vendor.inputShort') }}
        {{ formatUsageNumber(row.input_tokens) }}
      </p>
      <p class="whitespace-nowrap text-xs text-gray-500">
        {{ t('vendor.outputShort') }}
        {{ formatUsageNumber(row.output_tokens) }}
      </p>
    </template>
    <template #cell-cache_tokens="{ row }">
      <p class="whitespace-nowrap text-xs">
        {{ t('vendor.cacheWriteShort') }}
        {{ formatUsageNumber(row.cache_creation_tokens) }}
      </p>
      <p class="mt-1 whitespace-nowrap text-xs text-gray-500">
        {{ t('vendor.cacheReadShort') }}
        {{ formatUsageNumber(row.cache_read_tokens) }}
      </p>
    </template>
    <template #cell-cache_hit_rate="{ row }">{{ displayPercent(row.cache_hit_rate) }}</template>
    <template #cell-avg_duration_ms="{ row }">
      {{ formatUsageNumber(row.avg_duration_ms) }}
      <p class="mt-1 whitespace-nowrap text-xs text-gray-500">
        {{ t('vendor.firstToken') }}:
        {{ formatUsageNumber(row.avg_first_token_ms) }}
      </p>
    </template>
    <template #cell-result="{ row }"
      ><StatusBadge
        :status="row.result === 'failed' ? 'error' : row.result || 'unknown'"
        :label="
          t('vendor.' + (['success', 'failed'].includes(row.result) ? row.result : 'unknown'))
        "
    /></template>
  </DataTable>
</template>

<style scoped>
.supplier-usage-table :deep(td) {
  @apply py-3 text-xs;
}
.supplier-usage-table :deep(th) {
  @apply py-2.5 normal-case tracking-normal;
}
</style>
