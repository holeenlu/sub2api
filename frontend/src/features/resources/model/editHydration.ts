import type { EditResourceForm } from './editEditorTypes'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, nextTick } from 'vue'
import { DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES, bpsRecoveryIntervalOrDefault } from '@/features/resources/model/excelBPSRecovery'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import type { Account } from '@/features/resources/api/accountTypes';
import type { OpenAICompactMode, OpenAIResponsesMode, OpenAIEndpointCapability, GrokMediaEligibilityMode, GrokMediaEligibilityState } from '@/features/resources/api/accountCredentialTypes';
import { normalizeExplicitCacheUpstreamMaxTTL } from '@/features/resources/model/geminiExplicitCache'
import { readPlanType, resolveOpenCodeAccountMode, isCustomGrokBaseUrl, splitHeaderOverridesObject, isCNProviderPlatform, HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY, HEADER_OVERRIDES_CREDENTIAL_KEY, type CnApiProtocol, type CnNativeApiProtocol, type HeaderOverrideRow, type OpenCodeAccountMode, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import { supportsAccountQuota } from '@/features/resources/model/accountQuota'
import { readExcelBPSModels } from './excelBPSModels'
import { OPENAI_WS_MODE_OFF, type OpenAIWSMode, resolveOpenAIWSModeFromExtra } from '@/features/resources/model/openaiWsMode'
import type { Ref, ComputedRef } from 'vue'
import type { ModelMapping, CodexFingerprintMode, CodexImageToolMode, AnthropicAPIKeyAuthScheme } from './editEditorTypes'




interface EditHydrationInput {
  antigravityMixedChannelConfirmed: Ref<boolean, boolean>
  showMixedChannelWarning: Ref<boolean, boolean>
  mixedChannelWarningDetails: Ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null, { groupName: string; currentPlatform: string; otherPlatform: string } | { groupName: string; currentPlatform: string; otherPlatform: string } | null>
  mixedChannelWarningRawMessage: Ref<string, string>
  mixedChannelWarningAction: Ref<(() => Promise<void>) | null, (() => Promise<void>) | null>
  form: Pick<EditResourceForm, "name" | "notes" | "proxy_id" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "status" | "group_ids" | "expires_at">
  interceptWarmupRequests: Ref<boolean, boolean>
  autoPauseOnExpired: Ref<boolean, boolean>
  editVertexProjectId: Ref<string, string>
  editVertexClientEmail: Ref<string, string>
  editVertexLocation: Ref<string, string>
  editVertexApiKey: Ref<string, string>
  antigravityProjectId: Ref<string, string>
  mixedScheduling: Ref<boolean, boolean>
  allowOverages: Ref<boolean, boolean>
  upstreamRequestIdHeader: Ref<string, string>
  readUpstreamRequestIdHeader: (extra: unknown) => string
  openAIImagesUrlToB64JsonEnabled: Ref<boolean, boolean>
  autoPause5hThreshold: Ref<number | null, number | null>
  autoPause7dThreshold: Ref<number | null, number | null>
  autoPause5hDisabled: Ref<boolean, boolean>
  autoPause7dDisabled: Ref<boolean, boolean>
  autoResetCreditEnabled: Ref<boolean, boolean>
  autoResetCredit5hThreshold: Ref<number, number>
  autoResetCredit7dThreshold: Ref<number, number>
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
  upstreamBillingRateSyncEnabled: Ref<boolean, boolean>
  cloudEnabled: Ref<boolean, boolean>
  cloudEligible: ComputedRef<boolean>
  excelBPSEnabled: Ref<boolean, boolean>
  excelBPSModels: Ref<string[], string[]>
  excelBPSCacheCreationAsInput: Ref<boolean, boolean>
  excelBPSAutoDisableOn403: Ref<boolean, boolean>
  excelBPSAutoRecoverOn403: Ref<boolean, boolean>
  excelBPSRecoveryIntervalMinutes: Ref<string | number, string | number>
  excelBPSOmitUnsupportedTools: Ref<boolean, boolean>
  excelBPSIgnoreEncryptedContent: Ref<boolean, boolean>
  excelBPSAutoMoveOn403: Ref<boolean, boolean>
  excelBPS403TargetGroupID: Ref<string | number, string | number>
  openaiPassthroughEnabled: Ref<boolean, boolean>
  openaiFlattenNamespacesEnabled: Ref<boolean, boolean>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
  editPlanType: Ref<string, string>
  openAICompactMode: Ref<OpenAICompactMode, OpenAICompactMode>
  openAIResponsesMode: Ref<OpenAIResponsesMode, OpenAIResponsesMode>
  openAIEndpointCapabilities: Ref<OpenAIEndpointCapability[], OpenAIEndpointCapability[]>
  openAICompactModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  openaiAPIKeyResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  codexCLIOnlyEnabled: Ref<boolean, boolean>
  codexCLIOnlyAppServerEnabled: Ref<boolean, boolean>
  codexFingerprintMode: Ref<CodexFingerprintMode, CodexFingerprintMode>
  codexImageToolMode: Ref<CodexImageToolMode, CodexImageToolMode>
  anthropicPassthroughEnabled: Ref<boolean, boolean>
  anthropicAPIKeyAuthScheme: Ref<AnthropicAPIKeyAuthScheme, AnthropicAPIKeyAuthScheme>
  webSearchEmulationMode: Ref<string, string>
  normalizeOpenAIResponsesMode: (mode: unknown) => OpenAIResponsesMode
  readOpenAIEndpointCapabilities: (credentials?: Record<string, unknown>) => OpenAIEndpointCapability[]
  openAITextGenerationCapabilityEnabled: ComputedRef<boolean>
  editQuotaLimit: Ref<number | null, number | null>
  editQuotaDailyLimit: Ref<number | null, number | null>
  editQuotaWeeklyLimit: Ref<number | null, number | null>
  editDailyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editDailyResetHour: Ref<number | null, number | null>
  editWeeklyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editWeeklyResetDay: Ref<number | null, number | null>
  editWeeklyResetHour: Ref<number | null, number | null>
  editResetTimezone: Ref<string | null, string | null>
  loadQuotaNotifyFromExtra: ReturnType<typeof useQuotaNotifyState>["loadFromExtra"]
  resetQuotaNotify: ReturnType<typeof useQuotaNotifyState>["reset"]
  antigravityModelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  antigravityWhitelistModels: Ref<string[], string[]>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  loadQuotaControlSettings: (account: Account) => void
  loadTempUnschedRules: (credentials?: Record<string, unknown>) => void
  loadAccountSchedulingThresholdOverride: (platform: Account["platform"] | undefined, type: Account["type"] | undefined, credentials: Record<string, unknown> | undefined) => void
  headerOverrideEnabled: Ref<boolean, boolean>
  headerOverrideRows: Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>
  isHeaderOverrideCapable: ReturnType<typeof createProviderCredentialOptions>["isHeaderOverrideCapable"]
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  GROK_CLIENT_TOOL_CACHE_EXTRA_KEY: "grok_client_tool_cache_enabled"
  grokClientToolCacheEnabled: Ref<boolean, boolean>
  grokMediaEligibilityMode: Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>
  modeFromGrokMediaExtra: (extra: Record<string, unknown> | undefined) => GrokMediaEligibilityMode
  grokMediaEligibilityInitialMode: Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>
  grokMediaEligibilityState: Ref<{ account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null, GrokMediaEligibilityState | { account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null>
  grokMediaEligibilityError: Ref<string, string>
  loadGrokMediaEligibility: (accountID: number) => Promise<GrokMediaEligibilityState | null>
  grokMediaEligibilityRequestVersion: Ref<number>
  isMultiProtocolApiKeyPlatform: ReturnType<typeof createProviderCredentialOptions>["isMultiProtocolApiKeyPlatform"]
  editOpenCodeAccountMode: Ref<OpenCodeAccountMode, OpenCodeAccountMode>
  editAccountMode: Ref<string, string>
  resolveProviderAccountMode: ReturnType<typeof createProviderCredentialOptions>["resolveProviderAccountMode"]
  editApiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  cnSupportsNativeResponses: ReturnType<typeof createProviderCredentialOptions>["cnSupportsNativeResponses"]
  currentOpenCodeOrCNMode: () => string
  defaultCNAdaptiveBaseUrls: ReturnType<typeof createProviderCredentialOptions>["defaultCNAdaptiveBaseUrls"]
  editAdaptiveBaseUrls: Ref<{ chat_completions: string; anthropic: string; responses: string }, Record<CnNativeApiProtocol, string> | { chat_completions: string; anthropic: string; responses: string }>
  editZhipuOrganization: Ref<string, string>
  editZhipuProject: Ref<string, string>
  providerRoutesByModel: ReturnType<typeof createProviderCredentialOptions>["providerRoutesByModel"]
  editOpenCodeGoProtocolRules: Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>
  parseOpenCodeGoProtocolRules: ReturnType<typeof createProviderCredentialOptions>["parseOpenCodeGoProtocolRules"]
  cloneOpenCodeGoProtocolRules: ReturnType<typeof createProviderCredentialOptions>["cloneOpenCodeGoProtocolRules"]
  defaultProviderProtocolRules: ReturnType<typeof createProviderCredentialOptions>["defaultProviderProtocolRules"]
  defaultCNBaseUrl: ReturnType<typeof createProviderCredentialOptions>["defaultCNBaseUrl"]
  editBaseUrl: Ref<string, string>
  isCNApiKeyAccount: ComputedRef<boolean>
  loadModelRestrictionFromMapping: (rawMapping?: Record<string, unknown>, allowlist?: unknown) => void
  poolModeEnabled: Ref<boolean, boolean>
  poolModeRetryCount: Ref<number, number>
  normalizePoolModeRetryCount: (value: number) => number
  DEFAULT_POOL_MODE_RETRY_COUNT: 3
  poolModeRetryStatusCodesInput: Ref<string, string>
  formatPoolModeRetryStatusCodes: (value: unknown) => string
  explicitCacheUpstreamEnabled: Ref<boolean, boolean>
  explicitCacheUpstreamMaxTTL: Ref<string | number, string | number>
  customErrorCodesEnabled: Ref<boolean, boolean>
  selectedErrorCodes: Ref<number[], number[]>
  editBedrockRegion: Ref<string, string>
  editBedrockForceGlobal: Ref<boolean, boolean>
  editBedrockApiKeyValue: Ref<string, string>
  editBedrockAccessKeyId: Ref<string, string>
  editBedrockSecretAccessKey: Ref<string, string>
  editBedrockSessionToken: Ref<string, string>
  hasOAuthModelRestriction: ComputedRef<boolean>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  allowedModels: Ref<string[], string[]>
  editApiKey: Ref<string, string>
}




export function useEditHydrationDraft(input: EditHydrationInput): {
  syncingForm: Ref<boolean, boolean>
  syncFormFromAccount: (newAccount: Account | null) => void
} {

  // 回填窗口标志：syncFormFromAccount 会同步改写 editAccountMode / editApiProtocol，
  // 而 watcher（pre-flush）在同步代码执行完之后才触发——若不抑制，会把刚恢复的
  // 存储版 base_url（可能是用户自定义/中转地址）覆盖为官方预设并在下次保存时持久化。
  // nextTick 后解除，此后用户主动切换模式/协议仍正常联动重置。
  const syncingForm = ref(false)


  const syncFormFromAccount = (newAccount: Account | null) => {
    if (!newAccount) {
      return
    }
    // 进入回填窗口：抑制 CN 模式/协议 watcher 联动重置 base_url（见 syncingForm 注释）。
    syncingForm.value = true
    void nextTick(() => {
      syncingForm.value = false
    })
    input.antigravityMixedChannelConfirmed.value = false
    input.showMixedChannelWarning.value = false
    input.mixedChannelWarningDetails.value = null
    input.mixedChannelWarningRawMessage.value = ''
    input.mixedChannelWarningAction.value = null
    input.form.name = newAccount.name
    input.form.notes = newAccount.notes || ''
    input.form.proxy_id = newAccount.proxy_id
    input.form.concurrency = newAccount.concurrency
    input.form.load_factor = newAccount.load_factor ?? null
    input.form.priority = newAccount.priority
    input.form.rate_multiplier = newAccount.rate_multiplier ?? 1
    input.form.status = (newAccount.status === 'active' || newAccount.status === 'inactive' || newAccount.status === 'error')
      ? newAccount.status
      : 'active'
    input.form.group_ids = [...(newAccount.group_ids ?? [])]
    input.form.expires_at = newAccount.expires_at ?? null

    // Load intercept warmup requests setting (applies to all account types)
    const credentials = newAccount.credentials as Record<string, unknown> | undefined
    input.interceptWarmupRequests.value = credentials?.intercept_warmup_requests === true
    input.autoPauseOnExpired.value = newAccount.auto_pause_on_expired === true
    input.editVertexProjectId.value = ''
    input.editVertexClientEmail.value = ''
    input.editVertexLocation.value = 'us-central1'
    input.editVertexApiKey.value = ''
    input.antigravityProjectId.value =
      newAccount.platform === 'antigravity' &&
        newAccount.type === 'oauth' &&
        typeof credentials?.antigravity_project_id === 'string'
        ? credentials.antigravity_project_id.trim()
        : ''

    // Load mixed scheduling setting (only for antigravity accounts)
    input.mixedScheduling.value = false
    input.allowOverages.value = false
    const extra = newAccount.extra as Record<string, unknown> | undefined
    input.mixedScheduling.value = extra?.mixed_scheduling === true
    input.allowOverages.value = extra?.allow_overages === true
    input.upstreamRequestIdHeader.value = input.readUpstreamRequestIdHeader(extra)
    input.openAIImagesUrlToB64JsonEnabled.value = extra?.images_url_to_b64_json === true
    input.autoPause5hThreshold.value = typeof extra?.auto_pause_5h_threshold === 'number' ? extra.auto_pause_5h_threshold * 100 : null
    input.autoPause7dThreshold.value = typeof extra?.auto_pause_7d_threshold === 'number' ? extra.auto_pause_7d_threshold * 100 : null
    input.autoPause5hDisabled.value = extra?.auto_pause_5h_disabled === true
    input.autoPause7dDisabled.value = extra?.auto_pause_7d_disabled === true
    input.autoResetCreditEnabled.value = extra?.auto_reset_credit_enabled === true
    input.autoResetCredit5hThreshold.value =
      typeof extra?.auto_reset_credit_5h_threshold === 'number' ? extra.auto_reset_credit_5h_threshold * 100 : 100
    input.autoResetCredit7dThreshold.value =
      typeof extra?.auto_reset_credit_7d_threshold === 'number' ? extra.auto_reset_credit_7d_threshold * 100 : 100
    input.upstreamBillingAutoProbeEnabled.value = extra?.upstream_billing_probe_enabled === true
    input.upstreamBillingRateSyncEnabled.value =
      input.upstreamBillingAutoProbeEnabled.value && extra?.upstream_billing_rate_sync_enabled === true

    // Load OpenAI passthrough toggle (OpenAI OAuth/SetupToken/API Key)
    input.cloudEnabled.value = input.cloudEligible.value && extra?.openai_cloud === true
    input.excelBPSEnabled.value = false
    input.excelBPSModels.value = []
    input.excelBPSCacheCreationAsInput.value = false
    input.excelBPSAutoDisableOn403.value = false
    input.excelBPSAutoRecoverOn403.value = false
    input.excelBPSRecoveryIntervalMinutes.value = DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES
    input.excelBPSOmitUnsupportedTools.value = false
    input.excelBPSIgnoreEncryptedContent.value = false
    input.excelBPSAutoMoveOn403.value = false
    input.excelBPS403TargetGroupID.value = ''
    input.openaiPassthroughEnabled.value = false
    input.openaiFlattenNamespacesEnabled.value = false
    input.openAILongContextBillingEnabled.value = false
    input.editPlanType.value = ''
    input.openAICompactMode.value = 'auto'
    input.openAIResponsesMode.value = 'auto'
    input.openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
    input.openAICompactModelMappings.value = []
    input.openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
    input.openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
    input.codexCLIOnlyEnabled.value = false
    input.codexCLIOnlyAppServerEnabled.value = false
    input.codexFingerprintMode.value = 'off'
    input.codexImageToolMode.value = 'inherit'
    input.anthropicPassthroughEnabled.value = false
    input.anthropicAPIKeyAuthScheme.value = 'x_api_key'
    input.webSearchEmulationMode.value = 'default'
    if (newAccount.platform === 'openai' && (newAccount.type === 'oauth' || newAccount.type === 'setup-token' || newAccount.type === 'apikey')) {
      input.excelBPSEnabled.value = newAccount.type === 'oauth' && extra?.openai_excel_bps === true
      input.excelBPSModels.value = readExcelBPSModels(extra?.openai_excel_bps_models)
      input.excelBPSCacheCreationAsInput.value = extra?.openai_excel_bps_cache_creation_as_input === true
      input.excelBPSAutoDisableOn403.value = newAccount.type === 'oauth' && extra?.openai_excel_bps_auto_disable_on_403 === true
      input.excelBPSAutoRecoverOn403.value = newAccount.type === 'oauth' && extra?.openai_excel_bps_auto_recover_on_403 === true
      input.excelBPSRecoveryIntervalMinutes.value = bpsRecoveryIntervalOrDefault(extra?.openai_excel_bps_403_recovery_interval_minutes)
      input.excelBPSOmitUnsupportedTools.value = extra?.openai_excel_bps_omit_unsupported_tools === true
      input.excelBPSIgnoreEncryptedContent.value = extra?.openai_excel_bps_ignore_encrypted_content === true
      input.excelBPSAutoMoveOn403.value = newAccount.type === 'oauth' && extra?.openai_excel_bps_auto_move_on_403 === true
      const targetGroupID = extra?.openai_excel_bps_403_target_group_id
      input.excelBPS403TargetGroupID.value = typeof targetGroupID === 'number' && Number.isSafeInteger(targetGroupID) && targetGroupID >= 0 ? targetGroupID : ''
      input.openaiPassthroughEnabled.value = extra?.openai_passthrough === true || extra?.openai_oauth_passthrough === true
      input.openaiFlattenNamespacesEnabled.value =
        newAccount.type === 'oauth' && extra?.openai_responses_flatten_namespaces === true
      const longContextBillingValue = extra?.openai_long_context_billing_enabled
      input.openAILongContextBillingEnabled.value = longContextBillingValue === true
      // plan_type 手动覆盖仅 OAuth 有实际调度语义(IsOpenAIChatGPTSubscription 要求 oauth),故只对 oauth 回填
      input.editPlanType.value = newAccount.type === 'oauth'
        ? readPlanType(newAccount.credentials as Record<string, unknown> | undefined)
        : ''
      input.openAICompactMode.value = (extra?.openai_compact_mode as OpenAICompactMode) || 'auto'
      if (newAccount.type === 'apikey') {
        input.openAIResponsesMode.value = input.normalizeOpenAIResponsesMode(extra?.openai_responses_mode)
        input.openAIEndpointCapabilities.value = input.readOpenAIEndpointCapabilities(
          newAccount.credentials as Record<string, unknown> | undefined
        )
        if (!input.openAITextGenerationCapabilityEnabled.value) {
          input.openAIResponsesMode.value = 'auto'
        }
      }
      const codexImageGenerationBridgeValue = typeof extra?.codex_image_generation_bridge === 'boolean'
        ? extra.codex_image_generation_bridge
        : extra?.codex_image_generation_bridge_enabled
      if (extra?.codex_image_generation_explicit_tool_policy === 'strip') {
        input.codexImageToolMode.value = 'block'
      } else if (codexImageGenerationBridgeValue === true) {
        input.codexImageToolMode.value = 'enabled'
      } else if (codexImageGenerationBridgeValue === false) {
        input.codexImageToolMode.value = 'disabled'
      }
      input.openaiOAuthResponsesWebSocketV2Mode.value = resolveOpenAIWSModeFromExtra(extra, {
        modeKey: 'openai_oauth_responses_websockets_v2_mode',
        enabledKey: 'openai_oauth_responses_websockets_v2_enabled',
        fallbackEnabledKeys: ['responses_websockets_v2_enabled', 'openai_ws_enabled'],
        defaultMode: OPENAI_WS_MODE_OFF
      })
      input.openaiAPIKeyResponsesWebSocketV2Mode.value = resolveOpenAIWSModeFromExtra(extra, {
        modeKey: 'openai_apikey_responses_websockets_v2_mode',
        enabledKey: 'openai_apikey_responses_websockets_v2_enabled',
        fallbackEnabledKeys: ['responses_websockets_v2_enabled', 'openai_ws_enabled'],
        defaultMode: OPENAI_WS_MODE_OFF
      })
      if (newAccount.type === 'oauth' || newAccount.type === 'setup-token') {
        input.codexCLIOnlyEnabled.value = extra?.codex_cli_only === true
        input.codexCLIOnlyAppServerEnabled.value =
          extra?.codex_cli_only_allow_app_server === true
      }
      if (newAccount.type === 'oauth') {
        const fpMode = extra?.codex_fingerprint_mode as string | undefined
        // 缺省/非法值按 off 呈现，与后端 GetCodexFingerprintMode 的 opt-in 语义一致（#5610）
        input.codexFingerprintMode.value = (['off', 'device', 'session', 'full'].includes(fpMode || '')
          ? fpMode as CodexFingerprintMode
          : 'off')
      }
      const credentials = newAccount.credentials as Record<string, unknown> | undefined
      const compactMappings = credentials?.compact_model_mapping as Record<string, string> | undefined
      if (compactMappings && typeof compactMappings === 'object') {
        input.openAICompactModelMappings.value = Object.entries(compactMappings).map(([from, to]) => ({ from, to }))
      }
    }
    if (newAccount.platform === 'anthropic' && newAccount.type === 'apikey') {
      input.anthropicPassthroughEnabled.value = extra?.anthropic_passthrough === true
      input.anthropicAPIKeyAuthScheme.value = extra?.anthropic_apikey_auth_scheme === 'authorization_bearer'
        ? 'authorization_bearer'
        : 'x_api_key'
    }
    if ((newAccount.platform === 'anthropic' || newAccount.platform === 'openai') && newAccount.type === 'apikey') {
      // 三态：string "default"/"enabled"/"disabled"，向后兼容旧 bool
      const wsVal = extra?.web_search_emulation
      if (wsVal === 'enabled' || wsVal === 'disabled') {
        input.webSearchEmulationMode.value = wsVal
      } else if (wsVal === true) {
        input.webSearchEmulationMode.value = 'enabled'
      } else {
        input.webSearchEmulationMode.value = 'default'
      }
    }

    // Load quota limit for apikey/bedrock/service_account accounts (bedrock quota is also loaded in its own branch above)
    if (supportsAccountQuota(newAccount.type)) {
      const quotaVal = extra?.quota_limit as number | undefined
      input.editQuotaLimit.value = (quotaVal && quotaVal > 0) ? quotaVal : null
      const dailyVal = extra?.quota_daily_limit as number | undefined
      input.editQuotaDailyLimit.value = (dailyVal && dailyVal > 0) ? dailyVal : null
      const weeklyVal = extra?.quota_weekly_limit as number | undefined
      input.editQuotaWeeklyLimit.value = (weeklyVal && weeklyVal > 0) ? weeklyVal : null
      // Load quota reset mode config
      input.editDailyResetMode.value = (extra?.quota_daily_reset_mode as 'rolling' | 'fixed') || null
      input.editDailyResetHour.value = (extra?.quota_daily_reset_hour as number) ?? null
      input.editWeeklyResetMode.value = (extra?.quota_weekly_reset_mode as 'rolling' | 'fixed') || null
      input.editWeeklyResetDay.value = (extra?.quota_weekly_reset_day as number) ?? null
      input.editWeeklyResetHour.value = (extra?.quota_weekly_reset_hour as number) ?? null
      input.editResetTimezone.value = (extra?.quota_reset_timezone as string) || null
      // Load quota notify config
      input.loadQuotaNotifyFromExtra(extra)
    } else {
      input.editQuotaLimit.value = null
      input.editQuotaDailyLimit.value = null
      input.editQuotaWeeklyLimit.value = null
      input.editDailyResetMode.value = null
      input.editDailyResetHour.value = null
      input.editWeeklyResetMode.value = null
      input.editWeeklyResetDay.value = null
      input.editWeeklyResetHour.value = null
      input.editResetTimezone.value = null
      input.resetQuotaNotify()
    }

    // Load antigravity model mapping (Antigravity 只支持映射模式)
    if (newAccount.platform === 'antigravity') {
      const credentials = newAccount.credentials as Record<string, unknown> | undefined

      // Antigravity 始终使用映射模式
      input.antigravityModelRestrictionMode.value = 'mapping'
      input.antigravityWhitelistModels.value = []

      // 从 model_mapping 读取映射配置
      const rawAgMapping = credentials?.model_mapping as Record<string, string> | undefined
      if (rawAgMapping && typeof rawAgMapping === 'object') {
        const entries = Object.entries(rawAgMapping)
        // 无论是白名单样式(key===value)还是真正的映射，都统一转换为映射列表
        input.antigravityModelMappings.value = entries.map(([from, to]) => ({ from, to }))
      } else {
        // 兼容旧数据：从 model_whitelist 读取，转换为映射格式
        const rawWhitelist = credentials?.model_whitelist
        if (Array.isArray(rawWhitelist) && rawWhitelist.length > 0) {
          input.antigravityModelMappings.value = rawWhitelist
            .map((v) => String(v).trim())
            .filter((v) => v.length > 0)
            .map((m) => ({ from: m, to: m }))
        } else {
          input.antigravityModelMappings.value = []
        }
      }
    } else {
      input.antigravityModelRestrictionMode.value = 'mapping'
      input.antigravityWhitelistModels.value = []
      input.antigravityModelMappings.value = []
    }

    // Load quota control settings (Anthropic OAuth/SetupToken only)
    input.loadQuotaControlSettings(newAccount)

    input.loadTempUnschedRules(credentials)
    input.loadAccountSchedulingThresholdOverride(newAccount.platform, newAccount.type, credentials)

    // Load header override state for eligible account platforms/types
    input.headerOverrideEnabled.value = false
    input.headerOverrideRows.value = []
    if (newAccount.credentials && input.isHeaderOverrideCapable(newAccount.platform, newAccount.type)) {
      const overrideCreds = newAccount.credentials as Record<string, unknown>
      input.headerOverrideEnabled.value = overrideCreds[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] === true
      input.headerOverrideRows.value = splitHeaderOverridesObject(
        overrideCreds[HEADER_OVERRIDES_CREDENTIAL_KEY]
      )
    }

    // Load Grok OAuth custom upstream URL state（存储的官方地址视同未定制）
    input.grokOAuthCustomBaseUrlEnabled.value = false
    input.grokOAuthBaseUrl.value = ''
    const grokClientToolCacheSetting =
      newAccount.platform === 'grok' && newAccount.type === 'oauth'
        ? newAccount.extra?.[input.GROK_CLIENT_TOOL_CACHE_EXTRA_KEY]
        : undefined
    input.grokClientToolCacheEnabled.value =
      newAccount.platform === 'grok' &&
      newAccount.type === 'oauth' &&
      (grokClientToolCacheSetting === undefined || grokClientToolCacheSetting === true)
    input.grokMediaEligibilityMode.value = input.modeFromGrokMediaExtra(extra)
    input.grokMediaEligibilityInitialMode.value = input.grokMediaEligibilityMode.value
    input.grokMediaEligibilityState.value = null
    input.grokMediaEligibilityError.value = ''
    if (newAccount.platform === 'grok' && newAccount.type === 'oauth') {
      void input.loadGrokMediaEligibility(newAccount.id)
    } else {
      input.grokMediaEligibilityRequestVersion.value++
    }
    if (newAccount.platform === 'grok' && newAccount.type === 'oauth' && newAccount.credentials) {
      const grokCreds = newAccount.credentials as Record<string, unknown>
      if (isCustomGrokBaseUrl(grokCreds.base_url)) {
        input.grokOAuthCustomBaseUrlEnabled.value = true
        input.grokOAuthBaseUrl.value = (grokCreds.base_url as string).trim()
      }
    }

    // Initialize API Key fields for apikey type
    if (newAccount.type === 'apikey' && newAccount.credentials) {
      const credentials = newAccount.credentials as Record<string, unknown>
      // 国产供应商：读取 account_mode 与 api_protocol 作为可编辑初始值
      // （编辑弹窗允许修正两者，用于修复早期存错默认值的账号）。
      if (input.isMultiProtocolApiKeyPlatform(newAccount.platform)) {
        if (newAccount.platform === 'opencode_go') {
          input.editOpenCodeAccountMode.value = resolveOpenCodeAccountMode(credentials.account_mode)
        } else if (isCNProviderPlatform(newAccount.platform)) {
          input.editAccountMode.value = credentials.account_mode === 'coding' ? 'coding' : 'payg'
        } else {
          input.editAccountMode.value = input.resolveProviderAccountMode(newAccount.platform, credentials.account_mode)
        }
        const storedProtocol = credentials.api_protocol
        input.editApiProtocol.value =
          storedProtocol === 'adaptive' ||
            storedProtocol === 'chat_completions' ||
            storedProtocol === 'anthropic' ||
            storedProtocol === 'responses'
            ? storedProtocol
            : 'chat_completions'
        if (!input.cnSupportsNativeResponses(newAccount.platform, input.currentOpenCodeOrCNMode()) && input.editApiProtocol.value === 'responses') {
          input.editApiProtocol.value = 'chat_completions'
        }
        const adaptiveDefaults = input.defaultCNAdaptiveBaseUrls(newAccount.platform, input.currentOpenCodeOrCNMode())
        const storedBaseUrls = (credentials.api_base_urls as Record<string, unknown> | undefined) || {}
        const legacyBaseUrl = typeof credentials.base_url === 'string' ? credentials.base_url.trim() : ''
        const storedChatBaseUrl = typeof storedBaseUrls.chat_completions === 'string'
          ? storedBaseUrls.chat_completions.trim()
          : ''
        const storedAnthropicBaseUrl = typeof storedBaseUrls.anthropic === 'string'
          ? storedBaseUrls.anthropic.trim()
          : ''
        const storedResponsesBaseUrl = typeof storedBaseUrls.responses === 'string'
          ? storedBaseUrls.responses.trim()
          : ''
        const nextAdaptiveBaseUrls: Record<CnNativeApiProtocol, string> = {
          chat_completions: storedChatBaseUrl || adaptiveDefaults.chat_completions,
          anthropic: storedAnthropicBaseUrl || adaptiveDefaults.anthropic,
          responses: storedResponsesBaseUrl || adaptiveDefaults.responses
        }
        const legacyProtocol: CnNativeApiProtocol = input.editApiProtocol.value === 'anthropic'
          ? 'anthropic'
          : input.editApiProtocol.value === 'responses'
            ? 'responses'
            : 'chat_completions'
        const storedLegacyBaseUrl = legacyProtocol === 'anthropic'
          ? storedAnthropicBaseUrl
          : legacyProtocol === 'responses'
            ? storedResponsesBaseUrl
            : storedChatBaseUrl
        if (legacyBaseUrl && !storedLegacyBaseUrl) {
          nextAdaptiveBaseUrls[legacyProtocol] = legacyBaseUrl
        }
        input.editAdaptiveBaseUrls.value = nextAdaptiveBaseUrls
        // 智谱团队版 Coding Plan：回填组织/项目 ID
        if (newAccount.platform === 'zhipu') {
          input.editZhipuOrganization.value = typeof credentials.zhipu_organization === 'string' ? credentials.zhipu_organization : ''
          input.editZhipuProject.value = typeof credentials.zhipu_project === 'string' ? credentials.zhipu_project : ''
        }
        if (input.providerRoutesByModel(newAccount.platform)) {
          input.editOpenCodeGoProtocolRules.value =
            input.parseOpenCodeGoProtocolRules(credentials.protocol_rules) ??
            input.cloneOpenCodeGoProtocolRules(input.defaultProviderProtocolRules(newAccount.platform, input.currentOpenCodeOrCNMode()))
        }
      }
      const platformDefaultUrl =
        newAccount.platform === 'openai'
          ? 'https://api.openai.com'
          : newAccount.platform === 'gemini'
            ? 'https://generativelanguage.googleapis.com'
            : newAccount.platform === 'grok'
              ? 'https://api.x.ai/v1'
              : newAccount.platform === 'typesafe'
                ? 'https://api.typesafe.ai'
                : input.isMultiProtocolApiKeyPlatform(newAccount.platform)
                  ? input.defaultCNBaseUrl(newAccount.platform, input.currentOpenCodeOrCNMode(), input.editApiProtocol.value)
                  : 'https://api.anthropic.com'
      input.editBaseUrl.value = input.isCNApiKeyAccount.value && input.editApiProtocol.value === 'adaptive'
        ? input.editAdaptiveBaseUrls.value.chat_completions
        : (credentials.base_url as string) || platformDefaultUrl

      // Load model mappings and detect mode
      input.loadModelRestrictionFromMapping(credentials.model_mapping as Record<string, unknown> | undefined, credentials.model_allowlist)

      // Load pool mode
      input.poolModeEnabled.value = credentials.pool_mode === true
      input.poolModeRetryCount.value = input.normalizePoolModeRetryCount(
        Number(credentials.pool_mode_retry_count ?? input.DEFAULT_POOL_MODE_RETRY_COUNT)
      )
      input.poolModeRetryStatusCodesInput.value = input.formatPoolModeRetryStatusCodes(credentials.pool_mode_retry_status_codes)

      input.explicitCacheUpstreamEnabled.value = credentials.explicit_cache_upstream === true
      input.explicitCacheUpstreamMaxTTL.value =
        normalizeExplicitCacheUpstreamMaxTTL(credentials.explicit_cache_upstream_max_ttl_seconds) ?? ''

      // Load custom error codes
      input.customErrorCodesEnabled.value = credentials.custom_error_codes_enabled === true
      const existingErrorCodes = credentials.custom_error_codes as number[] | undefined
      if (existingErrorCodes && Array.isArray(existingErrorCodes)) {
        input.selectedErrorCodes.value = [...existingErrorCodes]
      } else {
        input.selectedErrorCodes.value = []
      }

    } else if (newAccount.type === 'bedrock' && newAccount.credentials) {
      const bedrockCreds = newAccount.credentials as Record<string, unknown>
      const authMode = (bedrockCreds.auth_mode as string) || 'sigv4'
      input.editBedrockRegion.value = (bedrockCreds.aws_region as string) || ''
      input.editBedrockForceGlobal.value = (bedrockCreds.aws_force_global as string) === 'true'

      if (authMode === 'apikey') {
        input.editBedrockApiKeyValue.value = ''
      } else {
        input.editBedrockAccessKeyId.value = (bedrockCreds.aws_access_key_id as string) || ''
        input.editBedrockSecretAccessKey.value = ''
        input.editBedrockSessionToken.value = ''
      }

      // Load pool mode for bedrock
      input.poolModeEnabled.value = bedrockCreds.pool_mode === true
      const retryCount = bedrockCreds.pool_mode_retry_count
      input.poolModeRetryCount.value = (typeof retryCount === 'number' && retryCount >= 0) ? retryCount : input.DEFAULT_POOL_MODE_RETRY_COUNT
      input.poolModeRetryStatusCodesInput.value = input.formatPoolModeRetryStatusCodes(bedrockCreds.pool_mode_retry_status_codes)

      // Load quota limits for bedrock
      const bedrockExtra = (newAccount.extra as Record<string, unknown>) || {}
      input.editQuotaLimit.value = typeof bedrockExtra.quota_limit === 'number' ? bedrockExtra.quota_limit : null
      input.editQuotaDailyLimit.value = typeof bedrockExtra.quota_daily_limit === 'number' ? bedrockExtra.quota_daily_limit : null
      input.editQuotaWeeklyLimit.value = typeof bedrockExtra.quota_weekly_limit === 'number' ? bedrockExtra.quota_weekly_limit : null
      // Load quota notify for bedrock
      input.loadQuotaNotifyFromExtra(bedrockExtra)

      // Load model mappings for bedrock
      input.loadModelRestrictionFromMapping(bedrockCreds.model_mapping as Record<string, unknown> | undefined, bedrockCreds.model_allowlist)
    } else if (newAccount.type === 'upstream' && newAccount.credentials) {
      const credentials = newAccount.credentials as Record<string, unknown>
      input.editBaseUrl.value = (credentials.base_url as string) || ''
    } else if ((newAccount.platform === 'gemini' || newAccount.platform === 'anthropic') && newAccount.type === 'service_account' && newAccount.credentials) {
      const credentials = newAccount.credentials as Record<string, unknown>
      input.editVertexProjectId.value = (credentials.project_id as string) || ''
      input.editVertexClientEmail.value = (credentials.client_email as string) || ''
      input.editVertexLocation.value = (credentials.location as string) || (credentials.vertex_location as string) || 'us-central1'
      input.editVertexApiKey.value = ''

      // Load model mappings for service_account
      input.loadModelRestrictionFromMapping(credentials.model_mapping as Record<string, unknown> | undefined, credentials.model_allowlist)
    } else {
      const platformDefaultUrl =
        newAccount.platform === 'openai'
          ? 'https://api.openai.com'
          : newAccount.platform === 'gemini'
            ? 'https://generativelanguage.googleapis.com'
            : newAccount.platform === 'grok'
              ? 'https://api.x.ai/v1'
              : 'https://api.anthropic.com'
      input.editBaseUrl.value = platformDefaultUrl

      // Load model mappings for OAuth / setup-token accounts
      if ((input.hasOAuthModelRestriction.value || newAccount.platform === 'openai' || newAccount.platform === 'grok') && newAccount.credentials) {
        const oauthCredentials = newAccount.credentials as Record<string, unknown>
        input.loadModelRestrictionFromMapping(oauthCredentials.model_mapping as Record<string, unknown> | undefined, oauthCredentials.model_allowlist)
      } else {
        input.modelRestrictionMode.value = 'whitelist'
        input.modelMappings.value = []
        input.allowedModels.value = []
      }
      input.poolModeEnabled.value = false
      input.poolModeRetryCount.value = input.DEFAULT_POOL_MODE_RETRY_COUNT
      input.poolModeRetryStatusCodesInput.value = ''
      input.explicitCacheUpstreamEnabled.value = false
      input.explicitCacheUpstreamMaxTTL.value = ''
      input.customErrorCodesEnabled.value = false
      input.selectedErrorCodes.value = []
    }
    input.editApiKey.value = ''
  }
  return { syncingForm, syncFormFromAccount }
}
