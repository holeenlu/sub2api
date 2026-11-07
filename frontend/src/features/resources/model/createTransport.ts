import type { CreateResourceForm } from './createEditorTypes'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpenAICompactMode, OpenAIResponsesMode, OpenAIEndpointCapability } from '@/features/resources/api/accountCredentialTypes';
import { applyHeaderOverride, validateHeaderOverrideRows, type HeaderOverrideRow } from "@/features/resources/model/credentialsBuilder"
import { OPENAI_WS_MODE_CTX_POOL, OPENAI_WS_MODE_OFF, OPENAI_WS_MODE_PASSTHROUGH, OPENAI_WS_MODE_HTTP_BRIDGE, isOpenAIWSModeEnabled, resolveOpenAIWSModeHintKey, type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref, ComputedRef, WritableComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, CodexFingerprintMode, AnthropicAPIKeyAuthScheme } from './createEditorTypes'




interface CreateTransportInput {
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
  t: ReturnType<typeof useI18n>["t"]
  form: Pick<CreateResourceForm, "platform" | "type">
  accountCategory: Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  props: Readonly<Pick<Props, "allowRateEdit">>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
}




export function useCreateTransportDraft(input: CreateTransportInput): {
  explicitCacheUpstreamEnabled: Ref<boolean, boolean>
  explicitCacheUpstreamMaxTTL: Ref<string | number, string | number>
  headerOverrideEnabled: Ref<boolean, boolean>
  headerOverrideRows: Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>
  validateGrokOAuthUpstreamConfig: () => boolean
  applyGrokOAuthUpstreamConfig: (credentials: Record<string, unknown>) => void
  interceptWarmupRequests: Ref<boolean, boolean>
  openaiPassthroughEnabled: Ref<boolean, boolean>
  openaiFlattenNamespacesEnabled: Ref<boolean, boolean>
  openAICompactMode: Ref<OpenAICompactMode, OpenAICompactMode>
  openAIResponsesMode: Ref<OpenAIResponsesMode, OpenAIResponsesMode>
  openAIImagesUrlToB64JsonEnabled: Ref<boolean, boolean>
  openAIEndpointCapabilities: Ref<OpenAIEndpointCapability[], OpenAIEndpointCapability[]>
  openaiAPIKeyResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  codexCLIOnlyEnabled: Ref<boolean, boolean>
  codexCLIOnlyAppServerEnabled: Ref<boolean, boolean>
  codexFingerprintMode: Ref<CodexFingerprintMode, CodexFingerprintMode>
  codexFingerprintModeOptions: ComputedRef<{ value: CodexFingerprintMode; label: string }[]>
  anthropicPassthroughEnabled: Ref<boolean, boolean>
  anthropicAPIKeyAuthScheme: Ref<AnthropicAPIKeyAuthScheme, AnthropicAPIKeyAuthScheme>
  webSearchEmulationMode: Ref<string, string>
  webSearchGlobalEnabled: Ref<boolean, boolean>
  mixedScheduling: Ref<boolean, boolean>
  allowOverages: Ref<boolean, boolean>
  openAICompactModeOptions: ComputedRef<{ value: string; label: string }[]>
  openAIResponsesModeOptions: ComputedRef<{ value: string; label: string }[]>
  openAITextEndpointCapabilityLabel: ComputedRef<string>
  openAIEndpointCapabilityOptions: ComputedRef<{ value: OpenAIEndpointCapability; label: string }[]>
  openAITextGenerationCapabilityEnabled: ComputedRef<boolean>
  normalizeOpenAIEndpointCapabilities: (values: OpenAIEndpointCapability[]) => OpenAIEndpointCapability[]
  toggleOpenAIEndpointCapability: (capability: OpenAIEndpointCapability, event?: Event) => void
  applyOpenAIEndpointCapabilities: (credentials: Record<string, unknown>) => void
  tlsFingerprintEnabled: Ref<boolean, boolean>
  tlsFingerprintProfileId: Ref<number | null, number | null>
  tlsFingerprintProfiles: Ref<{ id: number; name: string }[], { id: number; name: string }[] | { id: number; name: string }[]>
  sessionIdMaskingEnabled: Ref<boolean, boolean>
  cacheTTLOverrideEnabled: Ref<boolean, boolean>
  cacheTTLOverrideTarget: Ref<string, string>
  customBaseUrlEnabled: Ref<boolean, boolean>
  customBaseUrl: Ref<string, string>
  openAIWSModeOptions: ComputedRef<{ value: string; label: string }[]>
  openaiResponsesWebSocketV2Mode: WritableComputedRef<OpenAIWSMode, OpenAIWSMode>
  openAIWSModeHintKey: ComputedRef<"admin.accounts.openai.wsModeCtxPoolHint" | "admin.accounts.openai.wsModePassthroughHint" | "admin.accounts.openai.wsModeHttpBridgeHint" | null>
  buildOpenAIExtra: (base?: Record<string, unknown>) => Record<string, unknown> | undefined
  buildAnthropicExtra: (base?: Record<string, unknown>) => Record<string, unknown> | undefined
} {

  const explicitCacheUpstreamEnabled = ref(false)

  const explicitCacheUpstreamMaxTTL = ref<string | number>('')

  const headerOverrideEnabled = ref(false)

  const headerOverrideRows = ref<HeaderOverrideRow[]>([])


  // Grok OAuth 三条创建路径（授权码/RT 批量/SSO 批量）共用的前置校验。
  // 授权码路径必须在兑换 code 之前调用，避免校验失败时白白消耗一次性授权码。
  const validateGrokOAuthUpstreamConfig = (): boolean => {
    if (input.grokOAuthCustomBaseUrlEnabled.value) {
      const trimmed = input.grokOAuthBaseUrl.value.trim()
      if (!trimmed) {
        input.appStore.showError(input.t('admin.accounts.grokCustomBaseUrl.required'))
        return false
      }
      if (!/^https?:\/\//i.test(trimmed)) {
        input.appStore.showError(input.t('admin.accounts.grokCustomBaseUrl.invalid'))
        return false
      }
    }
    if (headerOverrideEnabled.value) {
      const headerError = validateHeaderOverrideRows(headerOverrideRows.value)
      if (headerError) {
        input.appStore.showError(input.t(`admin.accounts.headerOverride.${headerError}`))
        return false
      }
    }
    return true
  }


  // 把已通过校验的自定义上游地址与请求头覆写写入 credentials
  const applyGrokOAuthUpstreamConfig = (credentials: Record<string, unknown>) => {
    if (input.grokOAuthCustomBaseUrlEnabled.value) {
      credentials.base_url = input.grokOAuthBaseUrl.value.trim()
    }
    applyHeaderOverride(credentials, headerOverrideEnabled.value, headerOverrideRows.value, 'create')
  }

  const interceptWarmupRequests = ref(false)

  const openaiPassthroughEnabled = ref(false)

  // OpenAI Codex namespace 工具摊平兼容开关（仅 OAuth），缺省关闭即原样保留
  const openaiFlattenNamespacesEnabled = ref(false)

  const openAICompactMode = ref<OpenAICompactMode>('auto')

  const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')

  // Images 非流式响应缺 b64_json 时由网关下载 url 回填（仅 OpenAI API Key）。
  const openAIImagesUrlToB64JsonEnabled = ref(false)

  const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>(['chat_completions', 'embeddings'])

  const openaiAPIKeyResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)

  const codexCLIOnlyEnabled = ref(false)

  const codexCLIOnlyAppServerEnabled = ref(false)

  const codexFingerprintMode = ref<CodexFingerprintMode>('off')

  const codexFingerprintModeOptions = computed(() => [
    { value: 'off' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintOff') },
    { value: 'device' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintDevice') },
    { value: 'session' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintSession') },
    { value: 'full' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintFull') },
  ])

  const anthropicPassthroughEnabled = ref(false)

  const anthropicAPIKeyAuthScheme = ref<AnthropicAPIKeyAuthScheme>('x_api_key')

  const webSearchEmulationMode = ref('default')

  const webSearchGlobalEnabled = ref(false)

  const mixedScheduling = ref(false)
  // For antigravity accounts: enable mixed scheduling
  const allowOverages = ref(false)

  const openAICompactModeOptions = computed(() => [
    { value: 'auto', label: input.t('admin.accounts.openai.compactModeAuto') },
    { value: 'force_on', label: input.t('admin.accounts.openai.compactModeForceOn') },
    { value: 'force_off', label: input.t('admin.accounts.openai.compactModeForceOff') }
  ])

  const openAIResponsesModeOptions = computed(() => [
    { value: 'auto', label: input.t('admin.accounts.openai.responsesModeAuto') },
    { value: 'force_responses', label: input.t('admin.accounts.openai.responsesModeForceResponses') },
    { value: 'force_chat_completions', label: input.t('admin.accounts.openai.responsesModeForceChatCompletions') }
  ])

  const openAITextEndpointCapabilityLabel = computed(() => {
    if (openAIResponsesMode.value === 'force_responses') {
      return input.t('admin.accounts.openai.capabilityResponses')
    }
    if (openAIResponsesMode.value === 'force_chat_completions') {
      return input.t('admin.accounts.openai.capabilityChatCompletions')
    }
    return input.t('admin.accounts.openai.capabilityTextAuto')
  })

  const openAIEndpointCapabilityOptions = computed<{ value: OpenAIEndpointCapability; label: string }[]>(() => [
    { value: 'chat_completions', label: openAITextEndpointCapabilityLabel.value },
    { value: 'embeddings', label: input.t('admin.accounts.openai.capabilityEmbeddings') },
    { value: 'seedance', label: 'Seedance (Ark)' }
  ])

  const openAITextGenerationCapabilityEnabled = computed(() =>
    openAIEndpointCapabilities.value.includes('chat_completions')
  )


  const normalizeOpenAIEndpointCapabilities = (values: OpenAIEndpointCapability[]) => {
    const allowed: OpenAIEndpointCapability[] = ['chat_completions', 'embeddings', 'seedance']
    const selected = allowed.filter((value) => values.includes(value))
    return selected.length > 0 ? selected : ['chat_completions', 'embeddings'] as OpenAIEndpointCapability[]
  }


  const toggleOpenAIEndpointCapability = (capability: OpenAIEndpointCapability, event?: Event) => {
    if (openAIEndpointCapabilities.value.includes(capability)) {
      if (openAIEndpointCapabilities.value.length <= 1) {
        const input = event?.target as HTMLInputElement | null
        if (input) input.checked = true
        return
      }
      openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(
        (value) => value !== capability
      )
      if (!openAITextGenerationCapabilityEnabled.value) {
        openAIResponsesMode.value = 'auto'
      }
      return
    }
    openAIEndpointCapabilities.value = normalizeOpenAIEndpointCapabilities([
      ...openAIEndpointCapabilities.value,
      capability
    ])
  }


  const applyOpenAIEndpointCapabilities = (credentials: Record<string, unknown>) => {
    const capabilities = normalizeOpenAIEndpointCapabilities(openAIEndpointCapabilities.value)
    if (capabilities.length === 2 && !capabilities.includes('seedance')) {
      delete credentials.openai_capabilities
      return
    }
    credentials.openai_capabilities = capabilities
  }

  const tlsFingerprintEnabled = ref(false)

  const tlsFingerprintProfileId = ref<number | null>(null)

  const tlsFingerprintProfiles = ref<{ id: number; name: string }[]>([])

  const sessionIdMaskingEnabled = ref(false)

  const cacheTTLOverrideEnabled = ref(false)

  const cacheTTLOverrideTarget = ref<string>('5m')

  const customBaseUrlEnabled = ref(false)

  const customBaseUrl = ref('')


  const openAIWSModeOptions = computed(() => [
    { value: OPENAI_WS_MODE_OFF, label: input.t('admin.accounts.openai.wsModeOff') },
    { value: OPENAI_WS_MODE_CTX_POOL, label: input.t('admin.accounts.openai.wsModeCtxPool') },
    { value: OPENAI_WS_MODE_PASSTHROUGH, label: input.t('admin.accounts.openai.wsModePassthrough') },
    { value: OPENAI_WS_MODE_HTTP_BRIDGE, label: input.t('admin.accounts.openai.wsModeHttpBridge') }
  ])


  const openaiResponsesWebSocketV2Mode = computed({
    get: () => {
      if (input.form.platform === 'openai' && input.accountCategory.value === 'apikey') {
        return openaiAPIKeyResponsesWebSocketV2Mode.value
      }
      return input.openaiOAuthResponsesWebSocketV2Mode.value
    },
    set: (mode: OpenAIWSMode) => {
      if (input.form.platform === 'openai' && input.accountCategory.value === 'apikey') {
        openaiAPIKeyResponsesWebSocketV2Mode.value = mode
        return
      }
      input.openaiOAuthResponsesWebSocketV2Mode.value = mode
    }
  })


  const openAIWSModeHintKey = computed(() =>
    resolveOpenAIWSModeHintKey(openaiResponsesWebSocketV2Mode.value)
  )


  const buildOpenAIExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
    if (input.form.platform !== 'openai') {
      return base
    }

    const extra: Record<string, unknown> = { ...(base || {}) }
    if (input.accountCategory.value === 'oauth-based') {
      extra.openai_oauth_responses_websockets_v2_mode = input.openaiOAuthResponsesWebSocketV2Mode.value
      extra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(input.openaiOAuthResponsesWebSocketV2Mode.value)
    } else if (input.accountCategory.value === 'apikey') {
      extra.openai_apikey_responses_websockets_v2_mode = openaiAPIKeyResponsesWebSocketV2Mode.value
      extra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiAPIKeyResponsesWebSocketV2Mode.value)
    }
    // 清理兼容旧键，统一改用分类型开关。
    delete extra.responses_websockets_v2_enabled
    delete extra.openai_ws_enabled
    if (openaiPassthroughEnabled.value) {
      extra.openai_passthrough = true
    } else {
      delete extra.openai_passthrough
      delete extra.openai_oauth_passthrough
    }
    // 缺省即保留 namespace，不写空值，避免 extra 里堆积默认项
    if (input.form.type === 'oauth' && openaiFlattenNamespacesEnabled.value) {
      extra.openai_responses_flatten_namespaces = true
    } else {
      delete extra.openai_responses_flatten_namespaces
    }
    if (input.props.allowRateEdit) extra.openai_long_context_billing_enabled = input.openAILongContextBillingEnabled.value

    if (input.accountCategory.value === 'oauth-based' && codexCLIOnlyEnabled.value) {
      extra.codex_cli_only = true
    } else {
      delete extra.codex_cli_only
    }
    delete extra.codex_cli_only_allowed_clients
    if (
      input.accountCategory.value === 'oauth-based' &&
      codexCLIOnlyEnabled.value &&
      codexCLIOnlyAppServerEnabled.value
    ) {
      extra.codex_cli_only_allow_app_server = true
    } else {
      delete extra.codex_cli_only_allow_app_server
    }
    // 收敛是显式 opt-in：off 即默认值，不落键；device/session/full 必须显式写入，
    // 否则管理员的选择会被当成默认而丢失（#5610）。
    if (codexFingerprintMode.value !== 'off') {
      extra.codex_fingerprint_mode = codexFingerprintMode.value
    } else {
      delete extra.codex_fingerprint_mode
    }
    if (openAICompactMode.value !== 'auto') {
      extra.openai_compact_mode = openAICompactMode.value
    } else {
      delete extra.openai_compact_mode
    }

    if (
      input.accountCategory.value === 'apikey' &&
      openAITextGenerationCapabilityEnabled.value &&
      openAIResponsesMode.value !== 'auto'
    ) {
      extra.openai_responses_mode = openAIResponsesMode.value
    } else {
      delete extra.openai_responses_mode
    }
    if (input.accountCategory.value === 'apikey' && openAIImagesUrlToB64JsonEnabled.value) {
      extra.images_url_to_b64_json = true
    } else {
      delete extra.images_url_to_b64_json
    }

    return Object.keys(extra).length > 0 ? extra : undefined
  }


  const buildAnthropicExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
    if (input.form.platform !== 'anthropic' || input.accountCategory.value !== 'apikey') {
      return base
    }

    const extra: Record<string, unknown> = { ...(base || {}) }
    if (anthropicPassthroughEnabled.value) {
      extra.anthropic_passthrough = true
    } else {
      delete extra.anthropic_passthrough
    }
    if (anthropicAPIKeyAuthScheme.value === 'authorization_bearer') {
      extra.anthropic_apikey_auth_scheme = 'authorization_bearer'
    } else {
      delete extra.anthropic_apikey_auth_scheme
    }
    if (webSearchEmulationMode.value === 'default') {
      delete extra.web_search_emulation
    } else {
      extra.web_search_emulation = webSearchEmulationMode.value
    }

    return Object.keys(extra).length > 0 ? extra : undefined
  }
  return { explicitCacheUpstreamEnabled, explicitCacheUpstreamMaxTTL, headerOverrideEnabled, headerOverrideRows, validateGrokOAuthUpstreamConfig, applyGrokOAuthUpstreamConfig, interceptWarmupRequests, openaiPassthroughEnabled, openaiFlattenNamespacesEnabled, openAICompactMode, openAIResponsesMode, openAIImagesUrlToB64JsonEnabled, openAIEndpointCapabilities, openaiAPIKeyResponsesWebSocketV2Mode, codexCLIOnlyEnabled, codexCLIOnlyAppServerEnabled, codexFingerprintMode, codexFingerprintModeOptions, anthropicPassthroughEnabled, anthropicAPIKeyAuthScheme, webSearchEmulationMode, webSearchGlobalEnabled, mixedScheduling, allowOverages, openAICompactModeOptions, openAIResponsesModeOptions, openAITextEndpointCapabilityLabel, openAIEndpointCapabilityOptions, openAITextGenerationCapabilityEnabled, normalizeOpenAIEndpointCapabilities, toggleOpenAIEndpointCapability, applyOpenAIEndpointCapabilities, tlsFingerprintEnabled, tlsFingerprintProfileId, tlsFingerprintProfiles, sessionIdMaskingEnabled, cacheTTLOverrideEnabled, cacheTTLOverrideTarget, customBaseUrlEnabled, customBaseUrl, openAIWSModeOptions, openaiResponsesWebSocketV2Mode, openAIWSModeHintKey, buildOpenAIExtra, buildAnthropicExtra }
}
