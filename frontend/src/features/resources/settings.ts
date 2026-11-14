import { defineAsyncComponent } from 'vue'
export const OverloadCooldownSettingsPanel = defineAsyncComponent(() => import('./settings/OverloadCooldownSettingsPanel.vue'))
export const RateLimitCooldownSettingsPanel = defineAsyncComponent(() => import('./settings/RateLimitCooldownSettingsPanel.vue'))
export const StreamTimeoutSettingsPanel = defineAsyncComponent(() => import('./settings/StreamTimeoutSettingsPanel.vue'))
export const RectifierSettingsPanel = defineAsyncComponent(() => import('./settings/RectifierSettingsPanel.vue'))
export const ClaudeVersionSettingsPanel = defineAsyncComponent(() => import('./settings/ClaudeVersionSettingsPanel.vue'))
export { projectClaudeVersion, claudeVersionFields } from './settings/claudeVersionModel'
export type { ClaudeVersionDraft } from './settings/claudeVersionModel'
export const BillingProbeSettingsPanel = defineAsyncComponent(() => import('./settings/BillingProbeSettingsPanel.vue'))
export const OllamaUsageSettingsPanel = defineAsyncComponent(() => import('./settings/OllamaUsageSettingsPanel.vue'))
export const OpenCodeUsageSettingsPanel = defineAsyncComponent(() => import('./settings/OpenCodeUsageSettingsPanel.vue'))
export const SchedulingSettingsPanel = defineAsyncComponent(() => import('./settings/SchedulingSettingsPanel.vue'))
export { projectScheduling, schedulingFields } from './settings/schedulingModel'
export type { SchedulingDraft } from './settings/schedulingModel'
export const WebSearchSettingsPanel = defineAsyncComponent(() => import('./settings/WebSearchSettingsPanel.vue'))
export const BpsImagesSettingsPanel = defineAsyncComponent(() => import('./settings/BpsImagesSettingsPanel.vue'))
export { projectBpsImages, bpsImagesFields } from './settings/bpsImagesModel'
export type { BpsImagesDraft } from './settings/bpsImagesModel'
export const QuotaNotificationSettingsPanel = defineAsyncComponent(() => import('./settings/QuotaNotificationSettingsPanel.vue'))
export { projectQuotaNotification, quotaNotificationFields } from './settings/quotaNotificationModel'
export type { QuotaNotificationDraft } from './settings/quotaNotificationModel'
export { resourcesSettingsServicesKey } from './settings/ports'
export type { ResourcesSettingsServices } from './settings/ports'
export { prepareResourceSettings } from './settings/preparePatch'
export { SCHEDULING_THRESHOLD_PLATFORMS, SCHEDULING_THRESHOLD_SCOPES, normalizeAccountSchedulingThresholdsMap, sanitizeAccountSchedulingThresholdsMap } from './settings/schedulingThresholds'
