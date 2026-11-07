import { readApiError } from '@/shared/lib/apiError'
import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { useI18n } from 'vue-i18n'
import { buildModelMappingObject } from '../model/modelRestrictions'
import accountsAPI from '@/features/resources/accounts'
import { type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import { useGrokOAuth } from '@/features/resources/queries/useGrokOAuth'
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { Ref } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { OAuthFlowExposed, Props, ModelMapping } from './createEditorTypes'




interface CreateGrokAuthorizationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  grokOAuth: ReturnType<typeof useGrokOAuth>
  t: ReturnType<typeof useI18n>["t"]
  validateGrokOAuthUpstreamConfig: () => boolean
  form: Pick<CreateResourceForm, "platform" | "proxy_id" | "name" | "notes" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  applyGrokOAuthUpstreamConfig: (credentials: Record<string, unknown>) => void
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  allowedModels: Ref<string[], string[]>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  props: Readonly<Pick<Props, "allowRateEdit">>
  autoPauseOnExpired: Ref<boolean, boolean>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showSuccess" | "showWarning" | "showError">>
  emit: ((event: "close") => void) & ((event: "created") => void)
  handleClose: () => void
  resourceServices: Readonly<Pick<ResourceEditorServices, "credentialAuthorization">>
  oauthFlowRef: Ref<{ authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null, OAuthFlowExposed | { authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null>
  createAccountAndFinish: (platform: AccountPlatform, type: AccountType, credentials: Record<string, unknown>, extra?: Record<string, unknown>) => Promise<void>
}




export function useCreateGrokAuthorizationDraft(input: CreateGrokAuthorizationInput): {
  handleGrokValidateRT: (refreshTokenInput: string) => Promise<void>
  handleGrokImportSSO: (ssoInput: string) => Promise<void>
  handleGrokAuthorizePassword: (emailPasswordInput: string) => Promise<void>
  handleGrokExchange: (authCode: string) => Promise<void>
} {


  // Grok 手动 RT 批量验证和创建
  const handleGrokValidateRT = async (refreshTokenInput: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!refreshTokenInput.trim()) return

      const refreshTokens = refreshTokenInput
        .split('\n')
        .map((rt) => rt.trim())
        .filter((rt) => rt)

      if (refreshTokens.length === 0) {
        input.grokOAuth.error.value = input.t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
        return
      }
      if (!input.validateGrokOAuthUpstreamConfig()) return

      input.grokOAuth.loading.value = true
      input.grokOAuth.error.value = ''

      let successCount = 0
      let failedCount = 0
      const errors: string[] = []

      try {
        for (let i = 0;i < refreshTokens.length;i++) {
          try {
            const tokenInfo = await pending.wait(async () => input.grokOAuth.validateRefreshToken(refreshTokens[i], input.form.proxy_id))
            if (!tokenInfo) {
              failedCount++
              errors.push(`#${i + 1}: ${input.grokOAuth.error.value || 'Validation failed'}`)
              input.grokOAuth.error.value = ''
              continue
            }

            const credentials = input.grokOAuth.buildCredentials(tokenInfo)
            input.applyGrokOAuthUpstreamConfig(credentials)
            const extra = input.grokOAuth.buildExtraInfo(tokenInfo)
            const accountName = refreshTokens.length > 1 ? `${input.form.name || tokenInfo.email || 'Grok OAuth Account'} #${i + 1}` : (input.form.name || tokenInfo.email || 'Grok OAuth Account')

            const modelMapping = buildModelMappingObject(input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
            if (modelMapping) {
              credentials.model_mapping = modelMapping
            }
            if (!input.applyTempUnschedConfig(credentials)) {
              return
            }

            await pending.wait(async () => accountsAPI.create({
              name: accountName,
              notes: input.form.notes,
              platform: 'grok',
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
            successCount++
          } catch (error: unknown) {
            const errorDetails = readApiError(error)
            if (!pending.current()) throw new StaleResourceTask()

            failedCount++
            const errMsg = errorDetails.response?.data?.detail || errorDetails.message || 'Unknown error'
            errors.push(`#${i + 1}: ${errMsg}`)
          }
        }

        if (successCount > 0 && failedCount === 0) {
          input.appStore.showSuccess(
            refreshTokens.length > 1
              ? input.t('admin.accounts.oauth.batchSuccess', { count: successCount })
              : input.t('admin.accounts.accountCreated')
          )
          input.emit('created')
          input.handleClose()
        } else if (successCount > 0) {
          input.appStore.showWarning(input.t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount }))
          input.grokOAuth.error.value = errors.join('\n')
          input.emit('created')
        } else {
          input.grokOAuth.error.value = errors.join('\n')
          input.appStore.showError(input.t('admin.accounts.oauth.batchFailed'))
        }
      } finally {
        if (pending.current()) {

          input.grokOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleGrokImportSSO = async (ssoInput: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      // Align with OpenAI/Grok RT batch import: one token per line, no client-side dedupe.
      const ssoTokens = ssoInput
        .split('\n')
        .map((token) => token.trim())
        .filter((token) => token)
      if (ssoTokens.length === 0) return
      if (!input.validateGrokOAuthUpstreamConfig()) return

      input.grokOAuth.loading.value = true
      input.grokOAuth.error.value = ''

      const credentials: Record<string, unknown> = {}
      input.applyGrokOAuthUpstreamConfig(credentials)
      const modelMapping = buildModelMappingObject(input.modelRestrictionMode.value, input.allowedModels.value, input.modelMappings.value)
      if (modelMapping) {
        credentials.model_mapping = modelMapping
      }
      if (!input.applyTempUnschedConfig(credentials)) {
        input.grokOAuth.loading.value = false
        return
      }

      try {
        const result = await pending.wait(async () => input.resourceServices.credentialAuthorization.grok.createFromSSO({
          sso_tokens: ssoTokens,
          name: input.form.name || undefined,
          notes: input.form.notes || undefined,
          proxy_id: input.form.proxy_id,
          group_ids: input.form.group_ids,
          credentials,
          concurrency: input.form.concurrency,
          load_factor: input.form.load_factor ?? undefined,
          priority: input.form.priority,
          rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
          expires_at: input.form.expires_at,
          auto_pause_on_expired: input.autoPauseOnExpired.value
        }))

        const successCount = result.created?.length || 0
        const failedCount = result.failed?.length || 0
        if (successCount > 0 && failedCount === 0) {
          input.appStore.showSuccess(
            ssoTokens.length > 1
              ? input.t('admin.accounts.oauth.batchSuccess', { count: successCount })
              : input.t('admin.accounts.accountCreated')
          )
          input.emit('created')
          input.handleClose()
        } else if (successCount > 0 && failedCount > 0) {
          // Same as OpenAI/Grok RT: keep input, show failures, refresh list.
          input.appStore.showWarning(
            input.t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount })
          )
          input.grokOAuth.error.value = (result.failed || [])
            .map((item) => `#${item.index}: ${item.error || 'Unknown error'}`)
            .join('\n')
          input.emit('created')
        } else {
          input.grokOAuth.error.value = (result.failed || [])
            .map((item) => `#${item.index}: ${item.error || 'Unknown error'}`)
            .join('\n') || input.t('admin.accounts.oauth.grok.failedToConvertSSO')
          input.appStore.showError(input.t('admin.accounts.oauth.batchFailed'))
        }
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.grokOAuth.error.value = errorDetails.response?.data?.detail || errorDetails.message || input.t('admin.accounts.oauth.grok.failedToConvertSSO')
        input.appStore.showError(input.grokOAuth.error.value)
      } finally {
        if (pending.current()) {

          input.grokOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  /**
   * Grok password login: each line is email----password.
   * Password is only used for the authorize API call; buildCredentials never stores it.
   */
  const handleGrokAuthorizePassword = async (emailPasswordInput: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!emailPasswordInput.trim()) return
      if (!input.validateGrokOAuthUpstreamConfig()) return

      const lines = emailPasswordInput
        .split('\n')
        // Keep the password portion byte-for-byte; trim is only for determining
        // whether this textarea line is blank.
        .filter((line) => line.trim() && line.includes('----'))

      if (lines.length === 0) {
        input.grokOAuth.error.value = input.t(
          'admin.accounts.oauth.grok.pleaseEnterPassword',
          'Please enter email----password (one per line)'
        )
        return
      }

      input.grokOAuth.loading.value = true
      input.grokOAuth.error.value = ''

      let successCount = 0
      let failedCount = 0
      const errors: string[] = []

      try {
        for (let i = 0;i < lines.length;i++) {
          try {
            const tokenInfo = await pending.wait(async () => input.grokOAuth.authorizePassword(lines[i], input.form.proxy_id))
            if (!tokenInfo) {
              failedCount++
              errors.push(`#${i + 1}: ${input.grokOAuth.error.value || 'Authorization failed'}`)
              input.grokOAuth.error.value = ''
              continue
            }

            const credentials = input.grokOAuth.buildCredentials(tokenInfo)
            input.applyGrokOAuthUpstreamConfig(credentials)
            const extra = input.grokOAuth.buildExtraInfo(tokenInfo)
            const accountName =
              lines.length > 1
                ? `${input.form.name || tokenInfo.email || 'Grok OAuth Account'} #${i + 1}`
                : input.form.name || tokenInfo.email || 'Grok OAuth Account'

            const modelMapping = buildModelMappingObject(
              input.modelRestrictionMode.value,
              input.allowedModels.value,
              input.modelMappings.value
            )
            if (modelMapping) {
              credentials.model_mapping = modelMapping
            }
            if (!input.applyTempUnschedConfig(credentials)) {
              return
            }

            await pending.wait(async () => accountsAPI.create({
              name: accountName,
              notes: input.form.notes,
              platform: 'grok',
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
            successCount++
          } catch (error: unknown) {
            const errorDetails = readApiError(error)
            if (!pending.current()) throw new StaleResourceTask()

            failedCount++
            const errMsg = errorDetails.response?.data?.detail || errorDetails.message || 'Unknown error'
            errors.push(`#${i + 1}: ${errMsg}`)
          }
        }

        if (successCount > 0 && failedCount === 0) {
          input.appStore.showSuccess(
            lines.length > 1
              ? input.t('admin.accounts.oauth.batchSuccess', { count: successCount })
              : input.t('admin.accounts.accountCreated')
          )
          input.emit('created')
          input.handleClose()
        } else if (successCount > 0) {
          input.appStore.showWarning(
            input.t('admin.accounts.oauth.batchPartialSuccess', {
              success: successCount,
              failed: failedCount
            })
          )
          input.grokOAuth.error.value = errors.join('\n')
          input.emit('created')
        } else {
          input.grokOAuth.error.value = errors.join('\n')
          input.appStore.showError(input.t('admin.accounts.oauth.batchFailed'))
        }
      } finally {
        if (pending.current()) {

          input.grokOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // Grok OAuth 授权码兑换
  const handleGrokExchange = async (authCode: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!authCode.trim() || !input.grokOAuth.sessionId.value) return
      if (!input.validateGrokOAuthUpstreamConfig()) return

      input.grokOAuth.loading.value = true
      input.grokOAuth.error.value = ''

      try {
        const stateFromInput = input.oauthFlowRef.value?.oauthState || ''
        const stateToUse = stateFromInput || input.grokOAuth.state.value
        if (!stateToUse) {
          input.grokOAuth.error.value = input.t('admin.accounts.oauth.authFailed')
          input.appStore.showError(input.grokOAuth.error.value)
          return
        }

        const tokenInfo = await pending.wait(async () => input.grokOAuth.exchangeAuthCode({
          code: authCode.trim(),
          sessionId: input.grokOAuth.sessionId.value,
          state: stateToUse,
          proxyId: input.form.proxy_id
        }))
        if (!tokenInfo) return

        const credentials = input.grokOAuth.buildCredentials(tokenInfo)
        input.applyGrokOAuthUpstreamConfig(credentials)
        const extra = input.grokOAuth.buildExtraInfo(tokenInfo)
        await pending.wait(async () => input.createAccountAndFinish('grok', 'oauth', credentials, extra))
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.grokOAuth.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
        input.appStore.showError(input.grokOAuth.error.value)
      } finally {
        if (pending.current()) {

          input.grokOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { handleGrokValidateRT, handleGrokImportSSO, handleGrokAuthorizePassword, handleGrokExchange }
}
