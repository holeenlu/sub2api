import { readApiError } from '@/shared/lib/apiError'
import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceTaskScope, StaleResourceTask } from './resourceTaskScope'
import { useResourceEditorServices } from '@/features/resources/services'
import { ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'


import { extractApiErrorMessage } from '@/shared/lib/apiError'
import type { GeminiAuthUrlRequest, GeminiExchangeCodeRequest, GeminiOAuthCapabilities, GeminiTokenInfo } from '@/features/resources/gemini'

export interface GeminiOAuth {
  authUrl: Ref<string>
  sessionId: Ref<string>
  state: Ref<string>
  loading: Ref<boolean>
  error: Ref<string>
  resetState(): void
  generateAuthUrl(proxyId: number | null | undefined, projectId?: string | null, oauthType?: GeminiAuthUrlRequest['oauth_type'], tierId?: string): Promise<boolean>
  exchangeAuthCode(params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
    oauthType?: GeminiExchangeCodeRequest['oauth_type']
    tierId?: string
  }): Promise<GeminiTokenInfo | null>
  buildCredentials(tokenInfo: GeminiTokenInfo): Record<string, unknown>
  buildExtraInfo(tokenInfo: GeminiTokenInfo): Record<string, unknown> | undefined
  getCapabilities(): Promise<GeminiOAuthCapabilities | null>
}

export function useGeminiOAuth(): GeminiOAuth {
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

  const generateAuthUrl = async (
    proxyId: number | null | undefined,
    projectId?: string | null,
    oauthType?: GeminiAuthUrlRequest['oauth_type'],
    tierId?: string
  ): Promise<boolean> => {
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
        const payload: GeminiAuthUrlRequest = {}
        if (proxyId) payload.proxy_id = proxyId
        const trimmedProjectID = projectId?.trim()
        if (trimmedProjectID) payload.project_id = trimmedProjectID
        if (oauthType) payload.oauth_type = oauthType
        const trimmedTierID = tierId?.trim()
        if (trimmedTierID) payload.tier_id = trimmedTierID

        const response = await pending.wait(async () => resourceServices.credentialAuthorization.gemini.generateAuthUrl(payload))
        authUrl.value = response.auth_url
        sessionId.value = response.session_id
        state.value = response.state
        return true
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.gemini.failedToGenerateUrl'))
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
    oauthType?: GeminiExchangeCodeRequest['oauth_type']
    tierId?: string
  }): Promise<GeminiTokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      const code = params.code?.trim()
      if (!code || !params.sessionId || !params.state) {
        error.value = t('admin.accounts.oauth.gemini.missingExchangeParams')
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const payload: GeminiExchangeCodeRequest = {
          session_id: params.sessionId,
          state: params.state,
          code
        }
        if (params.proxyId) payload.proxy_id = params.proxyId
        if (params.oauthType) payload.oauth_type = params.oauthType
        const trimmedTierID = params.tierId?.trim()
        if (trimmedTierID) payload.tier_id = trimmedTierID

        return await pending.wait(async () => resourceServices.credentialAuthorization.gemini.exchangeCode(payload))
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        // Check for specific missing project_id error
        const errorMessage = errorDetails.message || errorDetails.response?.data?.message || ''
        if (errorMessage.includes('missing project_id')) {
          error.value = t('admin.accounts.oauth.gemini.missingProjectId')
        } else {
          error.value = errorMessage || t('admin.accounts.oauth.gemini.failedToExchangeCode')
        }
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

  const buildCredentials = (tokenInfo: GeminiTokenInfo): Record<string, unknown> => {
    let expiresAt: string | undefined
    if (typeof tokenInfo.expires_at === 'number' && Number.isFinite(tokenInfo.expires_at)) {
      expiresAt = Math.floor(tokenInfo.expires_at).toString()
    } else if (typeof tokenInfo.expires_at === 'string' && tokenInfo.expires_at.trim()) {
      expiresAt = tokenInfo.expires_at.trim()
    }

    return {
      access_token: tokenInfo.access_token,
      refresh_token: tokenInfo.refresh_token,
      token_type: tokenInfo.token_type,
      expires_at: expiresAt,
      scope: tokenInfo.scope,
      project_id: tokenInfo.project_id,
      oauth_type: tokenInfo.oauth_type,
      tier_id: tokenInfo.tier_id
    }
  }

  const buildExtraInfo = (tokenInfo: GeminiTokenInfo): Record<string, unknown> | undefined => {
    if (!tokenInfo.extra || typeof tokenInfo.extra !== 'object') return undefined
    return tokenInfo.extra
  }

  const getCapabilities = async (): Promise<GeminiOAuthCapabilities | null> => {
    const pending = resourceTasks.capture()
    try {

      try {
        return await pending.wait(async () => resourceServices.credentialAuthorization.gemini.getCapabilities())
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        // Capabilities are optional for older servers; don't block the UI.
        return null
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
    buildCredentials,
    buildExtraInfo,
    getCapabilities
  }
}
