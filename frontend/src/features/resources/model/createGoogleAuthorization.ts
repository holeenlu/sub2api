import { readApiError } from '@/shared/lib/apiError'
import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { useI18n } from 'vue-i18n'
import { buildModelMappingObject } from '../model/modelRestrictions'
import accountsAPI from '@/features/resources/accounts'
import { type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import { useGeminiOAuth } from '@/features/resources/queries/useGeminiOAuth'
import { useAntigravityOAuth } from '@/features/resources/queries/useAntigravityOAuth'
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { CreateAccountRequest } from '@/features/resources/api/accountTypes';
import { applyAntigravityProjectID, applyInterceptWarmup } from "@/features/resources/model/credentialsBuilder"
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { OAuthFlowExposed, Props, ModelMapping } from './createEditorTypes'




interface CreateGoogleAuthorizationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  antigravityOAuth: ReturnType<typeof useAntigravityOAuth>
  t: ReturnType<typeof useI18n>["t"]
  form: Pick<CreateResourceForm, "platform" | "proxy_id" | "name" | "notes" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  antigravityProjectId: Ref<string, string>
  withAntigravityConfirmFlag: (payload: CreateAccountRequest) => CreateAccountRequest
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  props: Readonly<Pick<Props, "allowRateEdit">>
  autoPauseOnExpired: Ref<boolean, boolean>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showSuccess" | "showWarning" | "showError">>
  emit: ((event: "close") => void) & ((event: "created") => void)
  handleClose: () => void
  geminiOAuth: ReturnType<typeof useGeminiOAuth>
  oauthFlowRef: Ref<{ authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null, OAuthFlowExposed | { authCode: string; oauthState: string; projectId: string; sessionKey: string; setupToken: string; refreshToken: string; sessionToken: string; codexSession: string; codexPAT: string; ssoCookie: string; inputMethod: AuthInputMethod; reset: () => void } | null>
  geminiOAuthType: Ref<"code_assist" | "google_one" | "ai_studio", "code_assist" | "google_one" | "ai_studio">
  geminiSelectedTier: ComputedRef<"" | "aistudio_free" | "aistudio_paid" | "google_one_free" | "google_ai_pro" | "google_ai_ultra" | "gcp_standard" | "gcp_enterprise">
  createAccountAndFinish: (platform: AccountPlatform, type: AccountType, credentials: Record<string, unknown>, extra?: Record<string, unknown>) => Promise<void>
  interceptWarmupRequests: Ref<boolean, boolean>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  buildAntigravityExtra: () => Record<string, unknown> | undefined
}




export function useCreateGoogleAuthorizationDraft(input: CreateGoogleAuthorizationInput): {
  handleAntigravityValidateRT: (refreshTokenInput: string) => Promise<void>
  handleGeminiExchange: (authCode: string) => Promise<void>
  handleAntigravityExchange: (authCode: string) => Promise<void>
} {


  // Antigravity 手动 RT 批量验证和创建
  const handleAntigravityValidateRT = async (refreshTokenInput: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!refreshTokenInput.trim()) return

      // Parse multiple refresh tokens (one per line)
      const refreshTokens = refreshTokenInput
        .split('\n')
        .map((rt) => rt.trim())
        .filter((rt) => rt)

      if (refreshTokens.length === 0) {
        input.antigravityOAuth.error.value = input.t('admin.accounts.oauth.antigravity.pleaseEnterRefreshToken')
        return
      }

      input.antigravityOAuth.loading.value = true
      input.antigravityOAuth.error.value = ''

      let successCount = 0
      let failedCount = 0
      const errors: string[] = []

      try {
        for (let i = 0;i < refreshTokens.length;i++) {
          try {
            const tokenInfo = await pending.wait(async () => input.antigravityOAuth.validateRefreshToken(
              refreshTokens[i],
              input.form.proxy_id
            ))
            if (!tokenInfo) {
              failedCount++
              errors.push(`#${i + 1}: ${input.antigravityOAuth.error.value || 'Validation failed'}`)
              input.antigravityOAuth.error.value = ''
              continue
            }

            const credentials = input.antigravityOAuth.buildCredentials(tokenInfo, refreshTokens[i])
            applyAntigravityProjectID(credentials, input.antigravityProjectId.value, 'create')

            // Generate account name with index for batch
            const accountName = refreshTokens.length > 1 ? `${input.form.name} #${i + 1}` : input.form.name

            // Note: Antigravity doesn't have buildExtraInfo, so we pass empty extra or rely on credentials
            const createPayload = input.withAntigravityConfirmFlag({
              name: accountName,
              notes: input.form.notes,
              platform: 'antigravity',
              type: 'oauth',
              credentials,
              extra: input.withAccountExtra({}),
              proxy_id: input.form.proxy_id,
              concurrency: input.form.concurrency,
              load_factor: input.form.load_factor ?? undefined,
              priority: input.form.priority,
              rate_multiplier: input.props.allowRateEdit ? input.form.rate_multiplier : undefined,
              group_ids: input.form.group_ids,
              expires_at: input.form.expires_at,
              auto_pause_on_expired: input.autoPauseOnExpired.value
            })
            await pending.wait(async () => accountsAPI.create(createPayload))
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
          input.antigravityOAuth.error.value = errors.join('\n')
          input.emit('created')
        } else {
          input.antigravityOAuth.error.value = errors.join('\n')
          input.appStore.showError(input.t('admin.accounts.oauth.batchFailed'))
        }
      } finally {
        if (pending.current()) {

          input.antigravityOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // Gemini OAuth 授权码兑换
  const handleGeminiExchange = async (authCode: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!authCode.trim() || !input.geminiOAuth.sessionId.value) return

      input.geminiOAuth.loading.value = true
      input.geminiOAuth.error.value = ''

      try {
        const stateFromInput = input.oauthFlowRef.value?.oauthState || ''
        const stateToUse = stateFromInput || input.geminiOAuth.state.value
        if (!stateToUse) {
          input.geminiOAuth.error.value = input.t('admin.accounts.oauth.authFailed')
          input.appStore.showError(input.geminiOAuth.error.value)
          return
        }

        const tokenInfo = await pending.wait(async () => input.geminiOAuth.exchangeAuthCode({
          code: authCode.trim(),
          sessionId: input.geminiOAuth.sessionId.value,
          state: stateToUse,
          proxyId: input.form.proxy_id,
          oauthType: input.geminiOAuthType.value,
          tierId: input.geminiSelectedTier.value
        }))
        if (!tokenInfo) return

        const credentials = input.geminiOAuth.buildCredentials(tokenInfo)
        const extra = input.geminiOAuth.buildExtraInfo(tokenInfo)
        await pending.wait(async () => input.createAccountAndFinish('gemini', 'oauth', credentials, extra))
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.geminiOAuth.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
        input.appStore.showError(input.geminiOAuth.error.value)
      } finally {
        if (pending.current()) {

          input.geminiOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // Antigravity OAuth 授权码兑换
  const handleAntigravityExchange = async (authCode: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id])
    try {

      if (!authCode.trim() || !input.antigravityOAuth.sessionId.value) return

      input.antigravityOAuth.loading.value = true
      input.antigravityOAuth.error.value = ''

      try {
        const stateFromInput = input.oauthFlowRef.value?.oauthState || ''
        const stateToUse = stateFromInput || input.antigravityOAuth.state.value
        if (!stateToUse) {
          input.antigravityOAuth.error.value = input.t('admin.accounts.oauth.authFailed')
          input.appStore.showError(input.antigravityOAuth.error.value)
          return
        }

        const tokenInfo = await pending.wait(async () => input.antigravityOAuth.exchangeAuthCode({
          code: authCode.trim(),
          sessionId: input.antigravityOAuth.sessionId.value,
          state: stateToUse,
          proxyId: input.form.proxy_id
        }))
        if (!tokenInfo) return

        const credentials = input.antigravityOAuth.buildCredentials(tokenInfo)
        applyAntigravityProjectID(credentials, input.antigravityProjectId.value, 'create')
        applyInterceptWarmup(credentials, input.interceptWarmupRequests.value, 'create')
        // Antigravity 只使用映射模式
        const antigravityModelMapping = buildModelMappingObject(
          'mapping',
          [],
          input.antigravityModelMappings.value
        )
        if (antigravityModelMapping) {
          credentials.model_mapping = antigravityModelMapping
        }
        const extra = input.buildAntigravityExtra()
        await pending.wait(async () => input.createAccountAndFinish('antigravity', 'oauth', credentials, extra))
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.antigravityOAuth.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
        input.appStore.showError(input.antigravityOAuth.error.value)
      } finally {
        if (pending.current()) {

          input.antigravityOAuth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { handleAntigravityValidateRT, handleGeminiExchange, handleAntigravityExchange }
}
