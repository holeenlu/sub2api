/**
 * Axios HTTP Client Configuration
 * Base client with interceptors for authentication, token refresh, and error handling
 */

import axios, { AxiosInstance, AxiosError, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import type { ApiResponse } from '@/types'
import { getLocale, i18n } from '@/i18n'
import {
  ADMIN_UI_REQUEST_HEADER,
  USER_UI_REQUEST_HEADER,
  shouldMarkAdminUIRequest,
  shouldMarkUserUIRequest,
} from './adminUIRequest'
import { refreshAuthTokens } from './tokenRefresh'
import { getAPIBaseURL } from './url'
import { requestAdminStepUp, adminSessionStamp, StepUpCancelledError } from '@/composables/useStepUp'
export { buildApiUrl, buildGatewayUrl } from './url'

// ==================== Axios Instance Configuration ====================

function clientMessage(key: string, fallback: string): string {
  // Public settings can fail before the lazy locale bundle has loaded.
  const message = i18n.global.t(key)
  return message === key ? fallback : message
}

export const apiClient: AxiosInstance = axios.create({
  baseURL: getAPIBaseURL(),
  withCredentials: true,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

function currentAuthorizationScope(): string {
  try {
    const user = JSON.parse(localStorage.getItem('auth_user') || 'null')
    return JSON.stringify([user?.id, user?.role, user?.policy_version])
  } catch { return '' }
}
let permissionRefresh: Promise<void> | null = null
async function refreshManagementPermissions(requestToken: string) {
  if (requestToken !== localStorage.getItem('auth_token')) return
  if (!permissionRefresh) {
    permissionRefresh = import('@/stores/auth').then(async ({ useAuthStore }) => {
      const auth = useAuthStore()
      if (requestToken !== localStorage.getItem('auth_token')) return
      await auth.refreshUser()
      const { default: router } = await import('@/router')
      const path = router.currentRoute.value.path
      if (path.startsWith('/admin/') && !auth.canAccessAdminPage(path)) await router.replace(auth.adminLandingPath)
    }).catch(() => { /* Preserve the original failure and let the next request retry. */ }).finally(() => { permissionRefresh = null })
  }
  await permissionRefresh
}

// ==================== Request Interceptor ====================

// Get user's timezone
const getUserTimezone = (): string => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    // Attach token from localStorage
    const token = localStorage.getItem('auth_token')
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }

    if (String(config.url || '').startsWith('/admin/')) {
      Object.assign(config, { _authorizationScope: currentAuthorizationScope() })
    }

    // Attach locale for backend translations
    if (config.headers) {
      config.headers['Accept-Language'] = getLocale()
    }

    // Attach timezone for all GET requests (backend may use it for default date ranges)
    if (config.method === 'get') {
      if (!config.params) {
        config.params = {}
      }
      config.params.timezone = getUserTimezone()
    }

    if (config.headers) {
      const requestURL = String(config.url || '')
      if (shouldMarkAdminUIRequest(requestURL)) {
        config.headers[ADMIN_UI_REQUEST_HEADER] = '1'
      }
      if (shouldMarkUserUIRequest(requestURL)) {
        config.headers[USER_UI_REQUEST_HEADER] = '1'
      }
    }

    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// ==================== Response Interceptor ====================

apiClient.interceptors.response.use(
  async (response: AxiosResponse) => {
    if (String(response.config.url || '').startsWith('/admin/')) {
      const config = response.config as InternalAxiosRequestConfig & { _authorizationScope?: string }
      if (config._authorizationScope !== undefined && config._authorizationScope !== currentAuthorizationScope()) throw new axios.CanceledError('Authorization scope changed')
      const version = response.headers['x-admin-policy-version']
      const requestToken = String(config.headers?.Authorization || '').replace(/^Bearer\s+/i, '')
      let user: { role?: string; policy_version?: number } | null = null
      try { user = JSON.parse(localStorage.getItem('auth_user') || 'null') } catch { /* no stored scope */ }
      if (user?.role === 'admin' && version !== undefined && Number(version) !== user.policy_version) {
        await refreshManagementPermissions(requestToken)
        if (config._authorizationScope !== currentAuthorizationScope()) throw new axios.CanceledError('Administrator permissions updated')
      }
    }
    // Unwrap standard API response format { code, message, data }
    const apiResponse = response.data as ApiResponse<unknown>
    if (apiResponse && typeof apiResponse === 'object' && 'code' in apiResponse) {
      if (apiResponse.code === 0) {
        // Success - return the data portion
        response.data = apiResponse.data
      } else {
        // API error
        const resp = apiResponse as unknown as Record<string, unknown>
        return Promise.reject({
          status: response.status,
          code: apiResponse.code,
          message: apiResponse.message || clientMessage('ui.unknownError', 'Unknown error'),
          reason: resp.reason,
          metadata: resp.metadata,
        })
      }
    }
    return response
  },
  async (error: AxiosError<ApiResponse<unknown>>) => {
    // Request cancellation: keep the original axios cancellation error so callers can ignore it.
    // Otherwise we'd misclassify it as a generic "network error".
    if (error.code === 'ERR_CANCELED' || axios.isCancel(error)) {
      return Promise.reject(error)
    }

    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean; _stepUpRetried?: boolean }

    // Handle common errors
    if (error.response) {
      const { status, data } = error.response
      const url = String(error.config?.url || '')

      // Validate `data` shape to avoid HTML error pages breaking our error handling.
      const apiData = (typeof data === 'object' && data !== null ? data : {}) as Record<string, any>

      if (status === 403 && url.startsWith('/admin/') && ['ADMIN_PERMISSION_DENIED', 'PERMISSION_DENIED'].includes(String(apiData.reason || apiData.code))) {
        await refreshManagementPermissions(String(originalRequest.headers?.Authorization || '').replace(/^Bearer\s+/i, ''))
      }

      if (status === 403 && url.startsWith('/admin/') && (apiData.reason === 'STEP_UP_REQUIRED' || apiData.code === 'STEP_UP_REQUIRED') && !originalRequest._stepUpRetried) {
        const originalToken = String(originalRequest.headers?.Authorization || '').replace(/^Bearer\s+/i, '')
        const stamp = adminSessionStamp(originalToken)
        if (!stamp || stamp !== adminSessionStamp(localStorage.getItem('auth_token'))) throw new StepUpCancelledError()
        originalRequest._stepUpRetried = true
        const verified = await requestAdminStepUp()
        if (!verified || stamp !== adminSessionStamp(localStorage.getItem('auth_token'))) throw new StepUpCancelledError()
        return apiClient(originalRequest)
      }

      // Ops monitoring disabled: treat as feature-flagged 404, and proactively redirect away
      // from ops pages to avoid broken UI states.
      if (status === 404 && apiData.message === 'Ops monitoring is disabled') {
        try {
          localStorage.setItem('ops_monitoring_enabled_cached', 'false')
        } catch {
          // ignore localStorage failures
        }
        try {
          window.dispatchEvent(new CustomEvent('ops-monitoring-disabled'))
        } catch {
          // ignore event failures
        }

        if (window.location.pathname.startsWith('/admin/ops')) {
          window.location.href = '/dashboard'
        }

        return Promise.reject({
          status,
          code: 'OPS_DISABLED',
          message: apiData.message || error.message,
          url
        })
      }

      if (status === 423 && apiData.code === 'ADMIN_COMPLIANCE_ACK_REQUIRED') {
        try {
          window.dispatchEvent(new CustomEvent('admin-compliance-required', {
            detail: apiData.metadata || {}
          }))
        } catch {
          // ignore event failures
        }

        return Promise.reject({
          status,
          code: apiData.code,
          message: apiData.message || error.message,
          metadata: apiData.metadata,
        })
      }

      // 401: Try to refresh the token if we have a refresh token
      // This handles TOKEN_EXPIRED, INVALID_TOKEN, TOKEN_REVOKED, etc.
      if (status === 401 && !originalRequest._retry) {
        const refreshToken = localStorage.getItem('refresh_token')
        const isAuthEndpoint =
          url.includes('/auth/login') || url.includes('/auth/register') || url.includes('/auth/refresh')

        // If we have a refresh token and this is not an auth endpoint, try to refresh
        if (refreshToken && !isAuthEndpoint) {
          const refreshSessionUser = localStorage.getItem('auth_user')
          originalRequest._retry = true

          try {
            const headers = originalRequest.headers as Record<string, unknown> | undefined
            const authHeader = headers?.Authorization ?? headers?.authorization
            const failedAccessToken =
              typeof authHeader === 'string' && authHeader.startsWith('Bearer ')
                ? authHeader.slice('Bearer '.length)
                : null
            const tokens = await refreshAuthTokens({ failedAccessToken })

            // Retry the original request with the refreshed token
            if (originalRequest.headers) {
              originalRequest.headers.Authorization = `Bearer ${tokens.access_token}`
            }
            return apiClient(originalRequest)
          } catch (refreshError) {
            // A stale request must never destroy a session that was logged out or replaced while
            // its refresh was in flight (for example, when another tab signs in as another user).
            const sessionChanged =
              localStorage.getItem('refresh_token') !== refreshToken ||
              localStorage.getItem('auth_user') !== refreshSessionUser
            if (sessionChanged) {
              return Promise.reject({
                status: 401,
                code: 'AUTH_SESSION_CHANGED',
                message: clientMessage('ui.authSessionChanged', 'Authentication session changed while refreshing.')
              })
            }

            if (axios.isAxiosError(refreshError)) {
              const refreshStatus = refreshError.response?.status ?? 0
              if (refreshStatus === 0 || refreshStatus === 429 || refreshStatus >= 500) {
                return Promise.reject({
                  status: refreshStatus,
                  code: 'TOKEN_REFRESH_UNAVAILABLE',
                  message: refreshError.response?.data?.message || refreshError.message
                })
              }
            }

            // Clear tokens and redirect to login
            localStorage.removeItem('auth_token')
            localStorage.removeItem('refresh_token')
            localStorage.removeItem('auth_user')
            localStorage.removeItem('token_expires_at')
            sessionStorage.setItem('auth_expired', '1')

            if (!window.location.pathname.includes('/login')) {
              window.location.href = '/login'
            }

            return Promise.reject({
              status: 401,
              code: 'TOKEN_REFRESH_FAILED',
              message: clientMessage('ui.sessionExpired', 'Session expired. Please log in again.')
            })
          }
        }

        // No refresh token or is auth endpoint - clear auth and redirect
        const hasToken = !!localStorage.getItem('auth_token')
        const headers = error.config?.headers as Record<string, unknown> | undefined
        const authHeader = headers?.Authorization ?? headers?.authorization
        const sentAuth =
          typeof authHeader === 'string'
            ? authHeader.trim() !== ''
            : Array.isArray(authHeader)
              ? authHeader.length > 0
              : !!authHeader

        localStorage.removeItem('auth_token')
        localStorage.removeItem('refresh_token')
        localStorage.removeItem('auth_user')
        localStorage.removeItem('token_expires_at')
        if ((hasToken || sentAuth) && !isAuthEndpoint) {
          sessionStorage.setItem('auth_expired', '1')
        }
        // Only redirect if not already on login page
        if (!window.location.pathname.includes('/login')) {
          window.location.href = '/login'
        }
      }

      const permissionMessage = apiData.reason === 'ADMIN_PERMISSION_DENIED'
        ? clientMessage('admin.rolePermissions.denied', 'Your role does not allow this action.')
        : apiData.reason === 'ADMIN_POLICY_CONFLICT'
          ? clientMessage('admin.rolePermissions.conflict', 'Permissions changed. Reload before saving.')
          : undefined

      // Return structured error
      return Promise.reject({
        status,
        code: apiData.code,
        reason: apiData.reason,
        error: apiData.error,
        message: permissionMessage || apiData.message || apiData.detail || error.message,
        metadata: apiData.metadata,
      })
    }

    // Network error
    return Promise.reject({
      status: 0,
      code: error.code || 'ERR_NETWORK',
      message: clientMessage('ui.networkError', 'Network error. Please check your connection.')
    })
  }
)

export default apiClient
