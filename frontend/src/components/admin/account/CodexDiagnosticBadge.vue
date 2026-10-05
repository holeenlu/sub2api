<template>
  <button v-if="summary" type="button" class="mt-1 inline-flex max-w-full items-center gap-1 self-start rounded-md px-2 py-1 text-xs font-medium"
    :class="tone" :title="title" @click="$emit('open')">
    <span>{{ label }}</span>
    <span v-if="stale" class="opacity-75">· {{ t('admin.accounts.codexMonitor.stale') }}</span>
  </button>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DiagnosticSummary } from '@/api/admin/codexDiagnostics'
const props = defineProps<{ summary?: DiagnosticSummary | null }>()
defineEmits<{ open: [] }>()
const { t } = useI18n()
const stale = computed(() => props.summary?.stale === true)
const label = computed(() => t('admin.accounts.codexMonitor.status.' + (props.summary?.status || 'unknown')))
const title = computed(() => t('admin.accounts.codexMonitor.lastCheck') + ': ' + (props.summary?.checked_at ? new Date(props.summary.checked_at).toLocaleString() : '—'))
const tone = computed(() => {
  if (stale.value) return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
  if (props.summary?.status === 'normal') return 'bg-teal-50 text-teal-700 dark:bg-teal-900/30 dark:text-teal-300'
  if (props.summary?.status === 'degraded') return 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  return 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
})
</script>
