import { isBPSSystemDisabled, readBPSRecoveryPresentation, readBPSRecoveryDisabledAt } from './bpsRecoveryPresentation'
import { readApiError } from '@/shared/lib/apiError'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isOpenAICloudAccount } from '@/features/resources/model/openaiCloud'
import { DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES } from '@/features/resources/model/excelBPSRecovery'
import accountsAPI from '@/features/resources/accounts'
import type { Account } from '@/features/resources/api/accountTypes';
import type { OpenAICompactMode, OpenAIResponsesMode, OpenAIEndpointCapability, GrokMediaEligibilityMode, GrokMediaEligibilityState } from '@/features/resources/api/accountCredentialTypes';
import { type HeaderOverrideRow } from "@/features/resources/model/credentialsBuilder"
import { OPENAI_WS_MODE_CTX_POOL, OPENAI_WS_MODE_OFF, OPENAI_WS_MODE_PASSTHROUGH, OPENAI_WS_MODE_HTTP_BRIDGE, resolveOpenAIWSModeHintKey, type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref, ComputedRef, WritableComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, CodexFingerprintMode, CodexImageToolMode, AnthropicAPIKeyAuthScheme } from './editEditorTypes'




interface EditTransportInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  props: Readonly<Pick<Props, "account" | "supplierScope" | "show" | "groups" | "allowTransportEdit" | "captureBaseline" | "saveAccount">>
  isHeaderOverrideCapable: ReturnType<typeof createProviderCredentialOptions>["isHeaderOverrideCapable"]
  isGrokOAuthAccount: ComputedRef<boolean>
  t: ReturnType<typeof useI18n>["t"]
  authStore: Readonly<Pick<ResourceEditorServices['access'], "isSuperAdmin" | "isSimpleMode">>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  resourceServices: Readonly<Pick<ResourceEditorServices, "scopeKey" | "tlsProfiles">>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
}




