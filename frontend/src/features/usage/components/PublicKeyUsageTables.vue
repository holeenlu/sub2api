<template>
          <!-- Usage Stats Card -->
          <div
            v-if="usageStatCells.length > 0"
            class="fade-up fade-up-delay-3 rounded-2xl border border-gray-200 bg-white/90 backdrop-blur-sm overflow-hidden dark:border-dark-700 dark:bg-dark-900/90"
          >
            <div class="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
              <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.tokenStats') }}</h3>
            </div>
            <div class="grid grid-cols-2 md:grid-cols-4 gap-px bg-gray-100 dark:bg-dark-800">
              <div
                v-for="(cell, i) in usageStatCells"
                :key="i"
                class="bg-white px-6 py-4 dark:bg-dark-900"
              >
                <div class="text-xs text-gray-500 dark:text-dark-400 mb-1">{{ cell.label }}</div>
                <div class="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ cell.value }}</div>
              </div>
            </div>
          </div>

          <!-- Daily Usage Table -->
          <div
            v-if="showDailyUsage"
            class="fade-up fade-up-delay-4 rounded-2xl border border-gray-200 bg-white/90 backdrop-blur-sm overflow-hidden dark:border-dark-700 dark:bg-dark-900/90"
          >
            <div class="flex flex-col gap-3 px-8 py-5 border-b border-gray-200 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
              <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.dailyDetail') }}</h3>
              <div class="inline-flex rounded-lg border border-gray-200 bg-white p-0.5 dark:border-dark-700 dark:bg-dark-950">
                <button
                  v-for="option in dailyUsageOptions"
                  :key="option.value"
                  @click="emit('days', option.value)"
                  class="min-w-12 rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                  :class="dailyUsageDays === option.value
                    ? 'bg-primary-500 text-white'
                    : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <div v-if="dailyUsageRows.length > 0" class="overflow-x-auto">
              <table class="w-full">
                <thead>
                  <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-950">
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.date') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.requests') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.inputTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.outputTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cacheReadTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cacheWriteTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cost') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="row in dailyUsageRows"
                    :key="row.date"
                    class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
                  >
                    <td class="px-4 py-3 text-sm font-medium whitespace-nowrap text-gray-900 dark:text-white">{{ row.date }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(row.requests) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(row.input_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(row.output_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(row.cache_read_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(row.cache_write_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right font-medium text-gray-900 dark:text-white">{{ usd(row.actual_cost != null ? row.actual_cost : row.cost) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="px-8 py-8 text-center text-sm text-gray-500 dark:text-dark-400">
              {{ t('keyUsage.noDailyUsage') }}
            </div>
          </div>

          <!-- Model Stats Table -->
          <div
            v-if="modelStats.length > 0"
            class="fade-up fade-up-delay-4 rounded-2xl border border-gray-200 bg-white/90 backdrop-blur-sm overflow-hidden dark:border-dark-700 dark:bg-dark-900/90"
          >
            <div class="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
              <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.modelStats') }}</h3>
            </div>
            <div class="overflow-x-auto">
              <table class="w-full">
                <thead>
                  <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-950">
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.model') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.requests') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.inputTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.outputTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cacheCreationTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cacheReadTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.totalTokens') }}</th>
                    <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.cost') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="(m, i) in modelStats"
                    :key="i"
                    class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
                  >
                    <td class="px-4 py-3 text-sm font-medium whitespace-nowrap text-gray-900 dark:text-white">{{ m.model || '-' }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.requests) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.input_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.output_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.cache_creation_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.cache_read_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right text-gray-700 dark:text-dark-200">{{ fmtNum(m.total_tokens) }}</td>
                    <td class="px-4 py-3 text-sm tabular-nums text-right font-medium text-gray-900 dark:text-white">{{ usd(m.actual_cost != null ? m.actual_cost : m.cost) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyDailyUsage, PublicKeyModelUsage } from '../api/publicKeyUsage'
import { publicUsageUsd as usd, publicUsageNumber as fmtNum } from '../model/publicKeyUsageFormat'
defineProps<{ usageStatCells: Array<{ label: string; value: string }>; dailyUsageRows: PublicKeyDailyUsage[]; modelStats: PublicKeyModelUsage[]; showDailyUsage: boolean; dailyUsageDays: 7 | 30 | 90 }>()
const emit = defineEmits<{ days: [value: 7 | 30 | 90] }>(), { t } = useI18n()
const dailyUsageOptions = computed(() => [7, 30, 90].map(value => ({ value: value as 7 | 30 | 90, label: t('keyUsage.dateRange' + value + 'd') })))
</script>
