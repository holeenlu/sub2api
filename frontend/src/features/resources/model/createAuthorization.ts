import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildModelMappingObject } from '../model/modelRestrictions'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import { buildClaudeSetupTokenCredentials, useAccountOAuth, type AddMethod, type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import { useOpenAIOAuth } from '@/features/resources/queries/useOpenAIOAuth'
import { useGeminiOAuth } from '@/features/resources/queries/useGeminiOAuth'
import { useAntigravityOAuth } from '@/features/resources/queries/useAntigravityOAuth'
import { useGrokOAuth } from '@/features/resources/queries/useGrokOAuth'
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { CreateAccountRequest } from '@/features/resources/api/accountTypes';
import { supportsAccountQuota } from '@/features/resources/model/accountQuota'
import { OPENAI_WS_MODE_OFF, type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { OAuthFlowExposed, Props, ModelMapping } from './createEditorTypes'




interface CreateAuthorizationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  form: Pick<CreateResourceForm, "platform" | "proxy_id" | "name" | "notes" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  t: ReturnType<typeof useI18n>["t"]
  antigravityAccountType: Ref<"oauth" | "upstream", "oauth" | "upstream">
  accountCategory: Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">
  addMethod: Ref<AddMethod, AddMethod>
  geminiAIStudioOAuthEnabled: Ref<boolean, boolean>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
  step: Ref<number, number>
  geminiSelectedTier: ComputedRef<"" | "aistudio_free" | "aistudio_paid" | "google_one_free" | "google_ai_pro" | "google_ai_ultra" | "gcp_standard" | "gcp_enterprise">
  handleOpenAIValidateRT: (rt: string) => Promise<void>
  handleAntigravityValidateRT: (refreshTokenInput: string) => Promise<void>
  handleGrokValidateRT: (refreshTokenInput: string) => Promise<void>
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  editQuotaLimit: Ref<number | null, number | null>
  editQuotaDailyLimit: Ref<number | null, number | null>
  editQuotaWeeklyLimit: Ref<number | null, number | null>
  editDailyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editDailyResetHour: Ref<number | null, number | null>
  editWeeklyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editWeeklyResetDay: Ref<number | null, number | null>
  editWeeklyResetHour: Ref<number | null, number | null>
  editResetTimezone: Ref<string | null, string | null>
  writeQuotaNotifyToExtra: ReturnType<typeof useQuotaNotifyState>["writeToExtra"]
  applyOpenAIEndpointCapabilities: (credentials: Record<string, unknown>) => void
  buildOpenAICompactModelMapping: () => Record<string, string> | null
  apiKeyBaseUrl: Ref<string, string>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  allowedModels: Ref<string[], string[]>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  doCreateAccount: (payload: CreateAccountRequest) => Promise<void>
  props: Readonly<Pick<Props, "allowRateEdit">>
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
  autoPauseOnExpired: Ref<boolean, boolean>
  handleAnthropicSetupToken: (rawToken: string) => Promise<void>
  handleOpenAIExchange: (authCode: string) => Promise<void>
  handleGeminiExchange: (authCode: string) => Promise<void>
  handleAntigravityExchange: (authCode: string) => Promise<void>
  handleGrokExchange: (authCode: string) => Promise<void>
  handleAnthropicExchange: (authCode: string) => Promise<void>
}




export function useCreateAuthorizationDraft(input: CreateAuthorizationInput): {
  oauthStepTitle: ComputedRef<string>
  oauth: ReturnType<typeof useAccountOAuth>
  openaiOAuth: ReturnType<typeof useOpenAIOAuth>
  geminiOAuth: ReturnType<typeof useGeminiOAuth>
  antigravityOAuth: ReturnType<typeof useAntigravityOAuth>
  grokOAuth: ReturnType<typeof useGrokOAuth>
  currentAuthUrl: ComputedRef<string>
  currentSessionId: ComputedRef<string>
  currentOAuthLoading: ComputedRef<boolean>
  currentOAuthError: ComputedRef<string>
  oauthFlowRef: Ref<{ authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null, OAuthFlowExposed | { authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null>
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  geminiOAuthType: Ref<"code_assist" | "google_one" | "ai_studio", "code_assist" | "google_one" | "ai_studio">
  isOAuthFlow: ComputedRef<boolean>
  isGrokSSOInputMethod: ComputedRef<boolean>
  isManualInputMethod: ComputedRef<boolean>
  canExchangeCode: ComputedRef<boolean | "">
  handleSelectGeminiOAuthType: (oauthType: "code_assist" | "google_one" | "ai_studio") => void
  goBackToBasicInfo: () => void
  handleGenerateUrl: () => Promise<void>
  handleValidateRefreshToken: (rt: string) => void
  handleValidateSessionToken: (_sessionToken: string) => void
  createAccountAndFinish: (platform: AccountPlatform, type: AccountType, credentials: Record<string, unknown>, extra?: Record<string, unknown>) => Promise<void>
  handleExchangeCode: () => Promise<void>
} {


  const oauthStepTitle = computed(() => {
    if (input.form.platform === 'openai') return input.t('admin.accounts.oauth.openai.title')
    if (input.form.platform === 'gemini') return input.t('admin.accounts.oauth.gemini.title')
    if (input.form.platform === 'antigravity') return input.t('admin.accounts.oauth.antigravity.title')
    if (input.form.platform === 'grok') return input.t('admin.accounts.oauth.grok.title')
    return input.t('admin.accounts.oauth.title')
  })


  // OAuth composables
  const oauth = useAccountOAuth()
  // For Anthropic OAuth
  const openaiOAuth = useOpenAIOAuth()
  // For OpenAI OAuth
  const geminiOAuth = useGeminiOAuth()
  // For Gemini OAuth
  const antigravityOAuth = useAntigravityOAuth()
  // For Antigravity OAuth
  const grokOAuth = useGrokOAuth()
  // For Grok OAuth

  // Computed: current OAuth state for template binding
  const currentAuthUrl = computed(() => {
    if (input.form.platform === 'openai') return openaiOAuth.authUrl.value
    if (input.form.platform === 'gemini') return geminiOAuth.authUrl.value
    if (input.form.platform === 'antigravity') return antigravityOAuth.authUrl.value
    if (input.form.platform === 'grok') return grokOAuth.authUrl.value
    return oauth.authUrl.value
  })


  const currentSessionId = computed(() => {
    if (input.form.platform === 'openai') return openaiOAuth.sessionId.value
    if (input.form.platform === 'gemini') return geminiOAuth.sessionId.value
    if (input.form.platform === 'antigravity') return antigravityOAuth.sessionId.value
    if (input.form.platform === 'grok') return grokOAuth.sessionId.value
    return oauth.sessionId.value
  })


  const currentOAuthLoading = computed(() => {
    if (input.form.platform === 'openai') return openaiOAuth.loading.value
    if (input.form.platform === 'gemini') return geminiOAuth.loading.value
    if (input.form.platform === 'antigravity') return antigravityOAuth.loading.value
    if (input.form.platform === 'grok') return grokOAuth.loading.value
    return oauth.loading.value
  })


  const currentOAuthError = computed(() => {
    if (input.form.platform === 'openai') return openaiOAuth.error.value
    if (input.form.platform === 'gemini') return geminiOAuth.error.value
    if (input.form.platform === 'antigravity') return antigravityOAuth.error.value
    if (input.form.platform === 'grok') return grokOAuth.error.value
    return oauth.error.value
  })


  // Refs
  const oauthFlowRef = ref<OAuthFlowExposed | null>(null)


  // Grok OAuth：自定义上游地址（base_url 仅改写转发端点，OAuth 授权/刷新不受影响）
  const grokOAuthCustomBaseUrlEnabled = ref(false)

  const grokOAuthBaseUrl = ref('')

  const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)

  const geminiOAuthType = ref<'code_assist' | 'google_one' | 'ai_studio'>('google_one')


  // Helper to check if current type needs OAuth flow
  const isOAuthFlow = computed(() => {
    // Antigravity upstream 类型不需要 OAuth 流程
    if (input.form.platform === 'antigravity' && input.antigravityAccountType.value === 'upstream') {
      return false
    }
    // Bedrock 类型不需要 OAuth 流程
    if (input.form.platform === 'anthropic' && input.accountCategory.value === 'bedrock') {
      return false
    }
    return input.accountCategory.value === 'oauth-based'
  })


  const isGrokSSOInputMethod = computed(() => input.form.platform === 'grok' && oauthFlowRef.value?.inputMethod === 'sso_cookie')


  const isManualInputMethod = computed(() => {
    return oauthFlowRef.value?.inputMethod === 'manual' || oauthFlowRef.value?.inputMethod === 'setup_token'
  })


  const canExchangeCode = computed(() => {
    const authCode = oauthFlowRef.value?.authCode || ''
    if (input.form.platform === 'anthropic' && input.addMethod.value === 'setup-token-manual') {
      return !!buildClaudeSetupTokenCredentials(oauthFlowRef.value?.setupToken || '') && !oauth.loading.value
    }
    if (input.form.platform === 'openai') {
      return authCode.trim() && openaiOAuth.sessionId.value && !openaiOAuth.loading.value
    }
    if (input.form.platform === 'gemini') {
      return authCode.trim() && geminiOAuth.sessionId.value && !geminiOAuth.loading.value
    }
    if (input.form.platform === 'antigravity') {
      return authCode.trim() && antigravityOAuth.sessionId.value && !antigravityOAuth.loading.value
    }
    if (input.form.platform === 'grok') {
      return authCode.trim() && grokOAuth.sessionId.value && !grokOAuth.loading.value
    }
    return authCode.trim() && oauth.sessionId.value && !oauth.loading.value
  })


  const handleSelectGeminiOAuthType = (oauthType: 'code_assist' | 'google_one' | 'ai_studio') => {
    if (oauthType === 'ai_studio' && !input.geminiAIStudioOAuthEnabled.value) {
      input.appStore.showError(input.t('admin.accounts.oauth.gemini.aiStudioNotConfigured'))
      return
    }
    geminiOAuthType.value = oauthType
  }


  const goBackToBasicInfo = () => {
    input.step.value = 1
    oauth.resetState()
    openaiOAuth.resetState()
    geminiOAuth.resetState()
    antigravityOAuth.resetState()
    grokOAuth.resetState()
    oauthFlowRef.value?.reset()
  }


  // Draft getters are wired before all component refs are initialized. Bind after setup.
  onMounted(() => {
    // Token exchange and its following create must share the same authorization context.
    watch(
      () => [input.form.platform, input.form.proxy_id, input.accountCategory.value,
        input.addMethod.value, input.antigravityAccountType.value, geminiOAuthType.value],
      () => {
        oauth.resetState()
        openaiOAuth.resetState()
        geminiOAuth.resetState()
        antigravityOAuth.resetState()
        grokOAuth.resetState()
        oauthFlowRef.value?.reset()
      },
      { flush: 'sync' }
    )
  })

  const handleGenerateUrl = async () => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (input.form.platform === 'openai') {
        await pending.wait(async () => openaiOAuth.generateAuthUrl(input.form.proxy_id))
      } else if (input.form.platform === 'gemini') {
        await pending.wait(async () => geminiOAuth.generateAuthUrl(
          input.form.proxy_id,
          oauthFlowRef.value?.projectId,
          geminiOAuthType.value,
          input.geminiSelectedTier.value
        ))
      } else if (input.form.platform === 'antigravity') {
        await pending.wait(async () => antigravityOAuth.generateAuthUrl(input.form.proxy_id))
      } else if (input.form.platform === 'grok') {
        await pending.wait(async () => grokOAuth.generateAuthUrl(input.form.proxy_id))
      } else {
        await pending.wait(async () => oauth.generateAuthUrl(input.addMethod.value, input.form.proxy_id))
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleValidateRefreshToken = (rt: string) => {
    if (input.form.platform === 'openai') {
      input.handleOpenAIValidateRT(rt)
    } else if (input.form.platform === 'antigravity') {
      input.handleAntigravityValidateRT(rt)
    } else if (input.form.platform === 'grok') {
      input.handleGrokValidateRT(rt)
    }
  }


  const handleValidateSessionToken = (_sessionToken: string) => {
    // Session token validation removed
  }


  // Create account and handle success/failure
  const createAccountAndFinish = async (
    platform: AccountPlatform,
    type: AccountType,
    credentials: Record<string, unknown>,
    extra?: Record<string, unknown>
  ) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!input.applyTempUnschedConfig(credentials)) {
        return
      }
      // Inject quota limits for apikey/bedrock/service_account accounts
      let finalExtra = input.withAccountExtra(extra)
      if (supportsAccountQuota(type)) {
        const quotaExtra: Record<string, unknown> = { ...(finalExtra || {}) }
        if (input.editQuotaLimit.value != null && input.editQuotaLimit.value > 0) {
          quotaExtra.quota_limit = input.editQuotaLimit.value
        }
        if (input.editQuotaDailyLimit.value != null && input.editQuotaDailyLimit.value > 0) {
          quotaExtra.quota_daily_limit = input.editQuotaDailyLimit.value
        }
        if (input.editQuotaWeeklyLimit.value != null && input.editQuotaWeeklyLimit.value > 0) {
          quotaExtra.quota_weekly_limit = input.editQuotaWeeklyLimit.value
        }
        // Quota reset mode config
        if (input.editDailyResetMode.value === 'fixed') {
          quotaExtra.quota_daily_reset_mode = 'fixed'
          quotaExtra.quota_daily_reset_hour = input.editDailyResetHour.value ?? 0
        }
        if (input.editWeeklyResetMode.value === 'fixed') {
          quotaExtra.quota_weekly_reset_mode = 'fixed'
          quotaExtra.quota_weekly_reset_day = input.editWeeklyResetDay.value ?? 1
          quotaExtra.quota_weekly_reset_hour = input.editWeeklyResetHour.value ?? 0
        }
        if (input.editDailyResetMode.value === 'fixed' || input.editWeeklyResetMode.value === 'fixed') {
          quotaExtra.quota_reset_timezone = input.editResetTimezone.value || 'UTC'
        }
        // Quota notify config
        input.writeQuotaNotifyToExtra(quotaExtra, 'create')
        if (Object.keys(quotaExtra).length > 0) {
          finalExtra = quotaExtra
        }
      }
      if (platform === 'openai') {
        if (type === 'apikey') {
          input.applyOpenAIEndpointCapabilities(credentials)
        }
        const compactModelMapping = input.buildOpenAICompactModelMapping()
        if (compactModelMapping) {
          credentials.compact_model_mapping = compactModelMapping
        } else {
          delete credentials.compact_model_mapping
        }
      }
      if (platform === 'grok') {
        if (!credentials.base_url) {
          credentials.base_url = input.apiKeyBaseUrl.value.trim() || 'https://api.x.ai/v1'
        }
        const modelMapping = buildModelMappingObject(input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
        if (modelMapping) {
          credentials.model_mapping = modelMapping
        } else {
          delete credentials.model_mapping
        }
      }
      await pending.wait(async () => input.doCreateAccount({
        name: input.form.name,
        notes: input.form.notes,
        platform,
        type,
        credentials,
        extra: finalExtra,
        proxy_id: input.form.proxy_id,
        concurrency: input.form.concurrency,
        load_factor: input.form.load_factor ?? undefined,
        priority: input.form.priority,
        rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
        group_ids: input.form.group_ids,
        expires_at: input.form.expires_at,
        // 上游倍率探测对全部 API-key 平台开放（antigravity upstream 走本 helper）；
        // 非 apikey 类型（bedrock/oauth）不传，后端不动作。
        upstream_billing_probe_enabled: input.props.allowRateEdit && type === 'apikey' ? input.upstreamBillingAutoProbeEnabled.value : undefined,
        auto_pause_on_expired: input.autoPauseOnExpired.value
      }))

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // 主入口：根据平台路由到对应处理函数
  const handleExchangeCode = async () => {
    try {

      if (input.form.platform === 'anthropic' && input.addMethod.value === 'setup-token-manual') {
        return input.handleAnthropicSetupToken(oauthFlowRef.value?.setupToken || '')
      }
      const authCode = oauthFlowRef.value?.authCode || ''

      switch (input.form.platform) {
        case 'openai':
          return input.handleOpenAIExchange(authCode)
        case 'gemini':
          return input.handleGeminiExchange(authCode)
        case 'antigravity':
          return input.handleAntigravityExchange(authCode)
        case 'grok':
          return input.handleGrokExchange(authCode)
        default:
          return input.handleAnthropicExchange(authCode)
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { oauthStepTitle, oauth, openaiOAuth, geminiOAuth, antigravityOAuth, grokOAuth, currentAuthUrl, currentSessionId, currentOAuthLoading, currentOAuthError, oauthFlowRef, grokOAuthCustomBaseUrlEnabled, grokOAuthBaseUrl, openaiOAuthResponsesWebSocketV2Mode, geminiOAuthType, isOAuthFlow, isGrokSSOInputMethod, isManualInputMethod, canExchangeCode, handleSelectGeminiOAuthType, goBackToBasicInfo, handleGenerateUrl, handleValidateRefreshToken, handleValidateSessionToken, createAccountAndFinish, handleExchangeCode }
}
