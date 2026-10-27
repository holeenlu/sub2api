<template>
  <div v-if="details && (actions.length || recovery)" data-test="excel-bps-403-details"
    class="mt-3 space-y-1 rounded bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
    <p>{{ t('admin.accounts.openai.excelBPS403SavedState') }}</p>
    <p v-for="line in [...actions, ...diagnosticLines]" :key="line">{{ line }}</p>
  </div>
  <span v-else-if="active" data-test="excel-bps-403-badge"
    class="mt-1 inline-flex items-center self-start rounded bg-amber-400 px-1.5 py-0.5 text-[11px] font-semibold leading-4 text-amber-950 ring-1 ring-amber-500"
    :title="title">{{ label }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountListItem } from '@/types'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  account: Pick<AccountListItem, 'platform' | 'type' | 'extra' | 'group_ids' | 'excel_bps_403_recovery'>
  details?: boolean
  // The account's current groups, used to name the destination group.
  groups?: { id: number; name: string }[]
}>()

const { t } = useI18n()

function parseTime(value: unknown): Date | null {
  if (typeof value !== 'string') return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

// Historical actions remain available in account details, even while routing
// or recovery is deliberately disabled. They are not a ban diagnosis.
const actions = computed(() => {
  const { platform, type, extra, group_ids: groupIDs = [] } = props.account
  if (platform !== 'openai' || type !== 'oauth' || !extra) return []
  const lines: string[] = []
  const disabledAt = extra.openai_excel_bps === true ? null : parseTime(extra.openai_excel_bps_403_disabled_at)
  if (disabledAt) {
    lines.push(t('admin.accounts.openai.excelBPS403BadgeDisabled', { time: formatDateTime(disabledAt) }))
  }
  const movedAt = parseTime(extra.openai_excel_bps_403_moved_at)
  const target = extra.openai_excel_bps_403_moved_group_id
  if (movedAt && typeof target === 'number') {
    const time = formatDateTime(movedAt)
    if (target === 0 && groupIDs.length === 0) {
      lines.push(t('admin.accounts.openai.excelBPS403BadgeLeftGroups', { time }))
    } else if (target > 0 && groupIDs.length === 1 && groupIDs[0] === target) {
      const group = props.groups?.find(item => item.id === target)?.name ?? `#${target}`
      lines.push(t('admin.accounts.openai.excelBPS403BadgeMoved', { time, group }))
    }
  }
  return lines
})

const recovery = computed(() => props.account.excel_bps_403_recovery)
const active = computed(() => recovery.value && ['pending', 'failed', 'probing', 'unknown'].includes(recovery.value.state))
const label = computed(() => t(`admin.accounts.openai.excelBPS403State_${recovery.value?.state ?? 'history'}`))
const diagnosticLines = computed(() => {
  const state = recovery.value
  if (!state) return []
  const lines = [label.value]
  if (state.reason) lines.push(t(`admin.accounts.openai.excelBPS403Pause_${state.reason}`))
  lines.push(t('admin.accounts.openai.excelBPS403EffectiveInterval', { minutes: state.interval_minutes }))
  const attempt = parseTime(state.last_probe_at)
  if (attempt) lines.push(t('admin.accounts.openai.excelBPS403LastAttempt', { time: formatDateTime(attempt) }))
  const next = parseTime(state.next_probe_at)
  if (next) lines.push(t('admin.accounts.openai.excelBPS403NextAttempt', { time: formatDateTime(next) }))
  const result = state.last_result
  if (result) {
    lines.push(t(state.last_result_current_cycle ? 'admin.accounts.openai.excelBPS403CurrentCycleResult' : 'admin.accounts.openai.excelBPS403HistoricalResult'))
    if (result.attempted_at && parseTime(result.attempted_at)) lines.push(t('admin.accounts.openai.excelBPS403LastAttempt', { time: formatDateTime(new Date(result.attempted_at)) }))
    if (result.error_code || result.error_type || result.upstream_request_id) lines.push(t('admin.accounts.openai.excelBPS403UpstreamIdentifiers', { code: result.error_code || '—', type: result.error_type || '—', requestId: result.upstream_request_id || '—' }))
    lines.push(t('admin.accounts.openai.excelBPS403ProbeResult', {
    time: parseTime(result.completed_at) ? formatDateTime(new Date(result.completed_at)) : '—',
    reason: t(`admin.accounts.openai.excelBPS403Result_${result.reason}`),
    stage: result.stage, status: result.http_status ?? '—', duration: result.duration_ms
  }))
  }
  return lines
})
const title = computed(() =>
  [t('admin.accounts.openai.excelBPS403BadgeNote'), ...actions.value, ...diagnosticLines.value].join('\n')
)
</script>
