import { readApiError } from '@/shared/lib/apiError'
import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { useI18n } from 'vue-i18n'
import { applyAccountModelReferences } from './accountModelReferences'
import accountsAPI from '@/features/resources/accounts'
import { type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import { useOpenAIOAuth } from '@/features/resources/queries/useOpenAIOAuth'
import type { CodexSessionImportMessage } from '@/features/resources/api/accountImportTypes';
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { OAuthFlowExposed, Props, ModelMapping } from './createEditorTypes'




interface CreateOpenAIAuthorizationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  buildOpenAIExtra: (base?: Record<string, unknown>) => Record<string, unknown> | undefined
  openAILongContextBillingTouched: Ref<boolean, boolean>
  openaiOAuth: ReturnType<typeof useOpenAIOAuth>
  oauthFlowRef: Ref<{ authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null, OAuthFlowExposed | { authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null>
  t: ReturnType<typeof useI18n>["t"]
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError" | "showSuccess" | "showWarning">>
  form: Pick<CreateResourceForm, "proxy_id" | "platform" | "name" | "notes" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  isOpenAIModelRestrictionDisabled: ComputedRef<boolean>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  allowedModels: Ref<string[], string[]>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  buildOpenAICompactModelMapping: () => Record<string, string> | null
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  props: Readonly<Pick<Props, "allowRateEdit">>
  autoPauseOnExpired: Ref<boolean, boolean>
  emit: ((event: "close") => void) & ((event: "created") => void)
  handleClose: () => void
}




export function useCreateOpenAIAuthorizationDraft(input: CreateOpenAIAuthorizationInput): {
  buildOpenAICodexImportExtra: () => Record<string, unknown> | undefined
  handleOpenAIExchange: (authCode: string) => Promise<void>
  OPENAI_MOBILE_RT_CLIENT_ID: "app_LlGpXReQgckcGGUo2JrYvtJK"
  buildOpenAICodexImportCredentialExtras: () => Record<string, unknown> | null
  formatCodexImportMessages: (messages?: CodexSessionImportMessage[]) => string
  isAgentIdentityImportContent: (content: string) => boolean
  handleOpenAIImportCodexSession: (content: string) => Promise<void>
  handleOpenAIImportCodexPAT: (accessToken: string) => Promise<void>
  handleOpenAIBatchRT: (refreshTokenInput: string, clientId?: string) => Promise<void>
  handleOpenAIValidateRT: (rt: string) => Promise<void>
  handleOpenAIValidateMobileRT: (rt: string) => Promise<void>
} {


  const buildOpenAICodexImportExtra = (): Record<string, unknown> | undefined => {
    const extra = input.buildOpenAIExtra()
    if (!extra) {
      return undefined
    }
    if (!input.openAILongContextBillingTouched.value) {
      delete extra.openai_long_context_billing_enabled
    }
    return Object.keys(extra).length > 0 ? extra : undefined
  }


  // OpenAI OAuth 授权码兑换
  const handleOpenAIExchange = async (authCode: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      const oauthClient = input.openaiOAuth
      if (!authCode.trim() || !oauthClient.sessionId.value) return

      oauthClient.loading.value = true
      oauthClient.error.value = ''

      try {
        const stateToUse = (input.oauthFlowRef.value?.oauthState || oauthClient.oauthState.value || '').trim()
        if (!stateToUse) {
          oauthClient.error.value = input.t('admin.accounts.oauth.authFailed')
          input.appStore.showError(oauthClient.error.value)
          return
        }

        const tokenInfo = await pending.wait(async () => oauthClient.exchangeAuthCode(
          authCode.trim(),
          oauthClient.sessionId.value,
          stateToUse,
          input.form.proxy_id
        ))
        if (!tokenInfo) return

        const credentials = oauthClient.buildCredentials(tokenInfo)
        const oauthExtra = oauthClient.buildExtraInfo(tokenInfo) as Record<string, unknown> | undefined
        const extra = input.buildOpenAIExtra(oauthExtra)
        const shouldCreateOpenAI = input.form.platform === 'openai'

        // Add model mapping for OpenAI OAuth accounts（透传模式下不应用）
        if (shouldCreateOpenAI && !input.isOpenAIModelRestrictionDisabled.value) {
          applyAccountModelReferences(credentials, 'openai', input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
        }
        if (shouldCreateOpenAI) {
          const compactModelMapping = input.buildOpenAICompactModelMapping()
          if (compactModelMapping) {
            credentials.compact_model_mapping = compactModelMapping
          }
        }

        // 应用临时不可调度配置
        if (!input.applyTempUnschedConfig(credentials)) {
          return
        }

        if (shouldCreateOpenAI) {
          await pending.wait(async () => accountsAPI.create({
            name: input.form.name,
            notes: input.form.notes,
            platform: 'openai',
            type: 'oauth',
            credentials,
            extra: input.withAccountExtra(extra),
            proxy_id: input.form.proxy_id,
            concurrency: input.form.concurrency,
            load_factor: input.form.load_factor ?? undefined,
            priority: input.form.priority,
            rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
            group_ids: input.form.group_ids,
            expires_at: input.form.expires_at,
            auto_pause_on_expired: input.autoPauseOnExpired.value
          }))
          input.appStore.showSuccess(input.t('admin.accounts.accountCreated'))
        }

        input.emit('created')
        input.handleClose()
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        oauthClient.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
        input.appStore.showError(oauthClient.error.value)
      } finally {
        if (pending.current()) {

          oauthClient.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // OpenAI 手动 RT 批量验证和创建
  // OpenAI Mobile RT client_id
  const OPENAI_MOBILE_RT_CLIENT_ID = 'app_LlGpXReQgckcGGUo2JrYvtJK'


  const buildOpenAICodexImportCredentialExtras = (): Record<string, unknown> | null => {
    const credentials: Record<string, unknown> = {}
    if (!input.isOpenAIModelRestrictionDisabled.value) {
      applyAccountModelReferences(credentials, 'openai', input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
    }

    const compactModelMapping = input.buildOpenAICompactModelMapping()
    if (compactModelMapping) {
      credentials.compact_model_mapping = compactModelMapping
    }

    if (!input.applyTempUnschedConfig(credentials)) {
      return null
    }
    return credentials
  }


  const formatCodexImportMessages = (messages?: CodexSessionImportMessage[]) => {
    return (messages || [])
      .map((item) => {
        const name = item.name ? ` ${item.name}` : ''
        return `#${item.index}${name}: ${item.message}`
      })
      .join('\n')
  }


  const isAgentIdentityImportContent = (content: string) => {
    const isAgentIdentityValue = (value: unknown): boolean => {
      if (Array.isArray(value)) return value.length > 0 && value.every(isAgentIdentityValue)
      if (!value || typeof value !== 'object') return false
      const record = value as Record<string, unknown>
      const authMode = record.auth_mode ?? record.authMode
      const agentIdentity = record.agent_identity ?? record.agentIdentity
      return (typeof authMode === 'string' && authMode.toLowerCase() === 'agentidentity')
        || (!!agentIdentity && typeof agentIdentity === 'object')
    }

    try {
      return isAgentIdentityValue(JSON.parse(content))
    } catch {
      const lines = content.split('\n').map((line) => line.trim()).filter(Boolean)
      if (lines.length === 0) return false
      try {
        return lines.every((line) => isAgentIdentityValue(JSON.parse(line)))
      } catch {
        return false
      }
    }
  }


  const handleOpenAIImportCodexSession = async (content: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      const oauthClient = input.openaiOAuth
      const trimmed = content.trim()
      if (!trimmed) {
        oauthClient.error.value = input.t('admin.accounts.oauth.openai.codexSessionEmpty')
        return
      }
      if (input.oauthFlowRef.value?.inputMethod === 'agent_identity' && !isAgentIdentityImportContent(trimmed)) {
        oauthClient.error.value = input.t('admin.accounts.oauth.openai.agentIdentityInvalid')
        return
      }

      const credentialExtras = buildOpenAICodexImportCredentialExtras()
      if (credentialExtras === null) {
        return
      }

      oauthClient.loading.value = true
      oauthClient.error.value = ''

      try {
        const extra = buildOpenAICodexImportExtra()
        const result = await pending.wait(async () => accountsAPI.importCodexSession({
          content: trimmed,
          name: input.form.name,
          notes: input.form.notes || null,
          proxy_id: input.form.proxy_id,
          concurrency: input.form.concurrency,
          load_factor: input.form.load_factor ?? undefined,
          priority: input.form.priority,
          rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
          group_ids: input.form.group_ids,
          expires_at: input.form.expires_at,
          auto_pause_on_expired: input.autoPauseOnExpired.value,
          credential_extras: Object.keys(credentialExtras).length > 0 ? credentialExtras : undefined,
          extra: input.withAccountExtra(extra),
          update_existing: true
        }))

        const successCount = result.created + result.updated
        const params = {
          created: result.created,
          updated: result.updated,
          skipped: result.skipped,
          failed: result.failed
        }

        if (successCount > 0 && result.failed === 0) {
          input.appStore.showSuccess(input.t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
          input.emit('created')
          input.handleClose()
          return
        }

        const errorText = formatCodexImportMessages(result.errors)
        const warningText = formatCodexImportMessages(result.warnings)
        oauthClient.error.value = [errorText, warningText].filter(Boolean).join('\n')

        if (result.failed === 0) {
          input.appStore.showWarning(input.t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
          return
        }

        if (successCount > 0) {
          input.appStore.showWarning(input.t('admin.accounts.oauth.openai.codexSessionImportPartial', params))
          input.emit('created')
          return
        }

        input.appStore.showError(input.t('admin.accounts.oauth.openai.codexSessionImportFailed'))
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        oauthClient.error.value =
          errorDetails.response?.data?.detail ||
          errorDetails.response?.data?.message ||
          errorDetails.message ||
          input.t('admin.accounts.oauth.openai.codexSessionImportFailed')
        input.appStore.showError(oauthClient.error.value)
      } finally {
        if (pending.current()) {

          oauthClient.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleOpenAIImportCodexPAT = async (accessToken: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      const oauthClient = input.openaiOAuth
      const trimmed = accessToken.trim()
      if (!trimmed) {
        oauthClient.error.value = input.t('admin.accounts.oauth.openai.codexPatEmpty')
        return
      }

      const credentialExtras = buildOpenAICodexImportCredentialExtras()
      if (credentialExtras === null) {
        return
      }

      oauthClient.loading.value = true
      oauthClient.error.value = ''

      try {
        const extra = buildOpenAICodexImportExtra()
        await pending.wait(async () => accountsAPI.createOpenAICodexPAT({
          access_token: trimmed,
          name: input.form.name,
          notes: input.form.notes || null,
          proxy_id: input.form.proxy_id,
          concurrency: input.form.concurrency,
          load_factor: input.form.load_factor ?? undefined,
          priority: input.form.priority,
          rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
          group_ids: input.form.group_ids,
          expires_at: input.form.expires_at,
          auto_pause_on_expired: input.autoPauseOnExpired.value,
          credential_extras: Object.keys(credentialExtras).length > 0 ? credentialExtras : undefined,
          extra: input.withAccountExtra(extra)
        }))

        input.appStore.showSuccess(input.t('admin.accounts.accountCreated'))
        input.emit('created')
        input.handleClose()
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        oauthClient.error.value =
          errorDetails.response?.data?.detail ||
          errorDetails.response?.data?.message ||
          errorDetails.message ||
          input.t('admin.accounts.oauth.openai.codexPatImportFailed')
        input.appStore.showError(oauthClient.error.value)
      } finally {
        if (pending.current()) {

          oauthClient.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // OpenAI RT 批量验证和创建（共享逻辑）
  const handleOpenAIBatchRT = async (refreshTokenInput: string, clientId?: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      const oauthClient = input.openaiOAuth
      if (!refreshTokenInput.trim()) return

      const refreshTokens = refreshTokenInput
        .split('\n')
        .map((rt) => rt.trim())
        .filter((rt) => rt)

      if (refreshTokens.length === 0) {
        oauthClient.error.value = input.t('admin.accounts.oauth.openai.pleaseEnterRefreshToken')
        return
      }

      oauthClient.loading.value = true
      oauthClient.error.value = ''

      let successCount = 0
      let failedCount = 0
      const errors: string[] = []
      const shouldCreateOpenAI = input.form.platform === 'openai'

      try {
        for (let i = 0;i < refreshTokens.length;i++) {
          try {
            const tokenInfo = await pending.wait(async () => oauthClient.validateRefreshToken(
              refreshTokens[i],
              input.form.proxy_id,
              clientId
            ))
            if (!tokenInfo) {
              failedCount++
              errors.push(`#${i + 1}: ${oauthClient.error.value || 'Validation failed'}`)
              oauthClient.error.value = ''
              continue
            }

            const credentials = oauthClient.buildCredentials(tokenInfo)
            if (clientId) {
              credentials.client_id = clientId
            }
            const oauthExtra = oauthClient.buildExtraInfo(tokenInfo) as Record<string, unknown> | undefined
            const extra = input.buildOpenAIExtra(oauthExtra)

            // Add model mapping for OpenAI OAuth accounts（透传模式下不应用）
            if (shouldCreateOpenAI && !input.isOpenAIModelRestrictionDisabled.value) {
              applyAccountModelReferences(credentials, 'openai', input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
            }
            if (shouldCreateOpenAI) {
              const compactModelMapping = input.buildOpenAICompactModelMapping()
              if (compactModelMapping) {
                credentials.compact_model_mapping = compactModelMapping
              }
            }

            // Generate account name; fallback to email if name is empty (ent schema requires NotEmpty)
            const baseName = input.form.name || tokenInfo.email || 'OpenAI OAuth Account'
            const accountName = refreshTokens.length > 1 ? `${baseName} #${i + 1}` : baseName

            if (shouldCreateOpenAI) {
              await pending.wait(async () => accountsAPI.create({
                name: accountName,
                notes: input.form.notes,
                platform: 'openai',
                type: 'oauth',
                credentials,
                extra: input.withAccountExtra(extra),
                proxy_id: input.form.proxy_id,
                concurrency: input.form.concurrency,
                load_factor: input.form.load_factor ?? undefined,
                priority: input.form.priority,
                rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
                group_ids: input.form.group_ids,
                expires_at: input.form.expires_at,
                auto_pause_on_expired: input.autoPauseOnExpired.value
              }))
            }

            successCount++
          } catch (error: unknown) {
            const errorDetails = readApiError(error)
            if (!pending.current()) throw new StaleResourceTask()

            failedCount++
            const errMsg = errorDetails.response?.data?.detail || errorDetails.message || 'Unknown error'
            errors.push(`#${i + 1}: ${errMsg}`)
          }
        }

        // Show results
        if (successCount > 0 && failedCount === 0) {
          input.appStore.showSuccess(
            refreshTokens.length > 1
              ? input.t('admin.accounts.oauth.batchSuccess', { count: successCount })
              : input.t('admin.accounts.accountCreated')
          )
          input.emit('created')
          input.handleClose()
        } else if (successCount > 0 && failedCount > 0) {
          input.appStore.showWarning(
            input.t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount })
          )
          oauthClient.error.value = errors.join('\n')
          input.emit('created')
        } else {
          oauthClient.error.value = errors.join('\n')
          input.appStore.showError(input.t('admin.accounts.oauth.batchFailed'))
        }
      } finally {
        if (pending.current()) {

          oauthClient.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // 手动输入 RT（Codex CLI client_id，默认）
  const handleOpenAIValidateRT = (rt: string) => handleOpenAIBatchRT(rt)


  // 手动输入 Mobile RT
  const handleOpenAIValidateMobileRT = (rt: string) => handleOpenAIBatchRT(rt, OPENAI_MOBILE_RT_CLIENT_ID)
  return { buildOpenAICodexImportExtra, handleOpenAIExchange, OPENAI_MOBILE_RT_CLIENT_ID, buildOpenAICodexImportCredentialExtras, formatCodexImportMessages, isAgentIdentityImportContent, handleOpenAIImportCodexSession, handleOpenAIImportCodexPAT, handleOpenAIBatchRT, handleOpenAIValidateRT, handleOpenAIValidateMobileRT }
}
