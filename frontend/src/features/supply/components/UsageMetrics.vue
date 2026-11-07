<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { formatUsageNumber, formatUsageCost, displayPercent, otherModelCost } from '../helpers'
import type { Metrics } from '../types'
defineProps<{ metrics?: Metrics | null; compact?: boolean }>()
const { t } = useI18n()
const durationSeconds = (value?: number | null) => value == null || !Number.isFinite(value) ? '—' : (value / 1000).toLocaleString('en-US', { maximumFractionDigits: value < 1000 ? 3 : 2 })
const fields = [
  ['cost_usd', 'cost', 'dollar'],
  ['requests', 'requests', 'document'],
  ['total_tokens', 'tokens', 'bolt'],
  ['cache_tokens', 'cacheTokens', 'database'],
  ['success_rate', 'successRate', 'checkCircle'],
  ['avg_duration_ms', 'averageLatency', 'clock'],
  ['avg_first_token_ms', 'timeToFirstToken', 'clock']
] as const satisfies readonly (readonly [
  keyof Metrics,
  string,
  InstanceType<typeof Icon>['$props']['name']
])[]
</script>
<template>
  <div
    :class="
      compact
        ? 'card grid grid-cols-2 divide-x divide-gray-100 overflow-hidden dark:divide-dark-700 lg:grid-cols-[3fr_1fr_1fr_1fr]'
        : 'grid grid-cols-1 gap-3 min-[420px]:grid-cols-2 lg:grid-cols-4'
    "
  >
    <div
      v-for="[field, label, icon] in compact
        ? fields.slice(0, 4)
        : [fields[1], fields[0], fields[4], fields[2], fields[3], fields[5], fields[6]]"
      :key="field"
      :data-metric="field"
      :class="compact ? 'min-w-0 px-4 py-4' : 'card min-w-0 p-4'"
    >
      <div class="flex items-center justify-between gap-2">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
          {{
            t(
              'vendor.' +
                (compact
                  ? {
                      cost_usd: 'totalConsumption',
                      requests: 'totalRequests',
                      total_tokens: 'tokenConsumption',
                      cache_tokens: 'cacheSummary'
                    }[field as 'cost_usd' | 'requests' | 'total_tokens' | 'cache_tokens']
                  : field === 'cache_tokens'
                    ? 'cacheHit'
                    : label)
            )
          }}
        </p>
        <Icon v-if="!compact" :name="icon" size="sm" class="text-gray-400" />
      </div>
      <p
        class="break-words font-semibold tabular-nums"
        :class="[
          compact ? 'mt-1 text-xl' : 'mt-2 text-2xl',
          { 'text-primary-600 dark:text-primary-400': field === 'cost_usd', 'text-emerald-600 dark:text-emerald-400': field === 'success_rate' && metrics?.success_rate != null }
        ]"
      >
        {{
          !compact && field === 'cache_tokens'
            ? displayPercent(metrics?.cache_hit_rate)
            : field === 'success_rate'
              ? displayPercent(metrics?.[field])
              : field === 'cost_usd'
                ? formatUsageCost(metrics?.[field])
                : field === 'avg_duration_ms' || field === 'avg_first_token_ms'
                  ? durationSeconds(metrics?.[field])
                  : formatUsageNumber(metrics?.[field])
        }}
        <span
          v-if="
            (field === 'avg_duration_ms' || field === 'avg_first_token_ms') &&
            metrics?.[field] != null
          "
          class="text-sm font-normal"
        >
          s</span
        >
      </p>
      <component :is="compact ? 'div' : 'details'" v-if="field === 'cost_usd'" class="mt-2 space-y-1 text-xs text-gray-500 dark:text-gray-400">
        <summary v-if="!compact" class="cursor-pointer py-1">{{ t('vendor.modelCostBreakdown') }}</summary>
        <p
          v-for="[key, label, value] in [
            ['opus_cost_usd', 'opusCost', metrics?.opus_cost_usd],
            ['fable_cost_usd', 'fableCost', metrics?.fable_cost_usd],
            ['other_cost_usd', 'otherModelCost', otherModelCost(metrics)]
          ] as const"
          :key="key"
          :data-cost-family="key"
          class="flex flex-wrap justify-between gap-x-2"
        >
          <span>{{ t('vendor.' + label) }}</span>
          <span class="tabular-nums">{{ formatUsageCost(value) }}</span>
        </p>
      </component>
      <p v-if="field === 'requests' && !compact" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        {{ t('vendor.success') }} {{ formatUsageNumber(metrics?.successful_requests) }} /
        {{ t('vendor.failed') }} {{ formatUsageNumber(metrics?.failed_requests) }}
      </p>
      <p v-if="field === 'success_rate'" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        {{ metrics?.success_rate == null ? t('vendor.outcomesUnavailable') : t('vendor.successFraction', {
          success: formatUsageNumber(metrics?.successful_requests),
          total: formatUsageNumber(metrics?.requests)
        }) }}
      </p>
      <p
        v-if="field === 'total_tokens'"
        class="text-xs text-gray-500 dark:text-gray-400"
        :class="compact ? 'mt-1' : 'mt-2'"
      >
        {{ t(compact ? 'vendor.inputShort' : 'vendor.inputTokens') }}:
        {{ formatUsageNumber(metrics?.input_tokens) }} ·
        {{ t(compact ? 'vendor.outputShort' : 'vendor.outputTokens') }}:
        {{ formatUsageNumber(metrics?.output_tokens) }}
      </p>
      <p
        v-if="field === 'cache_tokens'"
        class="text-xs text-gray-500 dark:text-gray-400"
        :class="compact ? 'mt-1' : 'mt-2'"
      >
        {{ t(compact ? 'vendor.cacheWriteShort' : 'vendor.cacheWrite') }}:
        {{ formatUsageNumber(metrics?.cache_creation_tokens) }} ·
        {{ t(compact ? 'vendor.cacheReadShort' : 'vendor.cacheRead') }}:
        {{ formatUsageNumber(metrics?.cache_read_tokens) }}
      </p>
    </div>
  </div>
</template>
