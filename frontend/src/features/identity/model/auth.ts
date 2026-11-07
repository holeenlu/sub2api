/**
 * Authentication Store
 * Manages user authentication state, login/logout, token refresh, and token persistence
 */

import { defineStore } from 'pinia'
import { CanceledError, isCancel } from 'axios'
import { authSessionStamp, readAuthSessionSnapshot, AUTH_TOKEN_KEY, AUTH_USER_KEY, REFRESH_TOKEN_KEY, TOKEN_EXPIRES_AT_KEY } from '@/shared/session'
import { ref, computed, readonly, onScopeDispose } from 'vue'
import { authAPI, isTotp2FARequired, type LoginResponse } from '../api/auth'
import { passkeyAPI } from '../api/passkey'
import type {
  LoginRequest,
  RegisterRequest,
  AuthResponse,
  ActionCaptchaRequestProof
} from '../api/types'
import type { User } from '../api/profileTypes'
import { getPersistedPendingAuthSession, persistPendingAuthSession, clearPendingAuthSessionStorage, type PendingAuthSessionSummary } from './pendingSession'

const AUTO_REFRESH_INTERVAL = 60 * 1000 // 60 seconds for user data refresh
const TOKEN_REFRESH_BUFFER = 120 * 1000 // 120 seconds before expiry to refresh token

