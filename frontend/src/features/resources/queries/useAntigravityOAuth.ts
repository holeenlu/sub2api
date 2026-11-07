import { readApiError } from '@/shared/lib/apiError'
import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceTaskScope, StaleResourceTask } from './resourceTaskScope'
import { useResourceEditorServices } from '@/features/resources/services'
import { ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'


import type { AntigravityAuthUrlRequest, AntigravityExchangeCodeRequest, AntigravityTokenInfo } from '@/features/resources/antigravity'

export interface AntigravityOAuth {
  authUrl: Ref<string>
  sessionId: Ref<string>
  state: Ref<string>
  loading: Ref<boolean>
  error: Ref<string>
  resetState(): void
  generateAuthUrl(proxyId: number | null | undefined): Promise<boolean>
  exchangeAuthCode(params: { code: string; sessionId: string; state: string; proxyId?: number | null }): Promise<AntigravityTokenInfo | null>
  validateRefreshToken(refreshToken: string, proxyId?: number | null): Promise<AntigravityTokenInfo | null>
  buildCredentials(tokenInfo: AntigravityTokenInfo, fallbackRefreshToken?: string): Record<string, unknown>
}

export function useAntigravityOAuth(): AntigravityOAuth {
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
        const payload: AntigravityAuthUrlRequest = {}
        if (proxyId) payload.proxy_id = proxyId

        const response = await pending.wait(async () => resourceServices.credentialAuthorization.antigravity.generateAuthUrl(payload))
        authUrl.value = response.auth_url
        sessionId.value = response.session_id
        state.value = response.state
        return true
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        error.value =
          errorDetails.response?.data?.detail || t('admin.accounts.oauth.antigravity.failedToGenerateUrl')
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
  }): Promise<AntigravityTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      const code = params.code?.trim()
      if (!code || !params.sessionId || !params.state) {
        error.value = t('admin.accounts.oauth.antigravity.missingExchangeParams')
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const payload: AntigravityExchangeCodeRequest = {
          session_id: params.sessionId,
          state: params.state,
          code
        }
        if (params.proxyId) payload.proxy_id = params.proxyId

        return await pending.wait(async () => resourceServices.credentialAuthorization.antigravity.exchangeCode(payload))
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        error.value =
          errorDetails.response?.data?.detail || t('admin.accounts.oauth.antigravity.failedToExchangeCode')
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
  ): Promise<AntigravityTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!refreshToken.trim()) {
        error.value = t('admin.accounts.oauth.antigravity.pleaseEnterRefreshToken')
        return null
      }

      loading.value = true
      error.value = ''

      try {
        return await pending.wait(async () => resourceServices.credentialAuthorization.antigravity.refreshAntigravityToken(
          refreshToken.trim(),
          proxyId
        ))
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        error.value =
          errorDetails.response?.data?.detail || t('admin.accounts.oauth.antigravity.failedToValidateRT')
        // Don't show global error toast for batch validation to avoid spamming
        // appStore.showError(error.value)
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

  const buildCredentials = (
    tokenInfo: AntigravityTokenInfo,
    fallbackRefreshToken?: string
  ): Record<string, unknown> => {
    let expiresAt: string | undefined
    if (typeof tokenInfo.expires_at === 'number' && Number.isFinite(tokenInfo.expires_at)) {
      expiresAt = Math.floor(tokenInfo.expires_at).toString()
    } else if (typeof tokenInfo.expires_at === 'string' && tokenInfo.expires_at.trim()) {
      expiresAt = tokenInfo.expires_at.trim()
    }
    const refreshToken = tokenInfo.refresh_token?.trim()
      ? tokenInfo.refresh_token
      : fallbackRefreshToken

    return {
      access_token: tokenInfo.access_token,
      refresh_token: refreshToken,
      token_type: tokenInfo.token_type,
      expires_at: expiresAt,
      project_id: tokenInfo.project_id,
      email: tokenInfo.email,
      ...(tokenInfo.plan_type ? { plan_type: tokenInfo.plan_type } : {})
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
    buildCredentials
  }
}