export function useEditTransportDraft(input: EditTransportInput): {
  explicitCacheUpstreamEnabled: Ref<boolean, boolean>
  explicitCacheUpstreamMaxTTL: Ref<string | number, string | number>
  headerOverrideEnabled: Ref<boolean, boolean>
  headerOverrideRows: Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>
  headerOverrideCapable: ComputedRef<boolean>
  grokClientToolCacheEnabled: Ref<boolean, boolean>
  grokMediaEligibilityMode: Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>
  grokMediaEligibilityInitialMode: Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>
  grokMediaEligibilityState: Ref<{ account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null, GrokMediaEligibilityState | { account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null>
  grokMediaEligibilityLoading: Ref<boolean, boolean>
  grokMediaEligibilityError: Ref<string, string>
  grokMediaEligibilityRequestVersion: Ref<number>
  modeFromGrokMediaExtra: (extra: Record<string, unknown> | undefined) => GrokMediaEligibilityMode
  loadGrokMediaEligibility: (accountID: number) => Promise<GrokMediaEligibilityState | null>
  interceptWarmupRequests: Ref<boolean, boolean>
  mixedScheduling: Ref<boolean, boolean>
  allowOverages: Ref<boolean, boolean>
  tlsFingerprintEnabled: Ref<boolean, boolean>
  tlsFingerprintProfileId: Ref<number | null, number | null>
  tlsFingerprintProfiles: Ref<{ id: number; name: string }[], { id: number; name: string }[] | { id: number; name: string }[]>
  sessionIdMaskingEnabled: Ref<boolean, boolean>
  cacheTTLOverrideEnabled: Ref<boolean, boolean>
  cacheTTLOverrideTarget: Ref<string, string>
  customBaseUrlEnabled: Ref<boolean, boolean>
  customBaseUrl: Ref<string, string>
  cloudEligible: ComputedRef<boolean>
  cloudEnabled: Ref<boolean, boolean>
  excelBPSEnabled: Ref<boolean, boolean>
  excelBPSCacheCreationAsInput: Ref<boolean, boolean>
  excelBPSAutoDisableOn403: Ref<boolean, boolean>
  excelBPSAutoRecoverOn403: Ref<boolean, boolean>
  excelBPSRecoveryIntervalMinutes: Ref<string | number, string | number>
  excelBPSSystemDisabled: ComputedRef<boolean>
  excelBPSStopRequested: Ref<boolean>
  excelBPSSwitchTouched: Ref<boolean>
  excelBPSExpectedDisabledAt: Ref<string | undefined>
  canStopBPSRecovery: ComputedRef<boolean>
  requestBPSStop: () => void
  setBPSEnabled: (enabled: boolean) => void
  excelBPSOmitUnsupportedTools: Ref<boolean, boolean>
  excelBPSIgnoreEncryptedContent: Ref<boolean, boolean>
  excelBPSAutoMoveOn403: Ref<boolean, boolean>
  excelBPS403TargetGroupID: Ref<string | number, string | number>
  excelBPS403GroupOptions: ComputedRef<({ value: string; label: string } | { value: number; label: string })[]>
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
  codexImageToolMode: Ref<CodexImageToolMode, CodexImageToolMode>
  anthropicPassthroughEnabled: Ref<boolean, boolean>
  anthropicAPIKeyAuthScheme: Ref<AnthropicAPIKeyAuthScheme, AnthropicAPIKeyAuthScheme>
  webSearchEmulationMode: Ref<string, string>
  webSearchGlobalEnabled: Ref<boolean, boolean>
  codexFingerprintModeOptions: ComputedRef<{ value: CodexFingerprintMode; label: string }[]>
  openAIWSModeOptions: ComputedRef<{ value: string; label: string }[]>
  openaiResponsesWebSocketV2Mode: WritableComputedRef<OpenAIWSMode, OpenAIWSMode>
  openAIWSModeHintKey: ComputedRef<"admin.accounts.openai.wsModeCtxPoolHint" | "admin.accounts.openai.wsModePassthroughHint" | "admin.accounts.openai.wsModeHttpBridgeHint" | null>
  codexImageToolOptions: ComputedRef<{ value: CodexImageToolMode; label: string; description: string; selectedCardClass: string; selectedDotClass: string }[]>
  codexImageToolBadgeLabel: ComputedRef<string>
  codexImageToolBadgeClass: ComputedRef<"bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300" | "bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300" | "bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300" | "bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-300">
  openAICompactModeOptions: ComputedRef<{ value: string; label: string }[]>
  openAIResponsesModeOptions: ComputedRef<{ value: string; label: string }[]>
  openAITextEndpointCapabilityLabel: ComputedRef<string>
  openAIEndpointCapabilityOptions: ComputedRef<{ value: OpenAIEndpointCapability; label: string }[]>
  openAITextGenerationCapabilityEnabled: ComputedRef<boolean>
  normalizeOpenAIEndpointCapabilities: (values: OpenAIEndpointCapability[]) => OpenAIEndpointCapability[]
  readOpenAIEndpointCapabilities: (credentials?: Record<string, unknown>) => OpenAIEndpointCapability[]
  toggleOpenAIEndpointCapability: (capability: OpenAIEndpointCapability, event?: Event) => void
  applyOpenAIEndpointCapabilities: (credentials: Record<string, unknown>) => void
  normalizeOpenAIResponsesMode: (mode: unknown) => OpenAIResponsesMode
  openAIResponsesStatusKey: ComputedRef<"admin.accounts.openai.responsesStatusForcedResponses" | "admin.accounts.openai.responsesStatusForcedChatCompletions" | "admin.accounts.openai.responsesStatusAutoSupported" | "admin.accounts.openai.responsesStatusAutoUnsupported" | "admin.accounts.openai.responsesStatusAutoUnknown">
  openAICompactStatusKey: ComputedRef<"" | "admin.accounts.openai.compactSupported" | "admin.accounts.openai.compactUnsupported" | "admin.accounts.openai.compactAuto">
  loadTLSProfiles: () => Promise<void>
  persistGrokMediaEligibility: (accountID: number, updatedAccount: Account) => Promise<Account>
} {

  const explicitCacheUpstreamEnabled = ref(false)

  const explicitCacheUpstreamMaxTTL = ref<string | number>('')

  const headerOverrideEnabled = ref(false)

  const headerOverrideRows = ref<HeaderOverrideRow[]>([])


  const headerOverrideCapable = computed(
    () => !!input.props.account && input.isHeaderOverrideCapable(input.props.account.platform, input.props.account.type)
  )

  // Grok Free OAuth accounts use client-tool prompt caching by default. Keep an
  // explicit false in the account extra as the opt-out signal.
  const grokClientToolCacheEnabled = ref(true)

  const grokMediaEligibilityMode = ref<GrokMediaEligibilityMode>('auto')

  const grokMediaEligibilityInitialMode = ref<GrokMediaEligibilityMode>('auto')

  const grokMediaEligibilityState = ref<GrokMediaEligibilityState | null>(null)

  const grokMediaEligibilityLoading = ref(false)

  const grokMediaEligibilityError = ref('')

  const grokMediaEligibilityRequestVersion = ref(0)


  const modeFromGrokMediaExtra = (extra: Record<string, unknown> | undefined): GrokMediaEligibilityMode => {
    if (extra?.grok_media_eligible === true) return 'enabled'
    if (extra?.grok_media_eligible === false) return 'disabled'
    return 'auto'
  }


  const loadGrokMediaEligibility = async (accountID: number): Promise<GrokMediaEligibilityState | null> => {
    const pending = input.resourceTasks.capture()
    try {

      if (!input.isGrokOAuthAccount.value || typeof accountsAPI.getGrokMediaEligibility !== 'function') {
        return null
      }
      const modeAtStart = grokMediaEligibilityMode.value
      const requestVersion = ++grokMediaEligibilityRequestVersion.value
      grokMediaEligibilityLoading.value = true
      grokMediaEligibilityError.value = ''
      try {
        const state = await pending.wait(async () => accountsAPI.getGrokMediaEligibility(accountID))
        if (requestVersion !== grokMediaEligibilityRequestVersion.value) return null
        grokMediaEligibilityState.value = state
        if (grokMediaEligibilityMode.value === modeAtStart) grokMediaEligibilityMode.value = state.mode
        grokMediaEligibilityInitialMode.value = state.mode
        return state
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        if (requestVersion !== grokMediaEligibilityRequestVersion.value) return null
        grokMediaEligibilityError.value = errorDetails?.message || input.t('admin.accounts.grokMediaEligibility.loadFailed')
        return null
      } finally {
        if (pending.current()) {

          if (requestVersion === grokMediaEligibilityRequestVersion.value) {
            grokMediaEligibilityLoading.value = false
          }

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return null }
      throw error
    }
  }


  const interceptWarmupRequests = ref(false)

  const mixedScheduling = ref(false)

  const allowOverages = ref(false)

  const tlsFingerprintEnabled = ref(false)

  const tlsFingerprintProfileId = ref<number | null>(null)

  const tlsFingerprintProfiles = ref<{ id: number; name: string }[]>([])

  const sessionIdMaskingEnabled = ref(false)

  const cacheTTLOverrideEnabled = ref(false)

  const cacheTTLOverrideTarget = ref<string>('5m')

  const customBaseUrlEnabled = ref(false)

  const customBaseUrl = ref('')


  // OpenAI 自动透传开关（OAuth/API Key）
  const cloudEligible = computed(() => !input.props.supplierScope && isOpenAICloudAccount(input.props.account))

  const cloudEnabled = ref(false)

  const excelBPSEnabled = ref(false)


  const excelBPSCacheCreationAsInput = ref(false)

  const excelBPSAutoDisableOn403 = ref(false)

  const excelBPSAutoRecoverOn403 = ref(false)

  const excelBPSRecoveryIntervalMinutes = ref<number | string>(DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES)

  const excelBPSSystemDisabled = computed(() => {
    const extra = input.props.account?.extra
    if (extra?.openai_excel_bps === true) return false
    return isBPSSystemDisabled(readBPSRecoveryPresentation(input.props.account?.excel_bps_403_recovery))
  })

  const excelBPSOmitUnsupportedTools = ref(false)

  const excelBPSIgnoreEncryptedContent = ref(false)

  const excelBPSAutoMoveOn403 = ref(false)

  const excelBPS403TargetGroupID = ref<number | string>('')

  // This is an unsaved editor intent, never a routing or server recovery state.
  const excelBPSStopRequested = ref(false)
  const excelBPSSwitchTouched = ref(false)
  const excelBPSExpectedDisabledAt = ref<string>()
  const canEditBPS = () => Boolean(input.props.show && input.props.allowTransportEdit !== false &&
    input.resourceServices.scopeKey() && input.props.account?.platform === 'openai' &&
    input.props.account.type === 'oauth' && input.props.account.parent_account_id == null)
  const canStopBPSRecovery = computed(() => canEditBPS() && !input.props.supplierScope &&
    !input.props.captureBaseline && !excelBPSEnabled.value && excelBPSSystemDisabled.value)
  function requestBPSStop(): void {
    if (!canStopBPSRecovery.value) return
    excelBPSStopRequested.value = true
    excelBPSSwitchTouched.value = true
    excelBPSExpectedDisabledAt.value = undefined
  }
  function setBPSEnabled(enabled: boolean): void {
    if (!canEditBPS()) return
    excelBPSSwitchTouched.value = true
    excelBPSExpectedDisabledAt.value = enabled && input.props.account?.extra?.openai_excel_bps !== true
      ? readBPSRecoveryDisabledAt(input.props.account?.excel_bps_403_recovery) : undefined
    excelBPSEnabled.value = enabled
    excelBPSStopRequested.value = !enabled
  }
  watch(() => [input.props.show, input.props.account, input.props.allowTransportEdit,
    input.props.supplierScope, input.props.captureBaseline, input.authStore.isSuperAdmin,
    input.resourceServices.scopeKey(), input.props.saveAccount ?? accountsAPI.update], () => {
    excelBPSStopRequested.value = false
    excelBPSSwitchTouched.value = false
    excelBPSExpectedDisabledAt.value = undefined
    excelBPSEnabled.value = input.props.account?.extra?.openai_excel_bps === true
  }, { flush: 'sync' })
  watch(excelBPSEnabled, enabled => {
    if (enabled) excelBPSStopRequested.value = false
  }, { flush: 'sync' })

  const excelBPS403GroupOptions = computed(() => [
    { value: '', label: input.t('admin.accounts.openai.excelBPS403SelectTarget') },
    { value: 0, label: input.t('admin.accounts.openai.excelBPS403LeaveAllGroups') },
    ...input.props.groups
      .filter(group => group.platform === 'openai' || (!input.authStore.isSimpleMode && group.platform === 'composite'))
      .map(group => ({ value: group.id, label: group.name }))
  ])

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

  const codexImageToolMode = ref<CodexImageToolMode>('inherit')

  const anthropicPassthroughEnabled = ref(false)

  const anthropicAPIKeyAuthScheme = ref<AnthropicAPIKeyAuthScheme>('x_api_key')

  const webSearchEmulationMode = ref('default')

  const webSearchGlobalEnabled = ref(false)

  const codexFingerprintModeOptions = computed(() => [
    { value: 'off' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintOff') },
    { value: 'device' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintDevice') },
    { value: 'session' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintSession') },
    { value: 'full' as CodexFingerprintMode, label: input.t('admin.accounts.openai.codexFingerprintFull') },
  ])


  const openAIWSModeOptions = computed(() => [
    { value: OPENAI_WS_MODE_OFF, label: input.t('admin.accounts.openai.wsModeOff') },
    { value: OPENAI_WS_MODE_CTX_POOL, label: input.t('admin.accounts.openai.wsModeCtxPool') },
    { value: OPENAI_WS_MODE_PASSTHROUGH, label: input.t('admin.accounts.openai.wsModePassthrough') },
    { value: OPENAI_WS_MODE_HTTP_BRIDGE, label: input.t('admin.accounts.openai.wsModeHttpBridge') }
  ])

  const openaiResponsesWebSocketV2Mode = computed({
    get: () => {
      if (input.props.account?.type === 'apikey') {
        return openaiAPIKeyResponsesWebSocketV2Mode.value
      }
      return input.openaiOAuthResponsesWebSocketV2Mode.value
    },
    set: (mode: OpenAIWSMode) => {
      if (input.props.account?.type === 'apikey') {
        openaiAPIKeyResponsesWebSocketV2Mode.value = mode
        return
      }
      input.openaiOAuthResponsesWebSocketV2Mode.value = mode
    }
  })

  const openAIWSModeHintKey = computed(() =>
    resolveOpenAIWSModeHintKey(openaiResponsesWebSocketV2Mode.value)
  )

  const codexImageToolOptions = computed<Array<{
    value: CodexImageToolMode
    label: string
    description: string
    selectedCardClass: string
    selectedDotClass: string
  }>>(() => [
    {
      value: 'inherit',
      label: input.t('admin.accounts.openai.codexImageToolInherit'),
      description: input.t('admin.accounts.openai.codexImageToolInheritDesc'),
      selectedCardClass: 'border-sky-300 bg-sky-50 text-sky-900 shadow-sm ring-1 ring-sky-200 dark:border-sky-700 dark:bg-sky-900/25 dark:text-sky-100 dark:ring-sky-800',
      selectedDotClass: 'border-sky-500 bg-sky-500 text-white'
    },
    {
      value: 'enabled',
      label: input.t('admin.accounts.openai.codexImageToolEnabled'),
      description: input.t('admin.accounts.openai.codexImageToolEnabledDesc'),
      selectedCardClass: 'border-emerald-300 bg-emerald-50 text-emerald-900 shadow-sm ring-1 ring-emerald-200 dark:border-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-100 dark:ring-emerald-800',
      selectedDotClass: 'border-emerald-500 bg-emerald-500 text-white'
    },
    {
      value: 'disabled',
      label: input.t('admin.accounts.openai.codexImageToolDisabled'),
      description: input.t('admin.accounts.openai.codexImageToolDisabledDesc'),
      selectedCardClass: 'border-amber-300 bg-amber-50 text-amber-900 shadow-sm ring-1 ring-amber-200 dark:border-amber-700 dark:bg-amber-900/25 dark:text-amber-100 dark:ring-amber-800',
      selectedDotClass: 'border-amber-500 bg-amber-500 text-white'
    },
    {
      value: 'block',
      label: input.t('admin.accounts.openai.codexImageToolBlock'),
      description: input.t('admin.accounts.openai.codexImageToolBlockDesc'),
      selectedCardClass: 'border-rose-300 bg-rose-50 text-rose-900 shadow-sm ring-1 ring-rose-200 dark:border-rose-700 dark:bg-rose-900/25 dark:text-rose-100 dark:ring-rose-800',
      selectedDotClass: 'border-rose-500 bg-rose-500 text-white'
    }
  ])

  const codexImageToolBadgeLabel = computed(() => {
    switch (codexImageToolMode.value) {
      case 'enabled':
        return input.t('admin.accounts.openai.codexImageToolBadgeEnabled')
      case 'disabled':
        return input.t('admin.accounts.openai.codexImageToolBadgeDisabled')
      case 'block':
        return input.t('admin.accounts.openai.codexImageToolBadgeBlock')
      default:
        return input.t('admin.accounts.openai.codexImageToolBadgeInherit')
    }
  })

  const codexImageToolBadgeClass = computed(() => {
    switch (codexImageToolMode.value) {
      case 'enabled':
        return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
      case 'disabled':
        return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
      case 'block':
        return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
      default:
        return 'bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-300'
    }
  })

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
    const extra = input.props.account?.extra as Record<string, unknown> | undefined
    if (extra?.openai_responses_supported === true) {
      return input.t('admin.accounts.openai.capabilityResponsesAuto')
    }
    if (extra?.openai_responses_supported === false) {
      return input.t('admin.accounts.openai.capabilityChatCompletionsAuto')
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


  const readOpenAIEndpointCapabilities = (credentials?: Record<string, unknown>): OpenAIEndpointCapability[] => {
    const raw = credentials?.openai_capabilities
    if (Array.isArray(raw)) {
      return normalizeOpenAIEndpointCapabilities(
        raw.filter((value): value is OpenAIEndpointCapability =>
          value === 'chat_completions' || value === 'embeddings' || value === 'seedance'
        )
      )
    }
    if (raw !== null && typeof raw === 'object') {
      const capabilityMap = raw as Record<string, unknown>
      return normalizeOpenAIEndpointCapabilities(
        openAIEndpointCapabilityOptions.value
          .map((option) => option.value)
          .filter((value) => capabilityMap[value] === true)
      )
    }
    return ['chat_completions', 'embeddings']
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

  const normalizeOpenAIResponsesMode = (mode: unknown): OpenAIResponsesMode => {
    if (mode === 'force_responses' || mode === 'force_chat_completions') {
      return mode
    }
    return 'auto'
  }

  const openAIResponsesStatusKey = computed(() => {
    if (openAIResponsesMode.value === 'force_responses') {
      return 'admin.accounts.openai.responsesStatusForcedResponses'
    }
    if (openAIResponsesMode.value === 'force_chat_completions') {
      return 'admin.accounts.openai.responsesStatusForcedChatCompletions'
    }
    const extra = input.props.account?.extra as Record<string, unknown> | undefined
    if (extra?.openai_responses_supported === true) {
      return 'admin.accounts.openai.responsesStatusAutoSupported'
    }
    if (extra?.openai_responses_supported === false) {
      return 'admin.accounts.openai.responsesStatusAutoUnsupported'
    }
    return 'admin.accounts.openai.responsesStatusAutoUnknown'
  })

  const openAICompactStatusKey = computed(() => {
    const extra = input.props.account?.extra as Record<string, unknown> | undefined
    if (!input.props.account || input.props.account.platform !== 'openai') return ''
    const mode = typeof extra?.openai_compact_mode === 'string' ? extra.openai_compact_mode : 'auto'
    if (mode === 'force_on') return 'admin.accounts.openai.compactSupported'
    if (mode === 'force_off') return 'admin.accounts.openai.compactUnsupported'
    if (typeof extra?.openai_compact_supported === 'boolean') {
      return extra.openai_compact_supported
        ? 'admin.accounts.openai.compactSupported'
        : 'admin.accounts.openai.compactUnsupported'
    }
    return 'admin.accounts.openai.compactAuto'
  })


  async function loadTLSProfiles() {
    const pending = input.resourceTasks.capture()
    try {

      try {
        if (!input.props.allowTransportEdit) return
        const profiles = await pending.wait(async () => input.resourceServices.tlsProfiles())
        tlsFingerprintProfiles.value = profiles.map(p => ({ id: p.id, name: p.name }))
      } catch {
        if (!pending.current()) throw new StaleResourceTask()

        tlsFingerprintProfiles.value = []
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const persistGrokMediaEligibility = async (accountID: number, updatedAccount: Account): Promise<Account> => {
    const pending = input.resourceTasks.capture()
    try {

      if (
        !input.isGrokOAuthAccount.value ||
        grokMediaEligibilityMode.value === grokMediaEligibilityInitialMode.value ||
        typeof accountsAPI.updateGrokMediaEligibility !== 'function'
      ) {
        return updatedAccount
      }

      try {
        const state = await pending.wait(async () => accountsAPI.updateGrokMediaEligibility(accountID, grokMediaEligibilityMode.value))
        grokMediaEligibilityState.value = state
        grokMediaEligibilityInitialMode.value = state.mode
        const nextExtra = { ...((updatedAccount.extra as Record<string, unknown> | undefined) || {}) }
        if (state.mode === 'auto') {
          delete nextExtra.grok_media_eligible
        } else {
          nextExtra.grok_media_eligible = state.mode === 'enabled'
        }
        updatedAccount.extra = nextExtra
      } catch (error: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        input.appStore.showError(input.t('admin.accounts.grokMediaEligibility.partialSave'))
        try {
          const state = await pending.wait(async () => loadGrokMediaEligibility(accountID))
          if (state) {
            const nextExtra = { ...((updatedAccount.extra as Record<string, unknown> | undefined) || {}) }
            if (state.mode === 'auto') delete nextExtra.grok_media_eligible
            else nextExtra.grok_media_eligible = state.mode === 'enabled'
            updatedAccount.extra = nextExtra
          }
        } catch {
          if (!pending.current()) throw new StaleResourceTask()

          // The original save result remains useful even when the refresh fails.
        }
      }
      return updatedAccount

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { throw error }
      throw error
    }
  }
  return { explicitCacheUpstreamEnabled, explicitCacheUpstreamMaxTTL, headerOverrideEnabled, headerOverrideRows, headerOverrideCapable, grokClientToolCacheEnabled, grokMediaEligibilityMode, grokMediaEligibilityInitialMode, grokMediaEligibilityState, grokMediaEligibilityLoading, grokMediaEligibilityError, grokMediaEligibilityRequestVersion, modeFromGrokMediaExtra, loadGrokMediaEligibility, interceptWarmupRequests, mixedScheduling, allowOverages, tlsFingerprintEnabled, tlsFingerprintProfileId, tlsFingerprintProfiles, sessionIdMaskingEnabled, cacheTTLOverrideEnabled, cacheTTLOverrideTarget, customBaseUrlEnabled, customBaseUrl, cloudEligible, cloudEnabled, excelBPSEnabled, excelBPSCacheCreationAsInput, excelBPSAutoDisableOn403, excelBPSAutoRecoverOn403, excelBPSRecoveryIntervalMinutes, excelBPSSystemDisabled, excelBPSStopRequested, excelBPSSwitchTouched, excelBPSExpectedDisabledAt, canStopBPSRecovery, requestBPSStop, setBPSEnabled, excelBPSOmitUnsupportedTools, excelBPSIgnoreEncryptedContent, excelBPSAutoMoveOn403, excelBPS403TargetGroupID, excelBPS403GroupOptions, openaiPassthroughEnabled, openaiFlattenNamespacesEnabled, openAICompactMode, openAIResponsesMode, openAIImagesUrlToB64JsonEnabled, openAIEndpointCapabilities, openaiAPIKeyResponsesWebSocketV2Mode, codexCLIOnlyEnabled, codexCLIOnlyAppServerEnabled, codexFingerprintMode, codexImageToolMode, anthropicPassthroughEnabled, anthropicAPIKeyAuthScheme, webSearchEmulationMode, webSearchGlobalEnabled, codexFingerprintModeOptions, openAIWSModeOptions, openaiResponsesWebSocketV2Mode, openAIWSModeHintKey, codexImageToolOptions, codexImageToolBadgeLabel, codexImageToolBadgeClass, openAICompactModeOptions, openAIResponsesModeOptions, openAITextEndpointCapabilityLabel, openAIEndpointCapabilityOptions, openAITextGenerationCapabilityEnabled, normalizeOpenAIEndpointCapabilities, readOpenAIEndpointCapabilities, toggleOpenAIEndpointCapability, applyOpenAIEndpointCapabilities, normalizeOpenAIResponsesMode, openAIResponsesStatusKey, openAICompactStatusKey, loadTLSProfiles, persistGrokMediaEligibility }
}