export const useAuthStore = defineStore('auth', () => {
  // ==================== State ====================

  const user = ref<User | null>(null)
  const token = ref<string | null>(null)
  const sessionEpoch = ref(0)
  let logicalSession = ''
  let disposed = false

  function updateSessionToken(nextToken: string | null): void {
    const nextSession = authSessionStamp(nextToken)
    if (nextSession !== logicalSession) {
      logicalSession = nextSession
      sessionEpoch.value += 1
    }
    token.value = nextToken
  }

  // This app-facing key contains no credentials and changes for a new same-user session.
  const identityScopeKey = computed(() => JSON.stringify([
    sessionEpoch.value, user.value?.id, user.value?.role, user.value?.policy_version
  ]))
  const refreshTokenValue = ref<string | null>(null)
  const tokenExpiresAt = ref<number | null>(null) // 过期时间戳（毫秒）
  const runMode = ref<'standard' | 'simple'>('standard')
  const pendingAuthSession = ref<PendingAuthSessionSummary | null>(null)
  let refreshIntervalId: ReturnType<typeof setInterval> | null = null
  let tokenRefreshTimeoutId: ReturnType<typeof setTimeout> | null = null
  // Restoring or replacing auth invalidates pending refresh work even if the logical session is unchanged.
  let authSessionGeneration = 0


  // ==================== Computed ====================

  const isAuthenticated = computed(() => {
    return !!token.value && !!user.value
  })

  const isAdmin = computed(() => {
    return user.value?.role === 'admin' || user.value?.role === 'super_admin'
  })

  const isSuperAdmin = computed(() => user.value?.role === 'super_admin')
  function can(permission: string): boolean {
    return isSuperAdmin.value || (user.value?.role === 'admin' && (user.value.permissions || []).includes(permission))
  }
  function canEditAdminField(schema: string, field: string): boolean {
    return isSuperAdmin.value || (user.value?.role === 'admin' && (user.value.admin_write_fields?.[schema] || []).includes(field))
  }
  function editableAdminFields<T extends object>(schema: string, payload: T): T {
    if (isSuperAdmin.value) return payload
    return Object.fromEntries(Object.entries(payload).filter(([field]) => canEditAdminField(schema, field))) as T
  }
  function canAccessAdminPage(path: string): boolean {
    if (!isAdmin.value) return false
    if (isSuperAdmin.value) return true
    const normalized = path.split('?')[0].replace(/\/$/, '')
    if (normalized === '/admin/ops' && user.value?.admin_features?.ops_monitoring_enabled === false) return false
    return (user.value?.admin_pages || []).some(page => normalized === page)
  }
  const adminLandingPath = computed(() => {
    if (!isAdmin.value) return '/dashboard'
    if (canAccessAdminPage('/admin/dashboard')) return '/admin/dashboard'
    return user.value?.admin_pages?.find(page => canAccessAdminPage(page)) || '/dashboard'
  })

  const isSimpleMode = computed(() => runMode.value === 'simple')
  const hasPendingAuthSession = computed(() => pendingAuthSession.value !== null)

  // ==================== Actions ====================

  /**
   * Initialize auth state from localStorage
   * Call this on app startup to restore session
   * Also starts auto-refresh and immediately fetches latest user data
   */
  function checkAuth(): void {
    authSessionGeneration += 1
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const savedUser = localStorage.getItem(AUTH_USER_KEY)
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
    pendingAuthSession.value = getPersistedPendingAuthSession()

    if (savedToken && savedUser) {
      try {
        updateSessionToken(savedToken)
        user.value = JSON.parse(savedUser)
        refreshTokenValue.value = savedRefreshToken
        tokenExpiresAt.value = savedExpiresAt ? parseInt(savedExpiresAt, 10) : null

        // Immediately refresh user data from backend (async, don't block)
        refreshUser().catch((error) => {
          console.error('Failed to refresh user on init:', error)
        })

        // Start auto-refresh interval for user data
        startAutoRefresh()

        // Start proactive token refresh if we have refresh token and expiry info
        // Note: use !== null to handle case when tokenExpiresAt.value is 0 (expired)
        if (savedRefreshToken && tokenExpiresAt.value !== null) {
          scheduleTokenRefreshAt(tokenExpiresAt.value)
        }
      } catch (error) {
        console.error('Failed to parse saved user data:', error)
        clearAuth({ preservePendingAuthSession: true })
      }
    } else if (token.value) {
      clearAuth({ preservePendingAuthSession: true })
    }
  }

  /**
   * Start auto-refresh interval for user data
   * Refreshes user data every 60 seconds
   */
  function startAutoRefresh(): void {
    // Clear existing interval if any
    stopAutoRefresh()

    refreshIntervalId = setInterval(() => {
      if (token.value) {
        refreshUser().catch((error) => {
          console.error('Auto-refresh user failed:', error)
        })
      }
    }, AUTO_REFRESH_INTERVAL)
  }

  /**
   * Stop auto-refresh interval
   */
  function stopAutoRefresh(): void {
    if (refreshIntervalId) {
      clearInterval(refreshIntervalId)
      refreshIntervalId = null
    }
  }

  /**
   * Schedule proactive token refresh before expiry (based on expiry timestamp)
   * @param expiresAtMs - Token expiry timestamp in milliseconds
   */
  function scheduleTokenRefreshAt(expiresAtMs: number): void {
    // Clear any existing timeout
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }

    // Calculate remaining time until refresh (buffer time before expiry)
    const now = Date.now()
    const refreshInMs = Math.max(0, expiresAtMs - now - TOKEN_REFRESH_BUFFER)

    if (refreshInMs <= 0) {
      // Token is about to expire or already expired, refresh immediately
      performTokenRefresh()
      return
    }

    tokenRefreshTimeoutId = setTimeout(() => {
      performTokenRefresh()
    }, refreshInMs)
  }

  /**
   * Schedule proactive token refresh before expiry (based on expires_in seconds)
   * @param expiresInSeconds - Token expiry time in seconds from now
   */
  function scheduleTokenRefresh(expiresInSeconds: number): void {
    const expiresAtMs = Date.now() + expiresInSeconds * 1000
    tokenExpiresAt.value = expiresAtMs
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(expiresAtMs))
    scheduleTokenRefreshAt(expiresAtMs)
  }

  /**
   * Perform the actual token refresh
   */
  async function performTokenRefresh(): Promise<void> {
    const refreshTokenSnapshot = refreshTokenValue.value
    const userIDSnapshot = user.value?.id
    const sessionGeneration = authSessionGeneration
    if (!refreshTokenSnapshot || userIDSnapshot == null) {
      return
    }

    try {
      const response = await authAPI.refreshToken()

      const persisted = readAuthSessionSnapshot()
      if (
        disposed ||
        authSessionGeneration !== sessionGeneration ||
        refreshTokenValue.value !== refreshTokenSnapshot ||
        user.value?.id !== userIDSnapshot ||
        persisted.userID !== userIDSnapshot ||
        persisted.accessToken !== response.access_token ||
        persisted.refreshToken !== response.refresh_token ||
        persisted.expiresAt === null
      ) {
        return
      }

      // Update state
      updateSessionToken(response.access_token)
      refreshTokenValue.value = response.refresh_token

      // The coordinator owns persistence; adopt its deadline without extending it on delayed delivery.
      tokenExpiresAt.value = persisted.expiresAt
      scheduleTokenRefreshAt(persisted.expiresAt)
    } catch (error) {
      if (!isCancel(error)) console.error('Token refresh failed:', error)
      // Don't clear auth here - the interceptor will handle 401 errors
    }
  }

  /**
   * Stop token refresh timeout
   */
  function stopTokenRefresh(): void {
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }
  }

  /**
   * User login
   * @param credentials - Login credentials (email and password)
   * @returns Promise resolving to the login response (may require 2FA)
   * @throws Error if login fails
   */
  async function login(credentials: LoginRequest): Promise<LoginResponse> {
    const epoch = sessionEpoch.value
    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      const response = await authAPI.login(credentials)
      if (disposed || epoch !== sessionEpoch.value) throw new CanceledError('Authentication changed')

      // If 2FA is required, return the response without setting auth state
      if (isTotp2FARequired(response)) {
        return response
      }

      // Set auth state from the response
      setAuthFromResponse(response)

      return response
    } catch (error) {
      // Clear any partial state on error
      if (!isCancel(error)) clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      throw error
    }
  }

  /**
   * Complete login with 2FA code
   * @param tempToken - Temporary token from initial login
   * @param totpCode - 6-digit TOTP code
   * @returns Promise resolving to the authenticated user
   * @throws Error if 2FA verification fails
   */
  async function login2FA(tempToken: string, totpCode: string): Promise<User> {
    const epoch = sessionEpoch.value
    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      const response = await authAPI.login2FA({ temp_token: tempToken, totp_code: totpCode })
      if (disposed || epoch !== sessionEpoch.value) throw new CanceledError('Authentication changed')
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      if (!isCancel(error)) clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      throw error
    }
  }

  async function loginWithPasskey(proof?: ActionCaptchaRequestProof): Promise<User> {
    const epoch = sessionEpoch.value
    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      const response = await passkeyAPI.login(proof)
      if (disposed || epoch !== sessionEpoch.value) throw new CanceledError('Authentication changed')
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      if (!isCancel(error)) clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      throw error
    }
  }

  /**
   * Set auth state from an AuthResponse
   * Internal helper function
   */
  function setAuthFromResponse(response: AuthResponse): void {
    authSessionGeneration += 1
    // Store token and user
    stopAutoRefresh()
    stopTokenRefresh()
    updateSessionToken(response.access_token)

    // Store refresh token if present
    if (response.refresh_token) {
      refreshTokenValue.value = response.refresh_token
      localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
    } else {
      refreshTokenValue.value = null
      localStorage.removeItem(REFRESH_TOKEN_KEY)
    }
    tokenExpiresAt.value = null
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)

    // Extract run_mode if present
    runMode.value = response.user.run_mode || 'standard'
    const { run_mode: _run_mode, ...userData } = response.user
    user.value = userData

    // Persist to localStorage
    localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))
    clearPendingAuthSession()

    // Start auto-refresh interval for user data
    startAutoRefresh()

    // Start proactive token refresh if we have refresh token and expiry info
    // scheduleTokenRefresh will also store the expiry timestamp
    if (response.refresh_token && response.expires_in) {
      scheduleTokenRefresh(response.expires_in)
    }
  }

  /**
   * User registration
   * @param userData - Registration data (username, email, password)
   * @returns Promise resolving to the newly registered and authenticated user
   * @throws Error if registration fails
   */
  async function register(userData: RegisterRequest): Promise<User> {
    const epoch = sessionEpoch.value
    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      const response = await authAPI.register(userData)
      if (disposed || epoch !== sessionEpoch.value) throw new CanceledError('Authentication changed')

      // Use the common helper to set auth state
      setAuthFromResponse(response)

      return user.value!
    } catch (error) {
      // Clear any partial state on error
      if (!isCancel(error)) clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      throw error
    }
  }

  /**
   * 直接设置 token（用于 OAuth/SSO 回调），并加载当前用户信息。
   * 会自动读取 localStorage 中已设置的 refresh_token 和 token_expires_in
   * @param newToken - 后端签发的 JWT access token
   */
  async function setToken(newToken: string): Promise<User> {
    authSessionGeneration += 1
    // Clear any previous state first (avoid mixing sessions)
    // Note: Don't clear localStorage here as OAuth callback may have set refresh_token
    stopAutoRefresh()
    stopTokenRefresh()
    token.value = null
    user.value = null

    updateSessionToken(newToken)
    localStorage.setItem(AUTH_TOKEN_KEY, newToken)

    // Read refresh token and expires_at from localStorage if set by OAuth callback
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)

    refreshTokenValue.value = savedRefreshToken
    tokenExpiresAt.value = savedExpiresAt ? parseInt(savedExpiresAt, 10) : null

    const session = authSessionStamp(newToken)
    try {
      const userData = await refreshUser()
      startAutoRefresh()

      // Start proactive token refresh if we have refresh token and expiry info
      // Note: use !== null to handle case when tokenExpiresAt.value is 0 (expired)
      if (savedRefreshToken && tokenExpiresAt.value !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt.value)
      }

      clearPendingAuthSession()
      return userData
    } catch (error) {
      if (!isCancel(error)) clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      throw error
    }
  }

  function setPendingAuthSession(session: PendingAuthSessionSummary | null): void {
    pendingAuthSession.value = session

    if (session) {
      persistPendingAuthSession(session)
      return
    }

    clearPendingAuthSessionStorage()
  }

  function clearPendingAuthSession(): void {
    setPendingAuthSession(null)
  }

  /**
   * User logout
   * Clears all authentication state and persisted data
   */
  async function logout(): Promise<void> {
    authSessionGeneration += 1
    stopAutoRefresh()
    stopTokenRefresh()
    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      // Call API logout (revokes refresh token on server)
      await authAPI.logout()
    } catch (err) {
      // 服务端吊销失败（网络/5xx/超时）不应阻止本地登出，否则用户点了退出仍处于登录态。
      if (!isCancel(err)) console.warn('Logout API call failed, clearing local session anyway', err)
    } finally {
      // A late logout must not clear a different login that has replaced it.
      clearAuth({ expectedSession: session })
    }
  }

  /**
   * Refresh current user data
   * Fetches latest user info from the server
   * @returns Promise resolving to the updated user
   * @throws Error if not authenticated or request fails
   */
  async function refreshUser(): Promise<User> {
    if (!token.value) {
      throw new Error('Not authenticated')
    }

    const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    try {
      const response = await authAPI.getCurrentUser()
      if (disposed || session !== authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))) throw new CanceledError('Authentication changed')
      if (response.data.run_mode) {
        runMode.value = response.data.run_mode
      }
      const { run_mode: _run_mode, ...userData } = response.data
      user.value = userData

      // Update localStorage
      localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))

      return userData
    } catch (error) {
      // If refresh fails with 401, clear auth state
      if ((error as { status?: number }).status === 401) {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null, expectedSession: session })
      }
      throw error
    }
  }

  /**
   * Clear all authentication state
   * Internal helper function
   */
  function clearAuth(options?: { preservePendingAuthSession?: boolean; expectedSession?: string }): void {
    if (options?.expectedSession !== undefined) {
      // A terminal 401 may already have removed persisted tokens. In that case
      // clear only the matching in-memory session, preserving any newer login.
      const currentSession = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY) || token.value)
      if (options.expectedSession !== currentSession) return
    }
    authSessionGeneration += 1
    // Stop auto-refresh
    stopAutoRefresh()
    // Stop token refresh
    stopTokenRefresh()

    updateSessionToken(null)
    runMode.value = 'standard'
    refreshTokenValue.value = null
    tokenExpiresAt.value = null
    user.value = null
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)

    if (options?.preservePendingAuthSession) {
      pendingAuthSession.value = getPersistedPendingAuthSession()
      return
    }

    pendingAuthSession.value = null
    clearPendingAuthSessionStorage()
  }

  onScopeDispose(() => {
    disposed = true
    stopAutoRefresh()
    stopTokenRefresh()
  })

  // ==================== Return Store API ====================

  return {
    // State
    user,
    token,
    sessionEpoch: readonly(sessionEpoch),
    identityScopeKey,
    runMode: readonly(runMode),
    pendingAuthSession: readonly(pendingAuthSession),

    // Computed
    isAuthenticated,
    isAdmin,
    isSuperAdmin,
    can,
    canAccessAdminPage,
    canEditAdminField,
    editableAdminFields,
    adminLandingPath,
    isSimpleMode,
    hasPendingAuthSession,

    // Actions
    login,
    loginWithPasskey,
    login2FA,
    register,
    setToken,
    logout,
    checkAuth,
    refreshUser,
    setPendingAuthSession,
    clearPendingAuthSession
  }
})
