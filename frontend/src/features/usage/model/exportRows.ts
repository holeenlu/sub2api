import { knownUsageCost, accountUsageCost } from './usageNumbers'
import type { Composer } from 'vue-i18n'
import type { UsageLog, AdminUsageLog } from '../api/types'
import type { UsagePermissions } from '../services'
import { formatReasoningEffort } from '@/shared/lib/format'
import { resolveUsageRequestType } from '@/features/usage/model/usageRequestType'
import { getBillingModeLabel, getDisplayBillingMode } from '@/features/usage/model/billingMode'
import { escapeUsageCSV as escapeCSVValue } from '../api/usageExport'
import { getUsageCacheHitRate, usageLatencyExportKeys, usageLatencyExportValues } from './metrics'

export function exportCost(value: number | null | undefined, status: UsageLog['billing_status'], digits: number): string {
  return knownUsageCost(value, status) ? value.toFixed(digits) : ''
}
function exportAccountCost(log: AdminUsageLog): string {
  return exportCost(accountUsageCost(log), log.billing_status, 6)
}
function getRequestTypeExportText(log: UsageLog): string {
  const type = resolveUsageRequestType(log)
  if (type === 'cyber') return 'Cyber'
  if (type === 'live') return 'Live'
  if (type === 'ws_v2') return 'WS'
  if (type === 'stream') return 'Stream'
  if (type === 'sync') return 'Sync'
  return 'Unknown'
}
export function userUsageExportHeaders(t: Composer['t']): string[] {
    return [
      t('usage.time'),
      t('usage.apiKeyFilter'),
      t('usage.model'),
      t('usage.reasoningEffort'),
      t('usage.inboundEndpoint'),
      t('admin.usage.ipAddress'),
      t('usage.type'),
      t('admin.usage.billingMode'),
      t('admin.usage.inputTokens'),
      t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'),
      t('admin.usage.cacheCreationTokens'),
      `${t('usage.cacheHitRate')} (%)`,
      t('usage.rate'),
      t('usage.userBilled'),
      t('usage.original'),
      ...usageLatencyExportKeys.map(key => t(key)),
      t('usage.billingStatusLabel'),
      t('usage.samplingSteps'), t('usage.samplingExport'),
    ]
}
export function userUsageExportRows(logs: UsageLog[], t: Composer['t']): string[][] {
      return logs.map((log) => [
        log.created_at,
        log.api_key?.name || '',
        log.model,
        formatReasoningEffort(log.reasoning_effort),
        log.inbound_endpoint || '',
        log.ip_address || '',
        getRequestTypeExportText(log),
        getBillingModeLabel(getDisplayBillingMode(log), t),
        log.input_tokens,
        log.output_tokens,
        log.cache_read_tokens,
        log.cache_creation_tokens,
        getUsageCacheHitRate(log)?.toFixed(2) ?? '',
        log.rate_multiplier,
        exportCost(log.actual_cost, log.billing_status, 8),
        exportCost(log.total_cost, log.billing_status, 8),
        ...usageLatencyExportValues(log),
        log.billing_status || 'unknown',
        log.sampling_details?.samples.length ?? '', log.sampling_details ? JSON.stringify(log.sampling_details) : '',
      ].map(escapeCSVValue))
}
export function adminUsageExportHeaders(t: Composer['t'], permissions: UsagePermissions): string[] {
    return [
      t('usage.time'), t('admin.usage.user'), t('usage.apiKeyFilter'),
      ...(permissions.isSuperAdmin ? [t('admin.usage.account')] : []), t('usage.requestedModel'), t('usage.sentUpstreamModel'), t('usage.upstreamResponseModel'), t('usage.upstreamModelMismatch'), t('usage.requestedReasoningEffort'), t('usage.reasoningEffort'), t('admin.usage.group'),
      t('usage.inboundEndpoint'), t('usage.upstreamEndpoint'),
      t('usage.type'),
      t('admin.usage.inputTokens'), t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'), t('admin.usage.cacheCreationTokens'), `${t('usage.cacheHitRate')} (%)`,
      t('admin.usage.inputCost'), t('admin.usage.outputCost'),
      t('admin.usage.cacheReadCost'), t('admin.usage.cacheCreationCost'),
      t('usage.rate'), ...(permissions.canReadBillingCost ? [t('usage.accountMultiplier')] : []), t('usage.original'), t('usage.userBilled'), ...(permissions.canReadBillingCost ? [t('usage.accountBilled')] : []),
      ...usageLatencyExportKeys.map(key => t(key)),
      t('admin.usage.requestId'), t('admin.usage.upstreamRequestId'), t('usage.userAgent'), t('admin.usage.ipAddress'), t('usage.billingStatusLabel'), t('usage.samplingSteps'), t('usage.samplingExport')
    ]
}
export function adminUsageExportRows(items: AdminUsageLog[], t: Composer['t'], permissions: UsagePermissions): unknown[][] {
  const getRequestTypeLabel = (row: UsageLog): string => {
    const type = resolveUsageRequestType(row)
    return t('usage.' + (type === 'ws_v2' ? 'ws' : type))
  }
      return items.map((log: AdminUsageLog) => [
        log.created_at, log.user?.email || '', log.api_key?.name || '', ...(permissions.isSuperAdmin ? [log.account?.name || ''] : []), log.model,
        log.upstream_model || log.model, log.upstream_response_model || '', log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'), formatReasoningEffort(log.reasoning_effort), formatReasoningEffort(log.upstream_reasoning_effort || log.reasoning_effort), log.group?.name || '',
        log.inbound_endpoint || '', log.upstream_endpoint || '', getRequestTypeLabel(log),
        log.input_tokens, log.output_tokens, log.cache_read_tokens, log.cache_creation_tokens, getUsageCacheHitRate(log)?.toFixed(2) ?? '',
        exportCost(log.input_cost, log.billing_status, 6), exportCost(log.output_cost, log.billing_status, 6),
        exportCost(log.cache_read_cost, log.billing_status, 6), exportCost(log.cache_creation_cost, log.billing_status, 6),
        log.rate_multiplier?.toPrecision(4) || '1.00', ...(permissions.canReadBillingCost ? [(log.account_rate_multiplier ?? 1).toPrecision(4)] : []),
        exportCost(log.total_cost, log.billing_status, 6), exportCost(log.actual_cost, log.billing_status, 6),
        ...(permissions.canReadBillingCost ? [exportAccountCost(log)] : []), ...usageLatencyExportValues(log),
        log.request_id || '', log.upstream_request_id || '', log.user_agent || '', log.ip_address || '', log.billing_status || 'unknown', log.sampling_details?.samples.length ?? '', log.sampling_details ? JSON.stringify(log.sampling_details) : ''
      ])
}
