import type { UpdateSettingsRequest, SettingsDraft } from '@/features/system/api'
import { sanitizeAccountSchedulingThresholdsMap } from './schedulingThresholds'
import { excelBPSImageLimits as limits } from '@/features/resources/model/excelBPSImageLimits'
import { parseClaudePromptBlocks, serializeClaudePromptBlocks, claudePromptValidationError } from '@/features/resources/model/claudePrompt'
import { projectBpsImages } from './bpsImagesModel'
import type { BpsImagesDraft } from './bpsImagesModel'
import type { SchedulingDraft } from './schedulingModel'
type ResourcePatch = Partial<BpsImagesDraft & SchedulingDraft>
export function prepareResourceSettings(patch: ResourcePatch, draft: SettingsDraft): Partial<UpdateSettingsRequest> {
  const result: Partial<UpdateSettingsRequest> = {}
  if ('openai_oauth_scheduling_rate_multiplier' in patch) {
    const value = draft.openai_oauth_scheduling_rate_multiplier
    if (value !== '' && value !== null && (!Number.isFinite(value) || value < 0)) throw { field: 'openai_oauth_scheduling_rate_multiplier', messageKey: 'admin.settings.openaiScheduler.oauthRateInvalid' }
    result.openai_oauth_scheduling_rate_multiplier = value === '' ? null : value
  }
  if ('account_scheduling_thresholds' in patch) result.account_scheduling_thresholds = sanitizeAccountSchedulingThresholdsMap(patch.account_scheduling_thresholds)
  if ('claude_oauth_system_prompt_blocks' in patch && patch.claude_oauth_system_prompt_blocks?.trim()) {
    try {
      const blocks = parseClaudePromptBlocks(patch.claude_oauth_system_prompt_blocks)
      if (claudePromptValidationError(blocks)) throw new Error('Invalid prompt blocks')
      result.claude_oauth_system_prompt_blocks = serializeClaudePromptBlocks(blocks)
    } catch { throw { field: 'claude_oauth_system_prompt_blocks', messageKey: 'admin.settings.gatewayForwarding.claudePromptInvalid' } }
  }
  if (Object.keys(patch).some(key => key.startsWith('excel_bps_image_'))) {
    const value = projectBpsImages(draft)
    const origin = value.excel_bps_image_base_url.trim()
    if ((value.excel_bps_image_relay_enabled && value.excel_bps_image_mode === 'relay') || origin) {
      try {
        const url = new URL(origin)
        if (url.protocol !== 'https:' || !url.hostname || url.username || url.password || (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash) throw new Error('Invalid image origin')
        if ('excel_bps_image_base_url' in patch) result.excel_bps_image_base_url = url.origin
      } catch { throw { field: 'excel_bps_image_base_url', messageKey: 'admin.settings.features.excelBpsImages.invalidBaseUrl' } }
    }
    const integer = (number: number, min: number, max: number) => Number.isInteger(number) && number >= min && number <= max
    if (!integer(value.excel_bps_image_body_limit_mib, 1, limits.bodyMiB) || !integer(value.excel_bps_image_budget_mib, limits.minBudgetMiB, limits.budgetMiB) || value.excel_bps_image_budget_mib < value.excel_bps_image_body_limit_mib * limits.bodyMultiplier || !integer(value.excel_bps_image_max_requests, 1, limits.requests)) throw { field: 'excel_bps_image_budget_mib', messageKey: 'admin.settings.features.excelBpsImages.invalidCapacity', params: limits }
    if (!integer(value.excel_bps_image_max_image_mib, 1, limits.imageMiB) || !integer(value.excel_bps_image_max_images, 1, limits.images) || !integer(value.excel_bps_image_max_total_mib, 1, limits.totalMiB) || !integer(value.excel_bps_image_storage_mib, 1, limits.storageMiB) || !integer(value.excel_bps_image_storage_entries, 1, limits.storageEntries) || !integer(value.excel_bps_image_ttl_minutes, 1, limits.ttlMinutes) || !integer(value.excel_bps_image_warning_remaining, 1, limits.images) || !integer(value.excel_bps_image_compact_reserve, 1, limits.images) || (value.excel_bps_image_limit_policy === 'warn' && (value.excel_bps_image_compact_reserve >= value.excel_bps_image_warning_remaining || value.excel_bps_image_warning_remaining >= value.excel_bps_image_max_images)) || (value.excel_bps_image_mode === 'relay' && (value.excel_bps_image_max_total_mib < value.excel_bps_image_max_image_mib || value.excel_bps_image_storage_mib < value.excel_bps_image_max_total_mib || value.excel_bps_image_storage_entries < value.excel_bps_image_max_images))) throw { field: 'excel_bps_image_max_images', messageKey: 'admin.settings.features.excelBpsImages.invalidLimits' }
  }
  return result
}
