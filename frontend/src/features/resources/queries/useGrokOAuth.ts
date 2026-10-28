import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceTaskScope, StaleResourceTask } from './resourceTaskScope'
import { useResourceEditorServices } from '@/features/resources/services'
import { ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'


import type { GrokAuthUrlRequest, GrokExchangeCodeRequest, GrokTokenInfo } from '@/features/resources/grok'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/shared/lib/apiError'

export interface GrokOAuth {
  authUrl: Ref<string>
  sessionId: Ref<string>
  state: Ref<string>
  loading: Ref<boolean>
  error: Ref<string>
  resetState(): void
  generateAuthUrl(proxyId: number | null | undefined): Promise<boolean>
  exchangeAuthCode(params: { code: string; sessionId: string; state: string; proxyId?: number | null }): Promise<GrokTokenInfo | null>
  validateRefreshToken(refreshToken: string, proxyId?: number | null): Promise<GrokTokenInfo | null>
  validateSSOToken(ssoToken: string, proxyId?: number | null): Promise<GrokTokenInfo | null>
  authorizePassword(emailAndPassword: string, proxyId?: number | null): Promise<GrokTokenInfo | null>
  buildCredentials(tokenInfo: GrokTokenInfo): Record<string, unknown>
  buildExtraInfo(tokenInfo: GrokTokenInfo): Record<string, unknown>
}

export function useGrokOAuth(): GrokOAuth {
  const resourceTasks = useResourceTaskScope()
  const editorScope = useResourceEditorServices().scopeKey
  const resourceServices = useResourceEditorServices()
  const appStore = useResourceEditorServices().notices
  const { t } = useI18n()

  const authUrl = ref('')
  const sessionId = ref('')
  const state = ref('')
  const loading = ref(false)
  const error = ref('')

  const resetState = () => {
    resourceTasks.invalidate()
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    loading.value = false
    error.value = ''
  }

  let authorizationGeneration = 0

  const generateAuthUrl = async (proxyId: number | null | undefined): Promise<boolean> => {
    // Supersede authorization work, without cancelling independent capability queries.
    authorizationGeneration++
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      loading.value = true
      authUrl.value = ''
      sessionId.value = ''
      state.value = ''
      error.value = ''

      try {
        const payload: GrokAuthUrlRequest = {}
        if (proxyId) payload.proxy_id = proxyId

        const response = await pending.wait(async () => resourceServices.credentialAuthorization.grok.generateAuthUrl(payload))
        authUrl.value = response.auth_url
        sessionId.value = response.session_id
        state.value = response.state
        return true
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.grok.failedToGenerateUrl'))
        appStore.showError(error.value)
        return false
      } finally {
        if (pending.current()) {

          loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return false }
      throw error
    }
  }

  const exchangeAuthCode = async (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
  }): Promise<GrokTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      const code = params.code?.trim()
      if (!code || !params.sessionId || !params.state) {
        error.value = t('admin.accounts.oauth.grok.missingExchangeParams')
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const payload: GrokExchangeCodeRequest = {
          session_id: params.sessionId,
          state: params.state,
          code
        }
        if (params.proxyId) payload.proxy_id = params.proxyId

        return await pending.wait(async () => resourceServices.credentialAuthorization.grok.exchangeCode(payload))
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.grok.errors',
          t('admin.accounts.oauth.grok.failedToExchangeCode')
        )
        appStore.showError(error.value)
        return null
      } finally {
        if (pending.current()) {

          loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return null }
      throw error
    }
  }

  const validateRefreshToken = async (
    refreshToken: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!refreshToken.trim()) {
        error.value = t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
        return null
      }

      loading.value = true
      error.value = ''

      try {
        return await pending.wait(async () => resourceServices.credentialAuthorization.grok.refreshGrokToken(refreshToken.trim(), proxyId))
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.grok.errors',
          t('admin.accounts.oauth.grok.failedToValidateRT')
        )
        return null
      } finally {
        if (pending.current()) {

          loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return null }
      throw error
    }
  }

  // Build account credentials for create/re-auth. Never persist raw SSO cookies
  // or passwords: those exist only for the one-shot authorize API call.
  const buildCredentials = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const credentials: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      token_type: tokenInfo.token_type,
      expires_at: tokenInfo.expires_at,
      client_id: tokenInfo.client_id,
      scope: tokenInfo.scope,
      email: tokenInfo.email,
      sub: tokenInfo.sub,
      team_id: tokenInfo.team_id,
      subscription_tier: tokenInfo.subscription_tier,
      entitlement_status: tokenInfo.entitlement_status,
      // Leave base_url unset so system/CLI mode can choose the correct host.
    }
    if (tokenInfo.refresh_token) credentials.refresh_token = tokenInfo.refresh_token
    if (tokenInfo.id_token) credentials.id_token = tokenInfo.id_token
    const blocked = new Set(['sso_token', 'password', 'sso', 'sso-rw'])
    return Object.fromEntries(
      Object.entries(credentials).filter(
        ([key, value]) => !blocked.has(key) && value !== undefined && value !== ''
      )
    )
  }

  const buildExtraInfo = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const extra: Record<string, unknown> = {}
    if (tokenInfo.email) extra.email = tokenInfo.email
    if (tokenInfo.subscription_tier) extra.subscription_tier = tokenInfo.subscription_tier
    if (tokenInfo.entitlement_status) extra.entitlement_status = tokenInfo.entitlement_status
    return extra
  }

  const validateSSOToken = async (
    ssoToken: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!ssoToken.trim()) {
        error.value = t('admin.accounts.oauth.grok.pleaseEnterSSOToken', 'Please enter an SSO token')
        return null
      }
      loading.value = true
      error.value = ''
      try {
        return await pending.wait(async () => resourceServices.credentialAuthorization.grok.validateSSOToken(ssoToken.trim(), proxyId))
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.grok.errors',
          t('admin.accounts.oauth.grok.failedToValidateSSO', 'Failed to validate SSO token')
        )
        appStore.showError(error.value)
        return null
      } finally {
        if (pending.current()) {

          loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return null }
      throw error
    }
  }

  const authorizePassword = async (
    emailAndPassword: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!emailAndPassword.trim()) {
        error.value = t('admin.accounts.oauth.grok.pleaseEnterPassword', 'Please enter email----password')
        return null
      }
      loading.value = true
      error.value = ''
      try {
        return await pending.wait(async () => resourceServices.credentialAuthorization.grok.authorizePassword(emailAndPassword, proxyId))
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.grok.errors',
          t('admin.accounts.oauth.grok.failedToAuthorizePassword', 'Password authorization failed')
        )
        appStore.showError(error.value)
        return null
      } finally {
        if (pending.current()) {

          loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return null }
      throw error
    }
  }

  watch(editorScope, resetState, { flush: 'sync' })
  if (getCurrentScope()) onScopeDispose(resetState)

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    validateSSOToken,
    authorizePassword,
    buildCredentials,
    buildExtraInfo
  }
}
