<template>
  <span
    v-if="presentation"
    data-test="bps-failure-badge"
    class="mt-1 inline-flex items-center self-start rounded px-1.5 py-0.5 text-[11px] font-semibold leading-4 ring-1"
    :class="presentation.historical ? 'bg-gray-100 text-gray-600 ring-gray-300 dark:bg-gray-800 dark:text-gray-300' : 'bg-amber-400 text-amber-950 ring-amber-500'"
    :title="presentation.title"
  >
    {{ presentation.label }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountListItem } from '@/features/resources/api/accountTypes';
import { readBPSRecoveryPresentation } from '@/features/resources/model/bpsRecoveryPresentation'
import { formatDateTime } from '@/shared/lib/format'
import { parseRfc3339Timestamp } from '@/shared/lib/parseTimestamp'

const props = defineProps<{
  account: Pick<AccountListItem, 'extra' | 'group_ids' | 'excel_bps_403_recovery'> & { platform: string; type: string }
  // The account's current groups, used to name the destination group.
  groups?: { id: number; name: string }[]
}>()

const { t } = useI18n()



// Open reason codes only select reviewed keys through Map; inherited object keys
// cannot become translation parameters. Recovery state owns authorization policy.
const failureReasonKeys = new Map([
  ['account_forbidden', 'admin.accounts.openai.excelBPSFailureAccountForbidden'],
  ['invalid_grant', 'admin.accounts.openai.excelBPSFailureAuthorizationInvalid'],
  ['token_revoked', 'admin.accounts.openai.excelBPSFailureAuthorizationRevoked'],
  ['grant_revoked', 'admin.accounts.openai.excelBPSFailureAuthorizationRevoked'],
  ['refreshed_access_token_rejected', 'admin.accounts.openai.excelBPSFailureRefreshedTokenRejected'],
  ['refresh_grant_missing', 'admin.accounts.openai.excelBPSFailureRefreshGrantMissing'],
  ['grant_missing', 'admin.accounts.openai.excelBPSFailureGrantMissing'],
  ['authorization_unavailable', 'admin.accounts.openai.excelBPSFailureAuthorizationUnavailable']
])
const pausedReasonKeys = new Map([
  ['global_disabled', 'admin.accounts.openai.excelBPSRecoveryGlobalDisabled'],
  ['cloud_enabled', 'admin.accounts.openai.excelBPSRecoveryCloudEnabled'],
  ['manual_disabled', 'admin.accounts.openai.excelBPSRecoveryManualDisabled'],
  ['recovery_disabled', 'admin.accounts.openai.excelBPSRecoveryDisabled'],
  ['no_text_model', 'admin.accounts.openai.excelBPSRecoveryNoTextModel'],
  ['account_ineligible', 'admin.accounts.openai.excelBPSRecoveryAccountIneligible'],
  ['account_inactive', 'admin.accounts.openai.excelBPSRecoveryAccountInactive'],
  ['scheduling_paused', 'admin.accounts.openai.excelBPSRecoverySchedulingPaused'],
  ['account_expired', 'admin.accounts.openai.excelBPSRecoveryAccountExpired'],
  ['invalid_timestamp', 'admin.accounts.openai.excelBPSRecoveryInvalidTimestamp']
])

const probeResultReasonKeys = new Map([
  ['global_disabled', 'admin.accounts.openai.excelBPS403Result_global_disabled'],
  ['success', 'admin.accounts.openai.excelBPS403Result_success'],
  ['probe_unavailable', 'admin.accounts.openai.excelBPS403Result_probe_unavailable'],
  ['identity_unavailable', 'admin.accounts.openai.excelBPS403Result_identity_unavailable'],
  ['authorization_failed', 'admin.accounts.openai.excelBPS403Result_authorization_failed'],
  ['transport_failed', 'admin.accounts.openai.excelBPS403Result_transport_failed'],
  ['empty_response', 'admin.accounts.openai.excelBPS403Result_empty_response'],
  ['http_rejected', 'admin.accounts.openai.excelBPS403Result_http_rejected'],
  ['read_failed', 'admin.accounts.openai.excelBPS403Result_read_failed'],
  ['response_too_large', 'admin.accounts.openai.excelBPS403Result_response_too_large'],
  ['invalid_response', 'admin.accounts.openai.excelBPS403Result_invalid_response'],
  ['response_mismatch', 'admin.accounts.openai.excelBPS403Result_response_mismatch'],
  ['timeout', 'admin.accounts.openai.excelBPS403Result_timeout'],
  ['canceled', 'admin.accounts.openai.excelBPS403Result_canceled'],
  ['prepare_failed', 'admin.accounts.openai.excelBPS403Result_prepare_failed'],
  ['state_write_failed', 'admin.accounts.openai.excelBPS403Result_state_write_failed'],
])

