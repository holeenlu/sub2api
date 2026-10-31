import axios from 'axios'
import type { AxiosError, AxiosResponse, AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'
import { authSessionStamp, AUTH_TOKEN_KEY, AUTH_USER_KEY } from '@/shared/session'
import { getAPIBaseURL } from './url'

export type HttpRequestConfig = AxiosRequestConfig<unknown> & { sessionPolicy?: 'anonymous' }

export type SessionRequestConfig = InternalAxiosRequestConfig & {
  sessionPolicy?: 'anonymous'
  _authorizationScope?: string
  _retry?: boolean
  _stepUpRetried?: boolean
}

/** App-owned policies are injected once before the router or settings make requests. */
export interface HttpLifecycle {
  prepareRequest?(config: InternalAxiosRequestConfig): void
  acceptResponse?(response: AxiosResponse): Promise<void>
  recoverError?(error: AxiosError<unknown>): Promise<AxiosResponse | undefined>
  message?(key: string, fallback: string): string
}

let lifecycle: HttpLifecycle = {}

export function configureHttpClient(configuration: HttpLifecycle): () => void {
  lifecycle = configuration
  return () => {
    // A failed/older host must not detach a more recently installed host.
    if (lifecycle === configuration) lifecycle = {}
  }
}

export function currentAuthorizationScope(): string {
  try {
    const user: unknown = JSON.parse(localStorage.getItem(AUTH_USER_KEY) || 'null')
    const fields = user && typeof user === 'object' ? user : {}
    return JSON.stringify([
      'id' in fields ? fields.id : undefined,
      'role' in fields ? fields.role : undefined,
      'policy_version' in fields ? fields.policy_version : undefined,
      authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
    ])
  } catch {
    return ''
  }
}

export function requireCurrentSession(config?: SessionRequestConfig): void {
  if (config?.sessionPolicy === 'anonymous') return
  if (config?._authorizationScope !== undefined && config._authorizationScope !== currentAuthorizationScope()) {
    throw new axios.CanceledError('Authorization scope changed')
  }
}

export function responseFields(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? value as Record<string, unknown> : {}
}

function message(key: string, fallback: string): string {
  return lifecycle.message?.(key, fallback) ?? fallback
}

function timezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

export const apiClient = axios.create({
  baseURL: getAPIBaseURL(),
  withCredentials: true,
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' }
})

apiClient.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const scoped = config as SessionRequestConfig
  if (scoped.sessionPolicy !== 'anonymous') {
    requireCurrentSession(scoped)
    scoped._authorizationScope ??= currentAuthorizationScope()
    const token = localStorage.getItem(AUTH_TOKEN_KEY)
    if (token) config.headers.Authorization = 'Bearer ' + token
  }
  // Anonymous requests carry only query parameters explicitly chosen by their caller.
  if (config.method === 'get' && scoped.sessionPolicy !== 'anonymous') {
    config.params ??= {}
    config.params.timezone ??= timezone()
  }
  lifecycle.prepareRequest?.(config)
  if (scoped.sessionPolicy === 'anonymous') {
    // XHR cannot omit same-origin cookies. The existing Axios fetch adapter can.
    config.adapter = 'fetch'
    config.withCredentials = false
    config.withXSRFToken = false
    delete config.auth
    delete scoped._authorizationScope
    config.headers.delete('Authorization')
    config.headers.delete('Proxy-Authorization')
    config.headers.delete('X-XSRF-TOKEN')
  }
  return config
})

apiClient.interceptors.response.use(async (response: AxiosResponse) => {
  requireCurrentSession(response.config as SessionRequestConfig)
  if ((response.config as SessionRequestConfig).sessionPolicy !== 'anonymous') {
    await lifecycle.acceptResponse?.(response)
  }
  requireCurrentSession(response.config as SessionRequestConfig)
  const envelope = responseFields(response.data)
  if ('code' in envelope) {
    if (envelope.code !== 0) {
      throw {
        status: response.status,
        code: envelope.code,
        message: envelope.message || message('ui.unknownError', 'Unknown error'),
        reason: envelope.reason,
        metadata: envelope.metadata
      }
    }
    response.data = envelope.data
  }
  return response
}, async (error: AxiosError<unknown>) => {
  if (error.code === 'ERR_CANCELED' || axios.isCancel(error)) throw error
  requireCurrentSession(error.config as SessionRequestConfig | undefined)
  const recovered = (error.config as SessionRequestConfig | undefined)?.sessionPolicy === 'anonymous'
    ? undefined : await lifecycle.recoverError?.(error)
  if (recovered) return recovered
  requireCurrentSession(error.config as SessionRequestConfig | undefined)
  if (error.response) {
    const data = responseFields(error.response.data)
    throw {
      status: error.response.status,
      code: data.code,
      reason: data.reason,
      error: data.error,
      message: data.message || data.detail || error.message,
      metadata: data.metadata
    }
  }
  throw {
    status: 0,
    code: error.code || 'ERR_NETWORK',
    message: message('ui.networkError', 'Network error. Please check your connection.')
  }
})

export default apiClient
