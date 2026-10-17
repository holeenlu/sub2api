<template>
  <div class="space-y-1 text-[10px] text-gray-600 dark:text-gray-400" data-test="anthropic-usage-details">
    <div v-if="details.profile_access !== 'granted'" class="text-amber-600 dark:text-amber-400">
      {{ t('admin.accounts.usageMetadata.permissions.' + details.profile_access) }}
    </div>
    <div v-for="row in highlights" :key="row.key" class="flex flex-wrap items-center gap-1">
      <span>{{ label(row.key) }}: {{ formatValue(row.observation.value) }}</span>
      <UsageObservationBadge :observation="row.observation" />
    </div>
    <details @toggle="onToggle">
      <summary class="cursor-pointer text-blue-600 dark:text-blue-400">{{ t('admin.accounts.usageMetadata.details') }}</summary>
      <div v-if="expanded" class="mt-1 max-h-80 space-y-2 overflow-auto rounded border border-gray-200 p-2 dark:border-gray-700">
        <p>{{ t('admin.accounts.usageMetadata.snapshotHint') }}</p>
        <div v-for="row in rows" :key="row.key" class="space-y-0.5 break-words" :data-field="row.key">
          <div>{{ label(row.key) }}: {{ formatValue(row.observation.value) }}</div>
          <UsageObservationBadge :observation="row.observation" show-time />
        </div>
        <div v-if="limits.length" class="space-y-1">
          <div class="font-medium">{{ t('admin.accounts.usageMetadata.limits') }}</div>
          <div v-for="(limit, index) in limits" :key="index" class="rounded bg-gray-50 p-1 dark:bg-gray-800" data-test="anthropic-limit">
            <div>{{ limit.kind }}<span v-if="limit.group"> · {{ limit.group }}</span> · {{ limit.percent == null ? '—' : limit.percent + '%' }}</div>
            <div v-if="limit.scope">{{ scopeLabel(limit) }}</div>
            <div v-if="limit.resets_at">{{ t('admin.accounts.usageMetadata.fields.resets_at') }}: {{ formatTime(limit.resets_at) }}</div>
            <div v-if="limit.severity">{{ t('admin.accounts.usageMetadata.fields.severity') }}: {{ limit.severity }}</div>
            <div v-if="limit.is_active != null">{{ t('admin.accounts.usageMetadata.fields.is_active') }}: {{ formatValue(limit.is_active) }}</div>
            <UsageObservationBadge :observation="details.observations.limits" show-time />
          </div>
        </div>
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AnthropicUsageDetails, AnthropicUsageLimit, UsageObservation } from '@/types'
import UsageObservationBadge from './UsageObservationBadge.vue'

const props = defineProps<{ details: AnthropicUsageDetails }>()
const { t, te } = useI18n()
const expanded = ref(false)
const onToggle = (event: Event) => { expanded.value = (event.target as HTMLDetailsElement).open }
const highlights = computed(() => ['profile.rate_limit_tier', 'limit.status', 'limit.representative_claim', 'overage.status', 'overage.disabled_reason']
  .filter(key => props.details.observations[key]?.value != null)
  .map(key => ({ key, observation: props.details.observations[key] })))
const rows = computed(() => Object.entries(props.details.observations).flatMap(([key, observation]) => {
  if (key === 'limits') return []
  if (key.endsWith('.window_stats') && observation.value && typeof observation.value === 'object') {
    return Object.entries(observation.value).map(([field, value]) => ({ key: key + '.' + field, observation: { ...observation, value } }))
  }
  return [{ key, observation }]
}).sort((a, b) => a.key.localeCompare(b.key)))
const limits = computed(() => Array.isArray(props.details.observations.limits?.value)
  ? props.details.observations.limits.value as AnthropicUsageLimit[] : [])
const label = (key: string) => key.split('.').map(part => {
  if (key === 'extra_usage.utilization' && part === 'utilization') return t('admin.accounts.usageMetadata.fields.utilization_raw')
  const windows: Record<string, string> = { five_hour: '5H', seven_day: '7D', seven_day_fable: '7D F', seven_day_sonnet: '7D S', seven_day_opus: '7D O' }
  if (windows[part]) return windows[part]
  const path = 'admin.accounts.usageMetadata.fields.' + part
  return te(path) ? t(path) : part
}).join(' · ')
const formatTime = (value: string) => Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : value
const formatValue = (value: UsageObservation['value']): string => {
  if (value == null || value === '') return t('admin.accounts.usageMetadata.unknown')
  if (typeof value === 'boolean') return t(value ? 'admin.accounts.usageMetadata.yes' : 'admin.accounts.usageMetadata.no')
  if (typeof value === 'number') return Number.isFinite(value) ? value.toLocaleString(undefined, { maximumFractionDigits: 4 }) : '—'
  if (Array.isArray(value)) return value.length ? value.join(' ') : t('admin.accounts.usageMetadata.unknown')
  if (typeof value === 'string' && /^\d{4}-\d\d-\d\dT/.test(value)) return formatTime(value)
  return String(value)
}
const scopeLabel = (limit: AnthropicUsageLimit) => Object.entries(limit.scope ?? {}).map(([kind, scope]) => kind + ': ' + (scope.display_name || scope.id || t('admin.accounts.usageMetadata.unknown'))).join(' · ')
</script>
