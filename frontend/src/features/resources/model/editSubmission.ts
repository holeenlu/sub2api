import { applyAccountModelReferences, usesManagedModelReferences } from './accountModelReferences'
import { readApiError, extractApiErrorCode } from '@/shared/lib/apiError'
import { applyCredentialUpdate } from './editCredentialPayload'
import { applyOAuthUpdate } from './editOAuthPayload'
import { applyAnthropicTransportUpdate } from './editAnthropicTransportPayload'
import { applyOpenAITransportUpdate } from './editOpenAITransportPayload'
import { applyQuotaUpdate } from './editQuotaPayload'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isValidBPSRecoveryInterval } from '@/features/resources/model/excelBPSRecovery'
import accountsAPI from '@/features/resources/accounts'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import type { Account, CheckMixedChannelResponse } from '@/features/resources/api/accountTypes';
import type { OpenAICompactMode, OpenAIResponsesMode } from '@/features/resources/api/accountCredentialTypes';
import { type CnApiProtocol, type CnNativeApiProtocol, type HeaderOverrideRow, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import { type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, ModelMapping, CodexFingerprintMode, CodexImageToolMode, AnthropicAPIKeyAuthScheme } from './editEditorTypes'





export interface EditSubmissionInput {
  canManageModels: boolean
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  antigravityMixedChannelConfirmed: Ref<boolean, boolean>
  clearMixedChannelDialog: () => void
  emit: ((event: "close") => void) & ((event: "updated", account: Account) => void) & ((event: "baseline", payload: Record<string, unknown>) => void)
  props: Readonly<Pick<Props, "show" | "allowRateEdit" | "showCost" | "allowTransportEdit" | "saveAccount" | "account" | "allowAuthorize">>
  withAntigravityConfirmFlag: (payload: Record<string, unknown>) => { [x: string]: unknown }
  persistGrokMediaEligibility: (accountID: number, updatedAccount: Account) => Promise<Account>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showSuccess" | "showError">>
  t: ReturnType<typeof useI18n>["t"]
  needsMixedChannelCheck: () => boolean
  openMixedChannelDialog: (opts: { response?: CheckMixedChannelResponse; message?: string; onConfirm: () => Promise<void> }) => void
  form: { name: string; notes: string; proxy_id: number | null; concurrency: number; load_factor: number | null; priority: number; rate_multiplier: number; status: Account["status"]; group_ids: number[]; expires_at: number | null }
  editApiKey: Ref<string, string>
  isSparkShadow: ComputedRef<boolean>
  excelBPSEnabled: Ref<boolean, boolean>
  excelBPSSystemDisabled: ComputedRef<boolean>
  excelBPSStopRequested: Ref<boolean>
  excelBPSSwitchTouched: Ref<boolean>
  excelBPSExpectedDisabledAt: Ref<string | undefined>
  excelBPSAutoDisableOn403: Ref<boolean, boolean>
  excelBPSAutoRecoverOn403: Ref<boolean, boolean>
  excelBPSRecoveryIntervalMinutes: Ref<string | number, string | number>
  excelBPSAutoMoveOn403: Ref<boolean, boolean>
  excelBPS403TargetGroupID: Ref<string | number, string | number>
  excelBPS403GroupOptions: ComputedRef<({ value: string; label: string } | { value: number; label: string })[]>
  autoResetCreditEnabled: Ref<boolean, boolean>
  autoResetCredit5hThreshold: Ref<number, number>
  autoResetCredit7dThreshold: Ref<number, number>
  autoPauseOnExpired: Ref<boolean, boolean>
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
  upstreamBillingRateSyncEnabled: Ref<boolean, boolean>
  editBaseUrl: Ref<string, string>
  defaultBaseUrl: ComputedRef<string>
  openaiPassthroughEnabled: Ref<boolean, boolean>
  isCNApiKeyAccount: ComputedRef<boolean>
  currentOpenCodeOrCNMode: () => string
  editApiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  defaultCNAdaptiveBaseUrls: ReturnType<typeof createProviderCredentialOptions>["defaultCNAdaptiveBaseUrls"]
  adaptivePresetPlatform: ComputedRef<string>
  editAdaptiveProtocolOptions: ComputedRef<{ value: CnNativeApiProtocol; labelKey: string }[]>
  editAdaptiveBaseUrls: Ref<{ chat_completions: string; anthropic: string; responses: string }, Record<CnNativeApiProtocol, string> | { chat_completions: string; anthropic: string; responses: string }>
  providerRoutesByModel: ReturnType<typeof createProviderCredentialOptions>["providerRoutesByModel"]
  editOpenCodeGoProtocolRules: Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>
  editZhipuOrganization: Ref<string, string>
  editZhipuProject: Ref<string, string>
  allowedModels: Ref<string[]>
  modelMappings: Ref<ModelMapping[]>
  buildModelRestrictionMapping: () => Record<string, string> | null
  applyOpenAIEndpointCapabilities: (credentials: Record<string, unknown>) => void
  openAICompactModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  poolModeEnabled: Ref<boolean, boolean>
  normalizePoolModeRetryCount: (value: number) => number
  poolModeRetryCount: Ref<number, number>
  parsePoolModeRetryStatusCodes: (input: string) => number[]
  poolModeRetryStatusCodesInput: Ref<string, string>
  explicitCacheUpstreamEnabled: Ref<boolean, boolean>
  explicitCacheUpstreamMaxTTL: Ref<string | number, string | number>
  customErrorCodesEnabled: Ref<boolean, boolean>
  selectedErrorCodes: Ref<number[], number[]>
  isHeaderOverrideCapable: ReturnType<typeof createProviderCredentialOptions>["isHeaderOverrideCapable"]
  headerOverrideEnabled: Ref<boolean, boolean>
  headerOverrideRows: Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>
  interceptWarmupRequests: Ref<boolean, boolean>
  applyAccountSchedulingThresholdOverridePatch: (credentials: Record<string, unknown>, currentCredentials: Record<string, unknown>, platform?: Account["platform"] | undefined, type?: Account["type"] | undefined) => void
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  isVertexAPIKeyMode: ComputedRef<boolean>
  editVertexApiKey: Ref<string, string>
  editVertexProjectId: Ref<string, string>
  editVertexLocation: Ref<string, string>
  editVertexClientEmail: Ref<string, string>
  editBedrockRegion: Ref<string, string>
  editBedrockForceGlobal: Ref<boolean, boolean>
  isBedrockAPIKeyMode: ComputedRef<boolean>
  editBedrockApiKeyValue: Ref<string, string>
  editBedrockAccessKeyId: Ref<string, string>
  editBedrockSecretAccessKey: Ref<string, string>
  editBedrockSessionToken: Ref<string, string>
  hasOAuthModelRestriction: ComputedRef<boolean>
  applyOpenAIModelMappingCredentials: (credentials: Record<string, unknown>) => void
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  GROK_CLIENT_TOOL_CACHE_EXTRA_KEY: "grok_client_tool_cache_enabled"
  grokClientToolCacheEnabled: Ref<boolean, boolean>
  editPlanType: Ref<string, string>
  antigravityProjectId: Ref<string, string>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  mixedScheduling: Ref<boolean, boolean>
  allowOverages: Ref<boolean, boolean>
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
  anthropicPassthroughEnabled: Ref<boolean, boolean>
  anthropicAPIKeyAuthScheme: Ref<AnthropicAPIKeyAuthScheme, AnthropicAPIKeyAuthScheme>
  webSearchEmulationMode: Ref<string, string>
  cloudEligible: ComputedRef<boolean>
  cloudEnabled: Ref<boolean, boolean>
  excelBPSModels: Ref<string[], string[]>
  excelBPSCacheCreationAsInput: Ref<boolean, boolean>
  excelBPSOmitUnsupportedTools: Ref<boolean, boolean>
  excelBPSIgnoreEncryptedContent: Ref<boolean, boolean>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  openaiAPIKeyResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
  openaiFlattenNamespacesEnabled: Ref<boolean, boolean>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
  openAICompactMode: Ref<OpenAICompactMode, OpenAICompactMode>
  openAITextGenerationCapabilityEnabled: ComputedRef<boolean>
  openAIResponsesMode: Ref<OpenAIResponsesMode, OpenAIResponsesMode>
  openAIImagesUrlToB64JsonEnabled: Ref<boolean, boolean>
  autoPause5hThreshold: Ref<number | null, number | null>
  autoPause7dThreshold: Ref<number | null, number | null>
  autoPause5hDisabled: Ref<boolean, boolean>
  autoPause7dDisabled: Ref<boolean, boolean>
  codexImageToolMode: Ref<CodexImageToolMode, CodexImageToolMode>
  codexCLIOnlyEnabled: Ref<boolean, boolean>
  codexCLIOnlyAppServerEnabled: Ref<boolean, boolean>
  codexFingerprintMode: Ref<CodexFingerprintMode, CodexFingerprintMode>
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
  upstreamRequestIdHeader: Ref<string, string>
  readUpstreamRequestIdHeader: (extra: unknown) => string
  ensureAntigravityMixedChannelConfirmed: (onConfirm: () => Promise<void>) => Promise<boolean>
}





export function useEditSubmissionDraft(input: EditSubmissionInput): {
  bpsStateConflict: Ref<boolean>
  submitting: Ref<boolean, boolean>
  handleClose: () => void
  submitUpdateAccount: (accountID: number, updatePayload: Record<string, unknown>) => Promise<void>
  buildAccountUpdatePayload: () => Record<string, unknown> | undefined
  handleSubmit: () => Promise<void>
} {


  // State
  const submitting = ref(false)
  const bpsStateConflict = ref(false)
  watch(() => [input.props.account, input.props.show, input.props.allowTransportEdit], () => {
    bpsStateConflict.value = false
  }, { flush: 'sync' })


  // Methods
  const handleClose = () => {
    bpsStateConflict.value = false
    input.resourceTasks.invalidate()
    input.antigravityMixedChannelConfirmed.value = false
    input.clearMixedChannelDialog()
    input.emit('close')
  }


  const submissionTarget = () => [
    input.props.account?.id, input.props.account?.platform, input.props.account?.type,
    input.form.proxy_id, input.form.group_ids, input.props.allowAuthorize,
    input.props.allowTransportEdit, input.props.allowRateEdit, input.props.showCost
  ]

  const submitUpdateAccount = async (accountID: number, updatePayload: Record<string, unknown>) => {
    const pending = input.resourceTasks.capture(submissionTarget)
    const saveAccount = input.props.saveAccount ?? accountsAPI.update
    const sameWriter = () => saveAccount === (input.props.saveAccount ?? accountsAPI.update)
    try {

      if (!input.props.allowRateEdit || !input.props.showCost) delete updatePayload.rate_multiplier
      if (!input.props.allowTransportEdit) delete updatePayload.proxy_id
      submitting.value = true
      let accountSaved = false
      try {
        let updatedAccount = await pending.wait(async () => saveAccount(accountID, input.withAntigravityConfirmFlag(updatePayload)))
        accountSaved = true
        if (!sameWriter()) throw new StaleResourceTask()
        updatedAccount = await pending.wait(async () => input.persistGrokMediaEligibility(accountID, updatedAccount))
        input.appStore.showSuccess(input.t('admin.accounts.accountUpdated'))
        input.emit('updated', updatedAccount)
        handleClose()
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (error instanceof StaleResourceTask || !pending.current() || !sameWriter()) throw new StaleResourceTask()

        if (!accountSaved && errorDetails.status === 409 && extractApiErrorCode(error) === 'OPENAI_EXCEL_BPS_STATE_CONFLICT') {
          bpsStateConflict.value = true
          input.appStore.showError(input.t('admin.accounts.openai.excelBPSStateConflict'))
          return
        }

        // Dismissing authorization before the write is a no-op. A later failure
        // must still report the already-saved account's partial outcome.
        if (!accountSaved && (errorDetails.code === 'STEP_UP_CANCELLED' || errorDetails.code === 'ERR_CANCELED')) return
        if (!accountSaved && errorDetails.status === 409 && errorDetails.error === 'mixed_channel_warning' && input.needsMixedChannelCheck()) {
          input.openMixedChannelDialog({
            message: errorDetails.message,
            onConfirm: async () => {
              // Confirmation belongs to the rejected command, never a newly opened editor.
              if (!pending.current() || !sameWriter()) return
              try {

                input.antigravityMixedChannelConfirmed.value = true
                await pending.wait(async () => submitUpdateAccount(accountID, updatePayload))

              } catch (error: unknown) {
                if (error instanceof StaleResourceTask) { return }
                throw error
              }
            }
          })
          return
        }
        input.appStore.showError(errorDetails.message || input.t('admin.accounts.failedToUpdate'))
      } finally {
        if (pending.current()) {

          submitting.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  function applyManagedModelDraft(payload: Record<string, unknown>): void {
    const account = input.props.account
    if (!account || !usesManagedModelReferences(account.platform)) return
    const outgoing = payload.credentials
    if (!input.canManageModels || input.props.allowAuthorize === false) {
      if (typeof outgoing !== 'object' || outgoing === null || Array.isArray(outgoing)) return
      const credentials: Record<string, unknown> = { ...outgoing }
      // Editing unrelated credentials must not migrate or clear references without permission.
      for (const field of ['model_allowlist', 'model_mapping']) {
        if (account.credentials && field in account.credentials) credentials[field] = account.credentials[field]
        else delete credentials[field]
      }
      payload.credentials = credentials
      return
    }
    if (account.platform === 'openai' && input.openaiPassthroughEnabled.value) return
    const credentials: Record<string, unknown> = typeof outgoing === 'object' && outgoing !== null && !Array.isArray(outgoing)
      ? { ...outgoing } : { ...account.credentials }
    applyAccountModelReferences(credentials, account.platform, 'combined', input.allowedModels.value, input.modelMappings.value)
    payload.credentials = credentials
  }

  const buildAccountUpdatePayload = (): Record<string, unknown> | undefined => {
    if (!input.props.account) return
    if (!input.props.allowTransportEdit) {
      const payload: Record<string, unknown> = { name: input.form.name, notes: input.form.notes, concurrency: input.form.concurrency, priority: input.form.priority, load_factor: input.form.load_factor || 0, group_ids: input.form.group_ids }
      if (input.props.allowRateEdit && input.props.showCost && input.form.rate_multiplier !== input.props.account.rate_multiplier) payload.rate_multiplier = input.form.rate_multiplier
      if (input.props.allowAuthorize && input.props.account.type === 'apikey' && input.editApiKey.value.trim()) payload.credentials = { ...input.props.account.credentials, api_key: input.editApiKey.value.trim() }
      applyManagedModelDraft(payload)
      return payload
    }
    if (input.props.account.platform === 'openai' && input.props.account.type === 'oauth' && !input.isSparkShadow.value && (input.excelBPSEnabled.value || input.excelBPSSystemDisabled.value) && input.excelBPSAutoDisableOn403.value && input.excelBPSAutoRecoverOn403.value && !isValidBPSRecoveryInterval(input.excelBPSRecoveryIntervalMinutes.value)) {
      input.appStore.showError(input.t('admin.accounts.openai.excelBPS403RecoveryIntervalInvalid'))
      return
    }
    if (input.props.account.platform === 'openai' && input.props.account.type === 'oauth' && !input.isSparkShadow.value && input.excelBPSEnabled.value && input.excelBPSAutoMoveOn403.value) {
      const target = Number(input.excelBPS403TargetGroupID.value)
      if (input.excelBPS403TargetGroupID.value === '' || !Number.isSafeInteger(target) || target < 0 ||
        !input.excelBPS403GroupOptions.value.some(option => option.value === target)) {
        input.appStore.showError(input.t('admin.accounts.openai.excelBPS403SelectTarget'))
        return
      }
    }

    if (input.form.status !== 'active' && input.form.status !== 'inactive' && input.form.status !== 'error' && input.form.status !== 'disabled') {
      input.appStore.showError(input.t('admin.accounts.pleaseSelectStatus'))
      return
    }
    if (input.autoResetCreditEnabled.value) {
      const thresholds = [input.autoResetCredit5hThreshold.value, input.autoResetCredit7dThreshold.value]
      if (thresholds.some((value) => !Number.isFinite(value) || value < 0.1 || value > 100)) {
        input.appStore.showError(input.t('admin.accounts.autoResetCredit.thresholdInvalid'))
        return
      }
    }

    const updatePayload: Record<string, unknown> = { ...input.form }
    try {
      // 后端期望 proxy_id: 0 表示清除代理，而不是 null
      if (updatePayload.proxy_id === null) {
        updatePayload.proxy_id = 0
      }
      if (input.form.expires_at === null) {
        updatePayload.expires_at = 0
      }
      // load_factor: 空值/NaN/0/负数 时发送 0（后端约定 <= 0 = 清除）
      const lf = input.form.load_factor
      if (lf == null || Number.isNaN(lf) || lf <= 0) {
        updatePayload.load_factor = 0
      }
      updatePayload.auto_pause_on_expired = input.autoPauseOnExpired.value
      if (input.props.account.type === 'apikey') {
        updatePayload.upstream_billing_probe_enabled = input.upstreamBillingAutoProbeEnabled.value
        updatePayload.upstream_billing_rate_sync_enabled = input.upstreamBillingRateSyncEnabled.value
        if (input.upstreamBillingRateSyncEnabled.value) {
          delete updatePayload.rate_multiplier
        }
      }

      // For apikey type, handle credentials update
      if (!applyCredentialUpdate(input, input.props.account, updatePayload)) return

      // OAuth / setup-token: persist model mapping to credentials
      if (!applyOAuthUpdate(input, input.props.account, updatePayload)) return

      applyManagedModelDraft(updatePayload)

      // For Anthropic OAuth/SetupToken accounts, handle quota control settings in extra
      if (!applyAnthropicTransportUpdate(input, input.props.account, updatePayload)) return

      // For OpenAI OAuth/SetupToken/API Key accounts, handle passthrough mode in extra
      if (!applyOpenAITransportUpdate(input, input.props.account, updatePayload)) return

      // For apikey/bedrock/service_account accounts, handle quota_limit in extra
      if (!applyQuotaUpdate(input, input.props.account, updatePayload)) return

      // 上游ID头名只在改动时写回 extra，避免用弹窗打开时的快照覆盖运行态键。
      const nextUpstreamRequestIdHeader = input.upstreamRequestIdHeader.value.trim()
      if (nextUpstreamRequestIdHeader !== input.readUpstreamRequestIdHeader(input.props.account.extra)) {
        const currentExtra = (updatePayload.extra as Record<string, unknown>) || (input.props.account.extra as Record<string, unknown>) || {}
        const newExtra: Record<string, unknown> = { ...currentExtra }
        if (nextUpstreamRequestIdHeader) {
          newExtra.upstream_request_id_header = nextUpstreamRequestIdHeader
        } else {
          delete newExtra.upstream_request_id_header
        }
        updatePayload.extra = newExtra
      }

      return updatePayload
    } catch (error: unknown) {
      const errorDetails = readApiError(error)
      input.appStore.showError(errorDetails.message || input.t('admin.accounts.failedToUpdate'))
    }
  }


  const handleSubmit = async () => {
    if (bpsStateConflict.value) return
    const pending = input.resourceTasks.capture(submissionTarget)
    const saveAccount = input.props.saveAccount ?? accountsAPI.update
    try {

      const payload = buildAccountUpdatePayload()
      if (!payload || !input.props.account) return
      const accountID = input.props.account.id
      const send = async () => {
        if (saveAccount !== (input.props.saveAccount ?? accountsAPI.update)) throw new StaleResourceTask()
        await pending.wait(() => submitUpdateAccount(accountID, payload))
      }
      const ready = await pending.wait(async () => input.ensureAntigravityMixedChannelConfirmed(send))
      if (ready) await send()

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { bpsStateConflict, submitting, handleClose, submitUpdateAccount, buildAccountUpdatePayload, handleSubmit }
}
