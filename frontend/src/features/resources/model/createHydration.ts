import type { CreateResourceForm } from './createEditorTypes'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { useAccountOAuth, type AddMethod, type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import { useOpenAIOAuth } from '@/features/resources/queries/useOpenAIOAuth'
import { useGeminiOAuth } from '@/features/resources/queries/useGeminiOAuth'
import { useAntigravityOAuth } from '@/features/resources/queries/useAntigravityOAuth'
import { useGrokOAuth } from '@/features/resources/queries/useGrokOAuth'
import type { OpenAICompactMode, OpenAIResponsesMode, OpenAIEndpointCapability } from '@/features/resources/api/accountCredentialTypes';
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';
import { type CnApiProtocol, type CnNativeApiProtocol, type HeaderOverrideRow, type OpenCodeAccountMode, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import { OPENAI_WS_MODE_OFF, type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref } from 'vue'
import type { OAuthFlowExposed, ModelMapping, TempUnschedRuleForm, CodexFingerprintMode, AnthropicAPIKeyAuthScheme } from './createEditorTypes'




interface CreateHydrationInput {
  step: Ref<number, number>
  form: Pick<CreateResourceForm, "name" | "notes" | "platform" | "type" | "credentials" | "proxy_id" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  accountCategory: Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">
  addMethod: Ref<AddMethod, AddMethod>
  accountMode: Ref<string, string>
  openCodeAccountMode: Ref<OpenCodeAccountMode, OpenCodeAccountMode>
  apiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  openCodeGoProtocolRules: Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>
  cloneOpenCodeGoProtocolRules: ReturnType<typeof createProviderCredentialOptions>["cloneOpenCodeGoProtocolRules"]
  defaultOpenCodeProtocolRules: ReturnType<typeof createProviderCredentialOptions>["defaultOpenCodeProtocolRules"]
  adaptiveBaseUrls: Ref<{ anthropic: string; chat_completions: string; responses: string }, Record<CnNativeApiProtocol, string> | { anthropic: string; chat_completions: string; responses: string }>
  apiKeyBaseUrl: Ref<string, string>
  apiKeyValue: Ref<string, string>
  upstreamRequestIdHeader: Ref<string, string>
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
  editQuotaLimit: Ref<number | null, number | null>
  editQuotaDailyLimit: Ref<number | null, number | null>
  editQuotaWeeklyLimit: Ref<number | null, number | null>
  editDailyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editDailyResetHour: Ref<number | null, number | null>
  editWeeklyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editWeeklyResetDay: Ref<number | null, number | null>
  editWeeklyResetHour: Ref<number | null, number | null>
  editResetTimezone: Ref<string | null, string | null>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  openAICompactModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  allowedModels: Ref<string[], string[]>
  antigravityModelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  antigravityWhitelistModels: Ref<string[], string[]>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  poolModeEnabled: Ref<boolean, boolean>
  poolModeRetryCount: Ref<number, number>
  DEFAULT_POOL_MODE_RETRY_COUNT: 3
  poolModeRetryStatusCodesInput: Ref<string, string>
  explicitCacheUpstreamEnabled: Ref<boolean, boolean>
  explicitCacheUpstreamMaxTTL: Ref<string | number, string | number>
  customErrorCodesEnabled: Ref<boolean, boolean>
  selectedErrorCodes: Ref<number[], number[]>
  customErrorCodeInput: Ref<number | null, number | null>
  headerOverrideEnabled: Ref<boolean, boolean>
  headerOverrideRows: Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>
  openAIImagesUrlToB64JsonEnabled: Ref<boolean, boolean>
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  interceptWarmupRequests: Ref<boolean, boolean>
  autoPauseOnExpired: Ref<boolean, boolean>
  openaiPassthroughEnabled: Ref<boolean, boolean>
  openaiFlattenNamespacesEnabled: Ref<boolean, boolean>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
  openAILongContextBillingTouched: Ref<boolean, boolean>
  openAICompactMode: Ref<OpenAICompactMode, OpenAICompactMode>
  openAIResponsesMode: Ref<OpenAIResponsesMode, OpenAIResponsesMode>
  openAIEndpointCapabilities: Ref<OpenAIEndpointCapability[], OpenAIEndpointCapability[]>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  openaiAPIKeyResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  codexCLIOnlyEnabled: Ref<boolean, boolean>
  codexCLIOnlyAppServerEnabled: Ref<boolean, boolean>
  codexFingerprintMode: Ref<CodexFingerprintMode, CodexFingerprintMode>
  anthropicPassthroughEnabled: Ref<boolean, boolean>
  anthropicAPIKeyAuthScheme: Ref<AnthropicAPIKeyAuthScheme, AnthropicAPIKeyAuthScheme>
  webSearchEmulationMode: Ref<string, string>
  windowCostEnabled: Ref<boolean, boolean>
  windowCostLimit: Ref<number | null, number | null>
  windowCostStickyReserve: Ref<number | null, number | null>
  sessionLimitEnabled: Ref<boolean, boolean>
  maxSessions: Ref<number | null, number | null>
  sessionIdleTimeout: Ref<number | null, number | null>
  rpmLimitEnabled: Ref<boolean, boolean>
  baseRpm: Ref<number | null, number | null>
  rpmStrategy: Ref<"tiered" | "sticky_exempt", "tiered" | "sticky_exempt">
  rpmStickyBuffer: Ref<number | null, number | null>
  userMsgQueueMode: Ref<string, string>
  tlsFingerprintEnabled: Ref<boolean, boolean>
  tlsFingerprintProfileId: Ref<number | null, number | null>
  sessionIdMaskingEnabled: Ref<boolean, boolean>
  cacheTTLOverrideEnabled: Ref<boolean, boolean>
  cacheTTLOverrideTarget: Ref<string, string>
  customBaseUrlEnabled: Ref<boolean, boolean>
  customBaseUrl: Ref<string, string>
  allowOverages: Ref<boolean, boolean>
  antigravityAccountType: Ref<"oauth" | "upstream", "oauth" | "upstream">
  antigravityProjectId: Ref<string, string>
  upstreamBaseUrl: Ref<string, string>
  upstreamApiKey: Ref<string, string>
  vertexServiceAccountJson: Ref<string, string>
  vertexProjectId: Ref<string, string>
  vertexClientEmail: Ref<string, string>
  vertexLocation: Ref<string, string>
  vertexAuthMode: Ref<"apikey" | "service_account", "apikey" | "service_account">
  vertexApiKey: Ref<string, string>
  tempUnschedEnabled: Ref<boolean, boolean>
  tempUnschedRules: Ref<{ error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[], TempUnschedRuleForm[] | { error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[]>
  geminiOAuthType: Ref<"code_assist" | "google_one" | "ai_studio", "code_assist" | "google_one" | "ai_studio">
  geminiTierGoogleOne: Ref<"google_one_free" | "google_ai_pro" | "google_ai_ultra", "google_one_free" | "google_ai_pro" | "google_ai_ultra">
  geminiTierGcp: Ref<"gcp_standard" | "gcp_enterprise", "gcp_standard" | "gcp_enterprise">
  geminiTierAIStudio: Ref<"aistudio_free" | "aistudio_paid", "aistudio_free" | "aistudio_paid">
  oauth: ReturnType<typeof useAccountOAuth>
  openaiOAuth: ReturnType<typeof useOpenAIOAuth>
  geminiOAuth: ReturnType<typeof useGeminiOAuth>
  antigravityOAuth: ReturnType<typeof useAntigravityOAuth>
  grokOAuth: ReturnType<typeof useGrokOAuth>
  oauthFlowRef: Ref<{ authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null, OAuthFlowExposed | { authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null>
  antigravityMixedChannelConfirmed: Ref<boolean, boolean>
  clearMixedChannelDialog: () => void
}




export function useCreateHydrationDraft(input: CreateHydrationInput): {
  resetForm: () => void
} {


  // Methods
  const resetForm = () => {
    input.step.value = 1
    input.form.name = ''
    input.form.notes = ''
    input.form.platform = 'anthropic'
    input.form.type = 'oauth'
    input.form.credentials = {}
    input.form.proxy_id = null
    input.form.concurrency = 10
    input.form.load_factor = null
    input.form.priority = 1
    input.form.rate_multiplier = 1
    input.form.group_ids = []
    input.form.expires_at = null
    input.accountCategory.value = 'oauth-based'
    input.addMethod.value = 'oauth'
    input.accountMode.value = 'payg'
    input.openCodeAccountMode.value = 'zen'
    input.apiProtocol.value = 'adaptive'
    input.openCodeGoProtocolRules.value = input.cloneOpenCodeGoProtocolRules(input.defaultOpenCodeProtocolRules('zen'))
    input.adaptiveBaseUrls.value = { chat_completions: '', anthropic: '', responses: '' }
    input.apiKeyBaseUrl.value = 'https://api.anthropic.com'
    input.apiKeyValue.value = ''
    input.upstreamRequestIdHeader.value = ''
    input.upstreamBillingAutoProbeEnabled.value = true
    input.editQuotaLimit.value = null
    input.editQuotaDailyLimit.value = null
    input.editQuotaWeeklyLimit.value = null
    input.editDailyResetMode.value = null
    input.editDailyResetHour.value = null
    input.editWeeklyResetMode.value = null
    input.editWeeklyResetDay.value = null
    input.editWeeklyResetHour.value = null
    input.editResetTimezone.value = null
    input.modelMappings.value = []
    input.openAICompactModelMappings.value = []
    input.modelRestrictionMode.value = 'whitelist'
    input.allowedModels.value = []

    input.antigravityModelRestrictionMode.value = 'mapping'
    input.antigravityWhitelistModels.value = []
    input.antigravityModelMappings.value = []
    input.poolModeEnabled.value = false
    input.poolModeRetryCount.value = input.DEFAULT_POOL_MODE_RETRY_COUNT
    input.poolModeRetryStatusCodesInput.value = ''
    input.explicitCacheUpstreamEnabled.value = false
    input.explicitCacheUpstreamMaxTTL.value = ''
    input.customErrorCodesEnabled.value = false
    input.selectedErrorCodes.value = []
    input.customErrorCodeInput.value = null
    input.headerOverrideEnabled.value = false
    input.headerOverrideRows.value = []
    input.openAIImagesUrlToB64JsonEnabled.value = false
    input.grokOAuthCustomBaseUrlEnabled.value = false
    input.grokOAuthBaseUrl.value = ''
    input.interceptWarmupRequests.value = false
    input.autoPauseOnExpired.value = true
    input.openaiPassthroughEnabled.value = false
    input.openaiFlattenNamespacesEnabled.value = false
    input.openAILongContextBillingEnabled.value = false
    input.openAILongContextBillingTouched.value = false
    input.openAICompactMode.value = 'auto'
    input.openAIResponsesMode.value = 'auto'
    input.openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
    input.openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
    input.openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
    input.codexCLIOnlyEnabled.value = false
    input.codexCLIOnlyAppServerEnabled.value = false
    input.codexFingerprintMode.value = 'off'
    input.anthropicPassthroughEnabled.value = false
    input.anthropicAPIKeyAuthScheme.value = 'x_api_key'
    input.webSearchEmulationMode.value = 'default'
    // Reset quota control state
    input.windowCostEnabled.value = false
    input.windowCostLimit.value = null
    input.windowCostStickyReserve.value = null
    input.sessionLimitEnabled.value = false
    input.maxSessions.value = null
    input.sessionIdleTimeout.value = null
    input.rpmLimitEnabled.value = false
    input.baseRpm.value = null
    input.rpmStrategy.value = 'tiered'
    input.rpmStickyBuffer.value = null
    input.userMsgQueueMode.value = ''
    input.tlsFingerprintEnabled.value = false
    input.tlsFingerprintProfileId.value = null
    input.sessionIdMaskingEnabled.value = false
    input.cacheTTLOverrideEnabled.value = false
    input.cacheTTLOverrideTarget.value = '5m'
    input.customBaseUrlEnabled.value = false
    input.customBaseUrl.value = ''
    input.allowOverages.value = false
    input.antigravityAccountType.value = 'oauth'
    input.antigravityProjectId.value = ''
    input.upstreamBaseUrl.value = ''
    input.upstreamApiKey.value = ''
    input.vertexServiceAccountJson.value = ''
    input.vertexProjectId.value = ''
    input.vertexClientEmail.value = ''
    input.vertexLocation.value = 'global'
    input.vertexAuthMode.value = 'service_account'
    input.vertexApiKey.value = ''
    input.tempUnschedEnabled.value = false
    input.tempUnschedRules.value = []
    input.geminiOAuthType.value = 'code_assist'
    input.geminiTierGoogleOne.value = 'google_one_free'
    input.geminiTierGcp.value = 'gcp_standard'
    input.geminiTierAIStudio.value = 'aistudio_free'
    input.oauth.resetState()
    input.openaiOAuth.resetState()
    input.geminiOAuth.resetState()
    input.antigravityOAuth.resetState()
    input.grokOAuth.resetState()
    input.oauthFlowRef.value?.reset()
    input.antigravityMixedChannelConfirmed.value = false
    input.clearMixedChannelDialog()
  }
  return { resetForm }
}
