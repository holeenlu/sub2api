import axios from 'axios'
import type { AxiosError, AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { getLocale, i18n } from '@/i18n'
import { apiClient, configureHttpClient } from '@/shared/http'
import { currentAuthorizationScope, requireCurrentSession, responseFields } from '@/shared/http/client'
import type { SessionRequestConfig } from '@/shared/http/client'
import { authSessionStamp } from '@/shared/session'
import { refreshAuthTokens } from '@/shared/http/tokenRefresh'
import { requestAdminStepUp, StepUpCancelledError } from '@/features/identity'
import type { AdminComplianceStatus } from '@/features/identity/api/compliance'
import { ADMIN_UI_REQUEST_HEADER, USER_UI_REQUEST_HEADER, shouldMarkAdminUIRequest, shouldMarkUserUIRequest } from './requestMarkers'

function clientMessage(key: string, fallback: string): string {
  // Public settings can fail before the lazy locale bundle has loaded.
  const message = i18n.global.t(key)
  return message === key ? fallback : message
}

let permissionRefresh: { session: string; promise: Promise<void> } | null = null
async function refreshManagementPermissions(requestToken: string) {
  const session = authSessionStamp(requestToken)
  const isCurrent = () => session === authSessionStamp(localStorage.getItem('auth_token'))
  if (!isCurrent()) return
  if (permissionRefresh?.session !== session) {
    const promise = import('@/features/identity').then(async ({ useAuthStore }) => {
      if (!isCurrent()) return
      const auth = useAuthStore()
      await auth.refreshUser()
      const { default: router } = await import('@/app/router')
      if (!isCurrent()) return
      const path = router.currentRoute.value.path
      if (path.startsWith('/admin/') && !auth.canAccessAdminPage(path)) await router.replace(auth.adminLandingPath)
    }).catch(() => { /* Preserve the original failure and let the next request retry. */ }).finally(() => {
      if (permissionRefresh?.promise === promise) permissionRefresh = null
    })
    permissionRefresh = { session, promise }
  }
  await permissionRefresh.promise
}

async function acceptResponse(response: AxiosResponse): Promise<void> {
    if (/^\/(admin|vendor)\//.test(String(response.config.url || ''))) {
      const config = response.config as SessionRequestConfig
      const version = response.headers['x-admin-policy-version']
      const requestToken = String(config.headers?.Authorization || '').replace(/^Bearer\s+/i, '')
      let user: { role?: string; policy_version?: number } | null = null
      try { user = JSON.parse(localStorage.getItem('auth_user') || 'null') } catch { /* no stored scope */ }
      if (user?.role === 'admin' && version !== undefined && Number(version) !== user.policy_version) {
        await refreshManagementPermissions(requestToken)
        if (config._authorizationScope !== currentAuthorizationScope()) throw new axios.CanceledError('Administrator permissions updated')
      }
    }
}

interface ApplicationHttpEffects {
  requireAdminCompliance?(status: Partial<AdminComplianceStatus>): void
}

function complianceStatus(value: unknown): Partial<AdminComplianceStatus> {
  const fields = responseFields(value)
  const status: Partial<AdminComplianceStatus> = {}
  for (const key of ['version', 'document_path_zh', 'document_path_en', 'document_url_zh', 'document_url_en', 'ack_phrase_zh', 'ack_phrase_en'] as const) {
    if (typeof fields[key] === 'string') status[key] = fields[key]
  }
  return status
}

function requireRequestOwner(config: SessionRequestConfig): void {
  if (config?.signal?.aborted) throw new axios.CanceledError('Request owner cancelled')
  requireCurrentSession(config)
}

/** Cancel this request's wait without cancelling another owner's shared prompt. */
function waitForAdminStepUp(config: SessionRequestConfig): Promise<boolean> {
  requireRequestOwner(config)
  const signal = config.signal
  return new Promise<boolean>((resolve, reject) => {
    const cancel = () => {
      signal?.removeEventListener?.('abort', cancel)
      reject(new axios.CanceledError('Request owner cancelled'))
    }
    signal?.addEventListener?.('abort', cancel, { once: true })
    requestAdminStepUp().then(resolve, reject).finally(() => signal?.removeEventListener?.('abort', cancel))
  })
}

async function recoverError(error: AxiosError<unknown>, effects: ApplicationHttpEffects): Promise<AxiosResponse | undefined> {
  const originalRequest = error.config as SessionRequestConfig
  requireRequestOwner(originalRequest)
    // Handle common errors
    if (error.response) {
      const { status, data } = error.response
      const url = String(error.config?.url || '')

      // Validate `data` shape to avoid HTML error pages breaking our error handling.
      const apiData = responseFields(data)

      if (status === 403 && url.startsWith('/admin/') && ['ADMIN_PERMISSION_DENIED', 'PERMISSION_DENIED'].includes(String(apiData.reason || apiData.code))) {
        await refreshManagementPermissions(String(originalRequest.headers?.Authorization || '').replace(/^Bearer\s+/i, ''))
        requireCurrentSession(originalRequest)
      }

      if (status === 403 && url.startsWith('/admin/') && (apiData.reason === 'STEP_UP_REQUIRED' || apiData.code === 'STEP_UP_REQUIRED') && !originalRequest._stepUpRetried) {
        const originalToken = String(originalRequest.headers?.Authorization || '').replace(/^Bearer\s+/i, '')
        const stamp = authSessionStamp(originalToken)
        if (!stamp || stamp !== authSessionStamp(localStorage.getItem('auth_token'))) throw new StepUpCancelledError()
        originalRequest._stepUpRetried = true
        const verified = await waitForAdminStepUp(originalRequest)
        requireRequestOwner(originalRequest)
        if (!verified || stamp !== authSessionStamp(localStorage.getItem('auth_token'))) throw new StepUpCancelledError()
        return apiClient(originalRequest)
      }

      // Ops monitoring disabled: treat as feature-flagged 404, and proactively redirect away
      // from ops pages to avoid broken UI states.
      if (status === 404 && apiData.message === 'Ops monitoring is disabled') {
        const { useAdminSettingsStore } = await import('@/app/model/adminSettings')
        requireCurrentSession(originalRequest)
        useAdminSettingsStore().setOpsMonitoringEnabledLocal(false)

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
        effects.requireAdminCompliance?.(complianceStatus(apiData.metadata))

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
          originalRequest._retry = true

          try {
            const headers = originalRequest.headers as Record<string, unknown> | undefined
            const authHeader = headers?.Authorization ?? headers?.authorization
            const failedAccessToken =
              typeof authHeader === 'string' && authHeader.startsWith('Bearer ')
                ? authHeader.slice('Bearer '.length)
                : null
            const tokens = await refreshAuthTokens({ failedAccessToken })

            requireCurrentSession(originalRequest)

            // Retry the original request with the refreshed token
            if (originalRequest.headers) {
              originalRequest.headers.Authorization = `Bearer ${tokens.access_token}`
            }
            return apiClient(originalRequest)
          } catch (refreshError) {
            requireCurrentSession(originalRequest)
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
        // A terminal 401 deliberately clears this scope; do not turn it into cancellation.
        throw {
          status, code: apiData.code, reason: apiData.reason, error: apiData.error,
          message: apiData.message || apiData.detail || error.message, metadata: apiData.metadata
        }
      }

      const permissionMessage = apiData.reason === 'ADMIN_PERMISSION_DENIED'
        ? clientMessage('admin.rolePermissions.denied', 'Your role does not allow this action.')
        : apiData.reason === 'ADMIN_POLICY_CONFLICT'
          ? clientMessage('admin.rolePermissions.conflict', 'Permissions changed. Reload before saving.')
          : undefined

      // Domain-specific messages remain owned by the application.
      if (permissionMessage) return Promise.reject({
        status,
        code: apiData.code,
        reason: apiData.reason,
        error: apiData.error,
        message: permissionMessage || apiData.message || apiData.detail || error.message,
        metadata: apiData.metadata,
      })
    }

    return undefined
}

function prepareRequest(config: InternalAxiosRequestConfig): void {
  config.headers['Accept-Language'] = getLocale()
  if ((config as SessionRequestConfig).sessionPolicy === 'anonymous') return
  const url = String(config.url || '')
  if (shouldMarkAdminUIRequest(url)) config.headers[ADMIN_UI_REQUEST_HEADER] = '1'
  if (shouldMarkUserUIRequest(url)) config.headers[USER_UI_REQUEST_HEADER] = '1'
}

export function installApplicationHttp(effects: ApplicationHttpEffects = {}): () => void {
  return configureHttpClient({ prepareRequest, acceptResponse, recoverError: error => recoverError(error, effects), message: clientMessage })
}
