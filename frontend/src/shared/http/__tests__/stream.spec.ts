// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

beforeEach(() => {
  vi.resetModules()
  const values = new Map<string, string>()
  const storage: Storage = { get length() { return values.size },
    getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, String(value)) },
    removeItem: key => { values.delete(key) }, clear: () => values.clear(), key: index => Array.from(values.keys())[index] ?? null }
  vi.stubGlobal('sessionStorage', storage)
  localStorage.clear()
  localStorage.setItem('auth_token', 'synthetic-management-session')
})
afterEach(() => { localStorage.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('management event streams', () => {
  it('uses the shared request policy and leaves successful SSE readable', async () => {
    const network = vi.fn<(input: Request | string | URL) => Promise<Response>>(async () => new Response('data: {"status":"ok"}\n\n', { headers: { 'Content-Type': 'text/event-stream' } }))
    vi.stubGlobal('fetch', network)
    const { apiClient, configureHttpClient } = await import('../client')
    const { postEventStream } = await import('../stream')
    apiClient.defaults.baseURL = 'http://localhost/api/v1'
    configureHttpClient({ prepareRequest(config) { config.headers['X-Admin-UI-Request'] = '1' } })
    const response = await postEventStream('/admin/accounts/7/test', { model_id: 'fixture-model' }, new AbortController().signal)
    const request = network.mock.calls[0]?.[0]
    if (!(request instanceof Request)) throw new Error('Expected the native Fetch Request')
    expect(request?.headers.get('Authorization')).toBe('Bearer synthetic-management-session')
    expect(request?.headers.get('X-Admin-UI-Request')).toBe('1')
    expect(request?.headers.get('Accept')).toBe('text/event-stream')
    expect(response.headers.get('content-type')).toBe('text/event-stream')
    expect(await response.text()).toBe('data: {"status":"ok"}\n\n')
  })

  it('decodes a failed stream envelope before the shared step-up/error policy sees it', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 'STEP_UP_REQUIRED', message: 'Verify identity', metadata: { challenge: 'totp' } }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    const { apiClient, configureHttpClient } = await import('../client')
    const { postEventStream } = await import('../stream')
    apiClient.defaults.baseURL = 'http://localhost/api/v1'
    const recoverError = vi.fn<(error: import('axios').AxiosError<unknown>) => Promise<undefined>>(async () => undefined)
    configureHttpClient({ recoverError })
    await expect(postEventStream('/admin/accounts/7/test', {}, new AbortController().signal)).rejects.toMatchObject({ status: 403, code: 'STEP_UP_REQUIRED' })
    expect(recoverError).toHaveBeenCalledOnce()
    expect(recoverError.mock.calls[0]?.[0]).toMatchObject({ response: { data: { code: 'STEP_UP_REQUIRED', metadata: { challenge: 'totp' } } } })
  })

  it('does not open a stream after its owning view cancels', async () => {
    const network = vi.fn()
    vi.stubGlobal('fetch', network)
    const { apiClient, configureHttpClient } = await import('../client')
    const { postEventStream } = await import('../stream')
    apiClient.defaults.baseURL = 'http://localhost/api/v1'
    configureHttpClient({})
    const abort = new AbortController(); abort.abort()
    await expect(postEventStream('/admin/accounts/7/test', {}, abort.signal)).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(network).not.toHaveBeenCalled()
  })
})
