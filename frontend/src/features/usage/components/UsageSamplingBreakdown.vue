<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UsageSamplingDetails } from '../api/types'
defineProps<{ details?: UsageSamplingDetails | null }>()
const { t } = useI18n()
const cost = (value: number | null) => value == null ? '—' : `$${value.toFixed(8)}`
</script>

<template>
  <section v-if="details" class="mt-2 border-t border-gray-600 pt-2" data-testid="sampling-breakdown">
    <p class="font-semibold">{{ t('usage.samplingCount', { count: details.samples.length }) }}</p>
    <p class="my-1 max-w-lg text-gray-400">{{ t('usage.samplingHint') }}</p>
    <p v-if="details.pricing_mode === 'per_request'" class="text-gray-400">{{ t('usage.samplingRootPrice') }}</p>
    <div class="max-h-64 overflow-auto">
      <table class="w-full whitespace-nowrap text-right tabular-nums">
        <thead><tr><th class="p-1">#</th><th class="p-1">{{ t('admin.usage.inputTokens') }}</th><th class="p-1">{{ t('admin.usage.cacheReadTokens') }}</th><th class="p-1">{{ t('admin.usage.cacheCreationTokens') }}</th><th class="p-1">{{ t('admin.usage.outputTokens') }}</th><th class="p-1">{{ t('usage.original') }}</th><th class="p-1">{{ t('usage.userBilled') }}</th></tr></thead>
        <tbody><tr v-for="(sample, index) in details.samples" :key="index">
          <td class="p-1">{{ index + 1 }}</td><td class="p-1">{{ sample.input_tokens.toLocaleString() }}</td><td class="p-1">{{ sample.cache_read_tokens.toLocaleString() }}</td><td class="p-1">{{ sample.cache_creation_tokens.toLocaleString() }}</td><td class="p-1">{{ sample.output_tokens.toLocaleString() }}</td><td class="p-1">{{ cost(sample.total_cost) }}</td><td class="p-1">{{ cost(sample.actual_cost) }}</td>
        </tr></tbody>
      </table>
    </div>
    <p v-if="details.hosted_cost || details.hosted_actual_cost" class="mt-1">{{ t('usage.samplingHosted') }}: {{ cost(details.hosted_cost) }} / {{ cost(details.hosted_actual_cost) }}</p>
  </section>
</template>