function failureReasonLabel(reason: string | null): string {
  return t(failureReasonKeys.get(reason ?? '') ?? 'admin.accounts.openai.excelBPSFailureReasonUnknown')
}

function pausedReasonLabel(reason: string | null): string {
  return t(pausedReasonKeys.get(reason ?? '') ?? 'admin.accounts.openai.excelBPSFailureReasonUnknown')
}

// The safe API projection owns recovery state. Historical group actions remain
// visible only while current memberships match; old shutdown times are not state.
const presentation = computed(() => {
  const { platform, type, extra, group_ids: groupIDs } = props.account
  if (platform !== 'openai' || type !== 'oauth') return null
  const lines: string[] = []
  let label = t('admin.accounts.openai.excelBPS403Badge')
  let note = t('admin.accounts.openai.excelBPS403BadgeNote')
  let historical = false
  const recovery = readBPSRecoveryPresentation(props.account.excel_bps_403_recovery)
  if (extra?.openai_excel_bps !== true && recovery && recovery.state !== 'history') {
    note = t('admin.accounts.openai.excelBPSFailureNote')
    switch (recovery.state) {
      case 'reauthorization_required':
        label = t('admin.accounts.openai.excelBPSFailureReauth')
        lines.push(failureReasonLabel(recovery.reason), t('admin.accounts.openai.excelBPSFailureReauthRequired'))
        break
      case 'paused':
        label = t('admin.accounts.openai.excelBPSRecoveryPaused')
        lines.push(pausedReasonLabel(recovery.reason))
        break
      case 'pending':
      case 'failed':
      case 'probing':
        label = recovery.state === 'pending'
          ? t('admin.accounts.openai.excelBPSRecoveryPending')
          : recovery.state === 'failed'
            ? t('admin.accounts.openai.excelBPSRecoveryFailed')
            : t('admin.accounts.openai.excelBPSRecoveryProbing')
        if (recovery.reason !== null) lines.push(failureReasonLabel(recovery.reason))
        if (recovery.nextProbeAt) {
          const time = formatDateTime(recovery.nextProbeAt)
          lines.push(recovery.nextProbeAt.getTime() <= Date.now()
            ? t('admin.accounts.openai.excelBPSFailureProbeElapsed', { time })
            : t('admin.accounts.openai.excelBPSFailureProbeScheduled', { time }))
        } else {
          lines.push(t('admin.accounts.openai.excelBPSFailureProbeUnavailable'))
        }
        break
      default:
        label = t('admin.accounts.openai.excelBPSFailureUnknown')
    }
    lines.unshift(label)
  }
  const movedAt = parseRfc3339Timestamp(extra?.openai_excel_bps_403_moved_at)
  const target = extra?.openai_excel_bps_403_moved_group_id
  if (movedAt && typeof target === 'number' && Number.isSafeInteger(target) && Array.isArray(groupIDs)) {
    const time = formatDateTime(movedAt)
    if (target === 0 && groupIDs.length === 0) {
      lines.push(t('admin.accounts.openai.excelBPS403BadgeLeftGroups', { time }))
    } else if (target > 0 && groupIDs.length === 1 && groupIDs[0] === target) {
      const group = props.groups?.find(item => item.id === target)?.name ?? `#${target}`
      lines.push(t('admin.accounts.openai.excelBPS403BadgeMoved', { time, group }))
    }
  }
  const result = recovery?.lastResult
  if (result) {
    if (!lines.length) {
      label = t('admin.accounts.openai.excelBPS403State_history')
      historical = true
    }
    const currentCycle = extra?.openai_excel_bps !== true && recovery?.state !== 'history' && recovery?.lastResultCurrentCycle
    lines.push(t(currentCycle ? 'admin.accounts.openai.excelBPS403CurrentCycleResult' : 'admin.accounts.openai.excelBPS403HistoricalResult'))
    if (result.attemptedAt) lines.push(t('admin.accounts.openai.excelBPS403LastAttempt', { time: formatDateTime(result.attemptedAt) }))
    if (result.errorCode || result.errorType || result.upstreamRequestId) {
      lines.push(t('admin.accounts.openai.excelBPS403UpstreamIdentifiers', {
        code: result.errorCode ?? '—', type: result.errorType ?? '—', requestId: result.upstreamRequestId ?? '—'
      }))
    }
    lines.push(t('admin.accounts.openai.excelBPS403ProbeResult', {
      time: result.completedAt ? formatDateTime(result.completedAt) : '—',
      reason: t(probeResultReasonKeys.get(result.reason ?? '') ?? 'admin.accounts.openai.excelBPSFailureReasonUnknown'),
      stage: result.stage ?? '—', status: result.httpStatus ?? '—', duration: result.durationMs ?? '—'
    }))
  }
  if (!lines.length) return null
  return { label, historical, title: [note, ...lines.map(line => `· ${line}`)].join('\n') }
})
</script>
