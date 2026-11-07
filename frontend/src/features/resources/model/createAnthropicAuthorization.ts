import { readApiError } from '@/shared/lib/apiError'
import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import { buildClaudeSetupTokenCredentials, useAccountOAuth, type AddMethod } from '@/features/resources/queries/useAccountOAuth'
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';
import { applyInterceptWarmup } from "@/features/resources/model/credentialsBuilder"
import type { Ref } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, TempUnschedRuleForm } from './createEditorTypes'




interface CreateAnthropicAuthorizationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
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
  oauth: ReturnType<typeof useAccountOAuth>
  t: ReturnType<typeof useI18n>["t"]
  createAccountAndFinish: (platform: AccountPlatform, type: AccountType, credentials: Record<string, unknown>, extra?: Record<string, unknown>) => Promise<void>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError" | "showSuccess">>
  form: Pick<CreateResourceForm, "proxy_id" | "platform" | "name" | "notes" | "concurrency" | "load_factor" | "priority" | "rate_multiplier" | "group_ids" | "expires_at">
  addMethod: Ref<AddMethod, AddMethod>
  interceptWarmupRequests: Ref<boolean, boolean>
  tempUnschedEnabled: Ref<boolean, boolean>
  buildTempUnschedRules: (rules: TempUnschedRuleForm[]) => { error_code: number; keywords: string[]; duration_minutes: number; description: string; scope: TempUnschedulableScope }[]
  tempUnschedRules: Ref<{ error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[], TempUnschedRuleForm[] | { error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[]>
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  props: Readonly<Pick<Props, "allowRateEdit">>
  autoPauseOnExpired: Ref<boolean, boolean>
  emit: ((event: "close") => void) & ((event: "created") => void)
  handleClose: () => void
}




export function useCreateAnthropicAuthorizationDraft(input: CreateAnthropicAuthorizationInput): {
  buildAnthropicOAuthExtra: (base?: Record<string, unknown>) => Record<string, unknown> | undefined
  handleAnthropicSetupToken: (rawToken: string) => Promise<void>
  handleAnthropicExchange: (authCode: string) => Promise<void>
  handleCookieAuth: (sessionKey: string) => Promise<void>
} {


  const buildAnthropicOAuthExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
    const extra: Record<string, unknown> = { ...(base || {}) }

    if (input.windowCostEnabled.value && input.windowCostLimit.value != null && input.windowCostLimit.value > 0) {
      extra.window_cost_limit = input.windowCostLimit.value
      extra.window_cost_sticky_reserve = input.windowCostStickyReserve.value ?? 10
    }
    if (input.sessionLimitEnabled.value && input.maxSessions.value != null && input.maxSessions.value > 0) {
      extra.max_sessions = input.maxSessions.value
      extra.session_idle_timeout_minutes = input.sessionIdleTimeout.value ?? 5
    }
    if (input.rpmLimitEnabled.value) {
      extra.base_rpm = input.baseRpm.value != null && input.baseRpm.value > 0 ? input.baseRpm.value : 15
      extra.rpm_strategy = input.rpmStrategy.value
      if (input.rpmStickyBuffer.value != null && input.rpmStickyBuffer.value > 0) {
        extra.rpm_sticky_buffer = input.rpmStickyBuffer.value
      }
    }
    if (input.userMsgQueueMode.value) extra.user_msg_queue_mode = input.userMsgQueueMode.value
    if (input.tlsFingerprintEnabled.value) {
      extra.enable_tls_fingerprint = true
      if (input.tlsFingerprintProfileId.value) extra.tls_fingerprint_profile_id = input.tlsFingerprintProfileId.value
    }
    if (input.sessionIdMaskingEnabled.value) extra.session_id_masking_enabled = true
    if (input.cacheTTLOverrideEnabled.value) {
      extra.cache_ttl_override_enabled = true
      extra.cache_ttl_override_target = input.cacheTTLOverrideTarget.value
    }
    if (input.customBaseUrlEnabled.value && input.customBaseUrl.value.trim()) {
      extra.custom_base_url_enabled = true
      extra.custom_base_url = input.customBaseUrl.value.trim()
    }

    return Object.keys(extra).length > 0 ? extra : undefined
  }


  const handleAnthropicSetupToken = async (rawToken: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id, input.addMethod.value])
    try {

      const credentials = buildClaudeSetupTokenCredentials(rawToken)
      if (!credentials) {
        input.oauth.error.value = input.t('admin.accounts.oauth.setupTokenInvalid')
        return
      }

      input.oauth.loading.value = true
      input.oauth.error.value = ''
      try {
        await pending.wait(async () => input.createAccountAndFinish('anthropic', 'setup-token', credentials, buildAnthropicOAuthExtra()))
      } catch (error: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        input.oauth.error.value = extractApiErrorMessage(error, input.t('admin.accounts.oauth.authFailed'))
        input.appStore.showError(input.oauth.error.value)
      } finally {
        if (pending.current()) {

          input.oauth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  // Anthropic OAuth 授权码兑换
  const handleAnthropicExchange = async (authCode: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id, input.addMethod.value])
    try {

      if (!authCode.trim() || !input.oauth.sessionId.value) return

      input.oauth.loading.value = true
      input.oauth.error.value = ''

      try {
        const proxyConfig = input.form.proxy_id ? { proxy_id: input.form.proxy_id } : {}
        const endpoint =
          input.addMethod.value === 'oauth'
            ? '/admin/accounts/exchange-code'
            : '/admin/accounts/exchange-setup-token-code'

        const tokenInfo = await pending.wait(async () => accountsAPI.exchangeCode(endpoint, {
          session_id: input.oauth.sessionId.value,
          code: authCode.trim(),
          ...proxyConfig
        }))

        const extra = buildAnthropicOAuthExtra(input.oauth.buildExtraInfo(tokenInfo))

        const credentials: Record<string, unknown> = { ...tokenInfo }
        applyInterceptWarmup(credentials, input.interceptWarmupRequests.value, 'create')
        await pending.wait(async () => input.createAccountAndFinish(
          input.form.platform,
          input.addMethod.value === 'oauth' ? 'oauth' : 'setup-token',
          credentials,
          extra
        ))
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.oauth.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
        input.appStore.showError(input.oauth.error.value)
      } finally {
        if (pending.current()) {

          input.oauth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleCookieAuth = async (sessionKey: string) => {
    const pending = input.resourceTasks.capture(() => [input.form.platform, input.form.proxy_id, input.addMethod.value])
    try {

      input.oauth.loading.value = true
      input.oauth.error.value = ''

      try {
        const proxyConfig = input.form.proxy_id ? { proxy_id: input.form.proxy_id } : {}
        const keys = input.oauth.parseSessionKeys(sessionKey)

        if (keys.length === 0) {
          input.oauth.error.value = input.t('admin.accounts.oauth.pleaseEnterSessionKey')
          return
        }

        const tempUnschedPayload = input.tempUnschedEnabled.value
          ? input.buildTempUnschedRules(input.tempUnschedRules.value)
          : []
        if (input.tempUnschedEnabled.value && tempUnschedPayload.length === 0) {
          input.appStore.showError(input.t('admin.accounts.tempUnschedulable.rulesInvalid'))
          return
        }

        const endpoint =
          input.addMethod.value === 'oauth'
            ? '/admin/accounts/cookie-auth'
            : '/admin/accounts/setup-token-cookie-auth'

        let successCount = 0
        let failedCount = 0
        const errors: string[] = []

        for (let i = 0;i < keys.length;i++) {
          try {
            const tokenInfo = await pending.wait(async () => accountsAPI.exchangeCode(endpoint, {
              session_id: '',
              code: keys[i],
              ...proxyConfig
            }))

            const extra = buildAnthropicOAuthExtra(input.oauth.buildExtraInfo(tokenInfo))

            const accountName = keys.length > 1 ? `${input.form.name} #${i + 1}` : input.form.name

            const credentials: Record<string, unknown> = { ...tokenInfo }
            applyInterceptWarmup(credentials, input.interceptWarmupRequests.value, 'create')
            if (input.tempUnschedEnabled.value) {
              credentials.temp_unschedulable_enabled = true
              credentials.temp_unschedulable_rules = tempUnschedPayload
            }

            await pending.wait(async () => accountsAPI.create({
              name: accountName,
              notes: input.form.notes,
              platform: input.form.platform,
              type: input.addMethod.value === 'oauth' ? 'oauth' : 'setup-token',
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
            errors.push(
              input.t('admin.accounts.oauth.keyAuthFailed', {
                index: i + 1,
                error: errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.authFailed')
              })
            )
          }
        }

        if (successCount > 0) {
          input.appStore.showSuccess(input.t('admin.accounts.oauth.successCreated', { count: successCount }))
          if (failedCount === 0) {
            input.emit('created')
            input.handleClose()
          } else {
            input.emit('created')
          }
        }

        if (failedCount > 0) {
          input.oauth.error.value = errors.join('\n')
        }
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.oauth.error.value = errorDetails.response?.data?.detail || input.t('admin.accounts.oauth.cookieAuthFailed')
      } finally {
        if (pending.current()) {

          input.oauth.loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { buildAnthropicOAuthExtra, handleAnthropicSetupToken, handleAnthropicExchange, handleCookieAuth }
}
