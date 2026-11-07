import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceTaskScope, StaleResourceTask } from './resourceTaskScope'
import { useResourceEditorServices } from '@/features/resources/services'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import accountsAPI from '@/features/resources/accounts'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/shared/lib/apiError'

export interface OpenAITokenInfo {
  access_token?: string
  refresh_token?: string
  client_id?: string
  id_token?: string
  token_type?: string
  expires_in?: number
  expires_at?: number
  scope?: string
  email?: string
  name?: string
  plan_type?: string
  subscription_expires_at?: string
  privacy_mode?: string
  // OpenAI specific IDs (extracted from ID Token)
  chatgpt_account_id?: string
  chatgpt_user_id?: string
  organization_id?: string
  [key: string]: unknown
}

export type OpenAIOAuthPlatform = 'openai'

export function useOpenAIOAuth() {
  const resourceTasks = useResourceTaskScope()
  const editorScope = useResourceEditorServices().scopeKey
  const appStore = useResourceEditorServices().notices
  const { t } = useI18n()
  const endpointPrefix = '/admin/openai'

  // State
  const authUrl = ref('')
  const sessionId = ref('')
  const oauthState = ref('')
  const loading = ref(false)
  const error = ref('')

  // Reset state
  const resetState = () => {
    resourceTasks.invalidate()
    authUrl.value = ''
    sessionId.value = ''
    oauthState.value = ''
    loading.value = false
    error.value = ''
  }

  // Generate auth URL for OpenAI OAuth
  let authorizationGeneration = 0

  const generateAuthUrl = async (
    proxyId?: number | null,
    redirectUri?: string
  ): Promise<boolean> => {
    // Supersede authorization work, without cancelling independent capability queries.
    authorizationGeneration++
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      loading.value = true
      authUrl.value = ''
      sessionId.value = ''
      oauthState.value = ''
      error.value = ''

      try {
        const payload: Record<string, unknown> = {}
        if (proxyId) {
          payload.proxy_id = proxyId
        }
        if (redirectUri) {
          payload.redirect_uri = redirectUri
        }

        const response = await pending.wait(async () => accountsAPI.generateAuthUrl(
          `${endpointPrefix}/generate-auth-url`,
          payload
        ))
        authUrl.value = response.auth_url
        sessionId.value = response.session_id
        try {
          const parsed = new URL(response.auth_url)
          oauthState.value = parsed.searchParams.get('state') || ''
        } catch {
          if (!pending.current()) throw new StaleResourceTask()

          oauthState.value = ''
        }
        return true
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.openai.failedToGenerateUrl'))
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

  // Exchange auth code for tokens
  const exchangeAuthCode = async (
    code: string,
    currentSessionId: string,
    state: string,
    proxyId?: number | null
  ): Promise<OpenAITokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!code.trim() || !currentSessionId || !state.trim()) {
        error.value = 'Missing auth code, session ID, or state'
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const payload: { session_id: string; code: string; state: string; proxy_id?: number } = {
          session_id: currentSessionId,
          code: code.trim(),
          state: state.trim()
        }
        if (proxyId) {
          payload.proxy_id = proxyId
        }

        const tokenInfo = await pending.wait(async () => accountsAPI.exchangeCode(`${endpointPrefix}/exchange-code`, payload))
        return tokenInfo as OpenAITokenInfo
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.openai.errors',
          t('admin.accounts.oauth.openai.failedToExchangeCode')
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

  // Validate refresh token and get full token info
  // clientId: 指定 OAuth client_id（用于第三方渠道获取的 RT，如 app_LlGpXReQgckcGGUo2JrYvtJK）
  const validateRefreshToken = async (
    refreshToken: string,
    proxyId?: number | null,
    clientId?: string
  ): Promise<OpenAITokenInfo | null> => {
    const pending = resourceTasks.capture(() => authorizationGeneration)
    try {

      if (!refreshToken.trim()) {
        error.value = 'Missing refresh token'
        return null
      }

      loading.value = true
      error.value = ''

      try {
        // Use dedicated refresh-token endpoint
        const tokenInfo = await pending.wait(async () => accountsAPI.refreshOpenAIToken(
          refreshToken.trim(),
          proxyId,
          `${endpointPrefix}/refresh-token`,
          clientId
        ))
        return tokenInfo as OpenAITokenInfo
      } catch (err: unknown) {
        if (!pending.current()) throw new StaleResourceTask()

        error.value = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.openai.errors',
          t('admin.accounts.oauth.openai.failedToValidateRT')
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

  // Build credentials for OpenAI OAuth account (aligned with backend BuildAccountCredentials)
  const buildCredentials = (tokenInfo: OpenAITokenInfo): Record<string, unknown> => {
    const creds: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      expires_at: tokenInfo.expires_at
    }

    // 仅在返回了新的 refresh_token 时才写入，防止用空值覆盖已有令牌
    if (tokenInfo.refresh_token) {
      creds.refresh_token = tokenInfo.refresh_token
    }
    if (tokenInfo.id_token) {
      creds.id_token = tokenInfo.id_token
    }
    if (tokenInfo.email) {
      creds.email = tokenInfo.email
    }
    if (tokenInfo.chatgpt_account_id) {
      creds.chatgpt_account_id = tokenInfo.chatgpt_account_id
    }
    if (tokenInfo.chatgpt_user_id) {
      creds.chatgpt_user_id = tokenInfo.chatgpt_user_id
    }
    if (tokenInfo.organization_id) {
      creds.organization_id = tokenInfo.organization_id
    }
    if (tokenInfo.plan_type) {
      creds.plan_type = tokenInfo.plan_type
    }
    if (tokenInfo.subscription_expires_at) {
      creds.subscription_expires_at = tokenInfo.subscription_expires_at
    }
    if (tokenInfo.client_id) {
      creds.client_id = tokenInfo.client_id
    }

    return creds
  }

  // Build extra info from token response
  const buildExtraInfo = (tokenInfo: OpenAITokenInfo): Record<string, string> | undefined => {
    const extra: Record<string, string> = {}
    if (tokenInfo.email) {
      extra.email = tokenInfo.email
    }
    if (tokenInfo.name) {
      extra.name = tokenInfo.name
    }
    if (tokenInfo.privacy_mode) {
      extra.privacy_mode = tokenInfo.privacy_mode
    }
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  watch(editorScope, resetState, { flush: 'sync' })
  if (getCurrentScope()) onScopeDispose(resetState)

  return {
    // State
    authUrl,
    sessionId,
    oauthState,
    loading,
    error,
    // Methods
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    buildCredentials,
    buildExtraInfo
  }
}
