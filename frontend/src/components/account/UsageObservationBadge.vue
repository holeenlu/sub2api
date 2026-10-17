<template>
  <span v-if="observation" class="inline-flex items-center gap-1 text-[9px] text-gray-500 dark:text-gray-400" :title="description" :data-source="observation.source">
    <span class="rounded border border-gray-200 px-1 dark:border-gray-700">{{ sourceLabel }}</span>
    <time v-if="showTime && observation.sampled_at" :datetime="observation.sampled_at">{{ sampledAt }}</time>
    <span v-else-if="showTime">{{ t('admin.accounts.usageMetadata.unknownTime') }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageObservation } from '@/types'

const props = defineProps<{ observation?: UsageObservation | null; showTime?: boolean }>()
const { t } = useI18n()
const sourceLabel = computed(() => t('admin.accounts.usageMetadata.sources.' + (props.observation?.source ?? 'stored_snapshot')))
const sampledAt = computed(() => {
  const value = props.observation?.sampled_at
  if (!value || !Number.isFinite(Date.parse(value))) return t('admin.accounts.usageMetadata.unknownTime')
  return new Date(value).toLocaleString()
})
const description = computed(() => sourceLabel.value + ' · ' + t('admin.accounts.usageMetadata.sampledAt', { time: sampledAt.value }))
</script>
