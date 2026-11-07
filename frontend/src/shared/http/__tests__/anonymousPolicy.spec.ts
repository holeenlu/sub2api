// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { HttpRequestConfig } from '../client'

beforeEach(() => {
  vi.resetModules()
  const values = new Map<string, string>()
  const memory: Storage = {
    get length() { return values.size },
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, String(value)) },
    removeItem: key => { values.delete(key) },
    clear: () => values.clear(),
    key: index => Array.from(values.keys())[index] ?? null
  }
  vi.stubGlobal('sessionStorage', memory)
  localStorage.clear()
  sessionStorage.clear()
  localStorage.setItem('auth_token', 'private-session-token')
  localStorage.setItem('refresh_token', 'private-refresh-token')
})
afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  sessionStorage.clear()
  vi.unstubAllGlobals()
})

describe('explicit anonymous request policy', () => {
  it('omits all session credentials and remains independent of a concurrent login change', async () => {
    let finish: (response: Response) => void = () => { throw new Error('Response not initialized') }
    const pending = new Promise<Response>(resolve => { finish = resolve })
    const fetchMock = vi.fn<(input: Request | string | URL, init?: RequestInit) => Promise<Response>>(() => pending)
    vi.stubGlobal('fetch', fetchMock)
    const { apiClient, configureHttpClient } = await import('../client')
    apiClient.defaults.baseURL = 'http://localhost/api/v1'
    const recoverError = vi.fn()
    const acceptResponse = vi.fn()
    configureHttpClient({
      recoverError, acceptResponse,
      prepareRequest(config) {
        config.headers.Authorization = 'Bearer injected-private-token'
        config.headers['X-XSRF-TOKEN'] = 'private-xsrf-token'
        config.auth = { username: 'private-user', password: 'private-password' }
      }
    })
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous' }
    const result = apiClient.post('/payment/public/orders/resolve', { resume_token: 'synthetic-public-proof' }, config)
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    const request = fetchMock.mock.calls[0]?.[0]
    if (!(request instanceof Request)) throw new Error('Axios did not construct a Fetch Request')
    expect(request.credentials).toBe('omit')
    expect(request.headers.get('Authorization')).toBeNull()
    expect(request.headers.get('X-XSRF-TOKEN')).toBeNull()
    expect(request.headers.get('Cookie')).toBeNull()
    localStorage.setItem('auth_token', 'new-private-session-token')
    finish(new Response(JSON.stringify({ code: 0, data: { paid: false, status: 'pending' } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    await expect(result).resolves.toMatchObject({ data: { paid: false, status: 'pending' } })
    expect(acceptResponse).not.toHaveBeenCalled()
    expect(recoverError).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('new-private-session-token')
  })

  it('returns a public 401 without refreshing, clearing, or redirecting the signed-in session', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 'PUBLIC_PROOF_INVALID', message: 'Invalid public proof' }), { status: 401, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const { apiClient, configureHttpClient } = await import('../client')
    apiClient.defaults.baseURL = 'http://localhost/api/v1'
    const recoverError = vi.fn()
    configureHttpClient({ recoverError })
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous' }
    await expect(apiClient.post('/payment/public/orders/verify', { out_trade_no: 'synthetic-order' }, config)).rejects.toMatchObject({ status: 401, code: 'PUBLIC_PROOF_INVALID' })
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(recoverError).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('private-session-token')
    expect(localStorage.getItem('refresh_token')).toBe('private-refresh-token')
    expect(sessionStorage.getItem('auth_expired')).toBeNull()
  })

  it('does not add private timezone parameters to an anonymous external lookup', async () => {
    const fetchMock = vi.fn<(input: Request | string | URL) => Promise<Response>>(async () => new Response(JSON.stringify({ ip: '203.0.113.7', country_code: 'ZZ' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const { apiClient, configureHttpClient } = await import('../client')
    configureHttpClient({})
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous' }
    await apiClient.get('https://get.geojs.io/v1/ip/geo/203.0.113.7.json', config)
    const request = fetchMock.mock.calls[0]?.[0]
    if (!(request instanceof Request)) throw new Error('Expected a native Fetch Request')
    expect(request.url).toBe('https://get.geojs.io/v1/ip/geo/203.0.113.7.json')
    expect(request.credentials).toBe('omit')
    expect(request.headers.get('Authorization')).toBeNull()
  })

  it('keeps cancellation identifiable without executing session recovery', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const { apiClient, configureHttpClient } = await import('../client')
    const recoverError = vi.fn()
    configureHttpClient({ recoverError })
    const controller = new AbortController()
    controller.abort()
    const config: HttpRequestConfig = { sessionPolicy: 'anonymous', signal: controller.signal }
    await expect(apiClient.post('/payment/public/orders/verify', {}, config)).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(recoverError).not.toHaveBeenCalled()
  })
})
