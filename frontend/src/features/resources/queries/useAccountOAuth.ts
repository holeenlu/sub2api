import { readApiError } from '@/shared/lib/apiError'
import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceTaskScope, StaleResourceTask } from './resourceTaskScope'
import { useResourceEditorServices } from '@/features/resources/services'
import { ref } from 'vue'

import accountsAPI from '@/features/resources/accounts'

export type AddMethod = 'oauth' | 'setup-token' | 'setup-token-manual'
export type AuthInputMethod =
  | 'manual'
  | 'setup_token'
  | 'cookie'
  | 'refresh_token'
  | 'mobile_refresh_token'
  | 'session_token'
  | 'access_token'
  | 'codex_session'
  | 'agent_identity'
  | 'codex_pat'
  | 'sso_cookie'
  | 'email_password'

export interface OAuthState {
  authUrl: string
  authCode: string
  sessionId: string
  sessionKey: string
  loading: boolean
  error: string
}

export interface TokenInfo {
  org_uuid?: string
  account_uuid?: string
  email_address?: string
  [key: string]: unknown
}

export function normalizeClaudeSetupToken(raw: string): string {
  let token = raw.trim()
  const assignment = token.match(/^export\s+CLAUDE_CODE_OAUTH_TOKEN\s*=\s*(.+)$/i)
  if (assignment) token = assignment[1].trim()
  if ((token.startsWith('"') && token.endsWith('"')) || (token.startsWith("'") && token.endsWith("'"))) {
    token = token.slice(1, -1).trim()
  }
  return token
}

export function buildClaudeSetupTokenCredentials(raw: string): Record<string, unknown> | null {
  const accessToken = normalizeClaudeSetupToken(raw)
  if (!accessToken || !accessToken.startsWith('sk-ant-oat')) return null
  return {
    access_token: accessToken,
    token_type: 'oauth',
    scope: 'user:inference'
  }
}

export function useAccountOAuth() {
  const resourceTasks = useResourceTaskScope()
  const editorScope = useResourceEditorServices().scopeKey
  const appStore = useResourceEditorServices().notices

  // State
  const authUrl = ref('')
  const authCode = ref('')
  const sessionId = ref('')
  const sessionKey = ref('')
  const loading = ref(false)
  const error = ref('')

  let generation = 0

  // Reset state
  const resetState = () => {
    resourceTasks.invalidate()
    generation++
    authUrl.value = ''
    authCode.value = ''
    sessionId.value = ''
    sessionKey.value = ''
    loading.value = false
    error.value = ''
  }

  // Generate auth URL
  const generateAuthUrl = async (
    addMethod: AddMethod,
    proxyId?: number | null
  ): Promise<boolean> => {
    // A new authorization session supersedes prior URL and token-exchange results.
    const epoch = ++generation
    const pending = resourceTasks.capture(() => generation)
    try {

      if (addMethod === 'setup-token-manual') return false
      loading.value = true
      authUrl.value = ''
      sessionId.value = ''
      error.value = ''

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth'
            ? '/admin/accounts/generate-auth-url'
            : '/admin/accounts/generate-setup-token-url'

        const response = await pending.wait(async () => accountsAPI.generateAuthUrl(endpoint, proxyConfig))
        if (epoch !== generation) return false
        authUrl.value = response.auth_url
        sessionId.value = response.session_id
        return true
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        if (epoch !== generation) return false
        error.value = errorDetails.response?.data?.detail || 'Failed to generate auth URL'
        appStore.showError(error.value)
        return false
      } finally {
        if (pending.current()) {

          if (epoch === generation) loading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return false }
      throw error
    }
  }

  // Exchange auth code for tokens
  const exchangeAuthCode = async (
    addMethod: AddMethod,
    proxyId?: number | null
  ): Promise<TokenInfo | null> => {
    const pending = resourceTasks.capture(() => generation)
    try {

      if (!authCode.value.trim() || !sessionId.value) {
        error.value = 'Missing auth code or session ID'
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth'
            ? '/admin/accounts/exchange-code'
            : '/admin/accounts/exchange-setup-token-code'

        const tokenInfo = await pending.wait(async () => accountsAPI.exchangeCode(endpoint, {
          session_id: sessionId.value,
          code: authCode.value.trim(),
          ...proxyConfig
        }))

        return tokenInfo as TokenInfo
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        error.value = errorDetails.response?.data?.detail || 'Failed to exchange auth code'
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

  // Cookie-based authentication
  const cookieAuth = async (
    addMethod: AddMethod,
    sessionKeyValue: string,
    proxyId?: number | null
  ): Promise<TokenInfo | null> => {
    const pending = resourceTasks.capture(() => generation)
    try {

      if (!sessionKeyValue.trim()) {
        error.value = 'Please enter sessionKey'
        return null
      }

      loading.value = true
      error.value = ''

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth'
            ? '/admin/accounts/cookie-auth'
            : '/admin/accounts/setup-token-cookie-auth'

        const tokenInfo = await pending.wait(async () => accountsAPI.exchangeCode(endpoint, {
          session_id: '',
          code: sessionKeyValue.trim(),
          ...proxyConfig
        }))

        return tokenInfo as TokenInfo
      } catch (err: unknown) {
        const errorDetails = readApiError(err)
        if (!pending.current()) throw new StaleResourceTask()

        error.value = errorDetails.response?.data?.detail || 'Cookie authorization failed'
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

  // Parse multiple session keys
  const parseSessionKeys = (input: string): string[] => {
    return input
      .split('\n')
      .map((k) => k.trim())
      .filter((k) => k)
  }

  // Build extra info from token response
  const buildExtraInfo = (tokenInfo: TokenInfo): Record<string, string> | undefined => {
    const extra: Record<string, string> = {}
    if (tokenInfo.org_uuid) {
      extra.org_uuid = tokenInfo.org_uuid
    }
    if (tokenInfo.account_uuid) {
      extra.account_uuid = tokenInfo.account_uuid
    }
    if (tokenInfo.email_address) {
      extra.email_address = tokenInfo.email_address
    }
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  watch(editorScope, resetState, { flush: 'sync' })
  if (getCurrentScope()) onScopeDispose(resetState)

  return {
    // State
    authUrl,
    authCode,
    sessionId,
    sessionKey,
    loading,
    error,
    // Methods
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    cookieAuth,
    parseSessionKeys,
    buildExtraInfo
  }
}
