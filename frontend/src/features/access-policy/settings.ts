import { defineAsyncComponent } from 'vue'
export const BetaPolicySettingsPanel = defineAsyncComponent(() => import('./settings/BetaPolicySettingsPanel.vue'))
export const FastPolicySettingsPanel = defineAsyncComponent(() => import('./settings/FastPolicySettingsPanel.vue'))
export { projectFastPolicy, fastPolicyFields } from './settings/fastPolicyModel'
export type { FastPolicyDraft } from './settings/fastPolicyModel'
export const ClientIpSettingsPanel = defineAsyncComponent(() => import('./settings/ClientIpSettingsPanel.vue'))
export { projectClientIp, clientIpFields } from './settings/clientIpModel'
export type { ClientIpDraft } from './settings/clientIpModel'
export const PanelRateLimitSettingsPanel = defineAsyncComponent(() => import('./settings/PanelRateLimitSettingsPanel.vue'))
export const CodexClientsSettingsPanel = defineAsyncComponent(() => import('./settings/CodexClientsSettingsPanel.vue'))
export { projectCodexClients, codexClientsFields } from './settings/codexClientsModel'
export type { CodexClientsDraft } from './settings/codexClientsModel'
export const RiskControlSettingsPanel = defineAsyncComponent(() => import('./settings/RiskControlSettingsPanel.vue'))
export { projectRiskControl, riskControlFields } from './settings/riskControlModel'
export type { RiskControlDraft } from './settings/riskControlModel'
export { accesspolicySettingsServicesKey } from './settings/ports'
export type { AccessPolicySettingsServices } from './settings/ports'
export const DefaultQuotaSettingsPanel = defineAsyncComponent(() => import('./settings/DefaultQuotaSettingsPanel.vue'))
export { projectDefaultQuota } from './settings/defaultQuotaModel'
export { prepareAccessPolicySettings } from './settings/preparePatch'
export { normalizePlatformQuotasMap, sanitizePlatformQuotasMap } from './settings/platformQuotas'
