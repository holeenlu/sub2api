import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Account } from '@/types'

export type AddMethod = 'oauth' | 'setup-token'
export type AuthInputMethod =
  | 'manual'
  | 'cookie'
  | 'setup_token'
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

/** Prefix of the long-lived token printed by `claude setup-token`. */
export const CLAUDE_SETUP_TOKEN_PREFIX = 'sk-ant-oat01-'
// Real tokens carry a long opaque body after the prefix; anything shorter is a
// truncated paste rather than a token.
const CLAUDE_SETUP_TOKEN_MIN_LENGTH = CLAUDE_SETUP_TOKEN_PREFIX.length + 20

export const CLAUDE_SETUP_TOKEN_INVALID_KEY = 'admin.accounts.oauth.invalidSetupToken'

/**
 * Raised by the setup-token helpers. `messageKey` is an i18n key so callers
 * render it with `t()` instead of matching on English text.
 */
export class ClaudeSetupTokenError extends Error {
  readonly messageKey: string

  constructor(messageKey: string) {
    super(messageKey)
    this.name = 'ClaudeSetupTokenError'
    this.messageKey = messageKey
  }
}

/** Split a pasted block into one token per line; blank lines and repeats are dropped. */
export const parseClaudeSetupTokens = (input: string): string[] => {
  const seen = new Set<string>()
  const tokens: string[] = []
  for (const line of input.split('\n')) {
    const token = line.trim()
    if (!token || seen.has(token)) continue
    seen.add(token)
    tokens.push(token)
  }
  return tokens
}

export const isClaudeSetupToken = (rawToken: string): boolean => {
  const token = rawToken.trim()
  return (
    token.startsWith(CLAUDE_SETUP_TOKEN_PREFIX) &&
    token.length >= CLAUDE_SETUP_TOKEN_MIN_LENGTH &&
    !/\s/.test(token)
  )
}

/**
 * Build the credentials stored for a directly imported setup token.
 *
 * `claude setup-token` issues a long-lived, inference-only bearer token. It is
 * deliberately stored without `expires_at` / `refresh_token`: there is nothing
 * to refresh, and the presence of either would enrol the account in the token
 * refresher, which would swap the long-lived token for a short-lived one.
 */
export const buildClaudeSetupTokenCredentials = (rawToken: string): TokenInfo => {
  const accessToken = rawToken.trim()
  if (!isClaudeSetupToken(accessToken)) {
    throw new ClaudeSetupTokenError(CLAUDE_SETUP_TOKEN_INVALID_KEY)
  }
  return {
    access_token: accessToken,
    token_type: 'Bearer',
    scope: 'user:inference'
  }
}

/**
 * Re-authorize an existing account with one directly pasted setup token.
 *
 * Shared by both ReAuthAccountModal copies. Re-auth goes through
 * apply-oauth-credentials, which replaces the whole token set server-side: the
 * previous refresh_token / expires_at are dropped (otherwise one refresh would
 * swap the long-lived token back out) while non-token credential keys such as
 * model_mapping survive.
 */
export const applyClaudeSetupTokenReAuthorization = async (
  accountID: number,
  setupTokenInput: string
): Promise<Account> => {
  const setupTokens = parseClaudeSetupTokens(setupTokenInput)
  if (setupTokens.length !== 1) {
    throw new ClaudeSetupTokenError('admin.accounts.oauth.pleaseEnterSetupToken')
  }
  const credentials = buildClaudeSetupTokenCredentials(setupTokens[0])
  return adminAPI.accounts.applyOAuthCredentials(accountID, {
    type: 'setup-token',
    credentials: credentials as Record<string, unknown>
  })
}

/** Render a setup-token helper error via its i18n key, or fall back to the API error text. */
export const describeClaudeSetupTokenError = (
  err: unknown,
  t: (key: string) => string,
  fallback: string
): string => {
  if (err instanceof ClaudeSetupTokenError) return t(err.messageKey)
  return extractApiErrorMessage(err, fallback)
}

export function useAccountOAuth() {
  const appStore = useAppStore()
  const { t } = useI18n()

  // State
  const authUrl = ref('')
  const authCode = ref('')
  const sessionId = ref('')
  const sessionKey = ref('')
  const loading = ref(false)
  const error = ref('')

  // Reset state
  const resetState = () => {
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
    // Setup tokens are pasted from `claude setup-token`; there is no browser flow.
    if (addMethod === 'setup-token') {
      error.value = t('admin.accounts.oauth.pleaseEnterSetupToken')
      return false
    }

    loading.value = true
    authUrl.value = ''
    sessionId.value = ''
    error.value = ''

    try {
      const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
      const response = await adminAPI.accounts.generateAuthUrl(
        '/admin/accounts/generate-auth-url',
        proxyConfig
      )
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      return true
    } catch (err: any) {
      error.value = err.response?.data?.detail || 'Failed to generate auth URL'
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
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
    parseSessionKeys,
    buildExtraInfo
  }
}
