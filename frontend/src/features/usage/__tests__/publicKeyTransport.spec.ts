// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
beforeEach(() => {
  vi.resetModules(); localStorage.clear()
  localStorage.setItem('auth_token', 'management-session-fixture')
  vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
})
afterEach(() => { localStorage.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
describe('public key usage transport boundary', () => {
  it('sends only the explicit gateway key and omits JWT, cookies and session recovery', async () => {
    let finish!: (response: Response) => void
    const pending = new Promise<Response>(resolve => { finish = resolve })
    const fetchMock = vi.fn(() => pending); vi.stubGlobal('fetch', fetchMock)
    const { configureHttpClient } = await import('@/shared/http')
    const recoverError = vi.fn(), acceptResponse = vi.fn()
    configureHttpClient({ recoverError, acceptResponse, prepareRequest(config) { config.headers.Authorization = 'Bearer injected-management-fixture'; config.auth = { username: 'user', password: 'private' } } })
    const { getPublicKeyUsage } = await import('../api/publicKeyUsage')
    const result = getPublicKeyUsage('sk-public-fixture', { days: 30, timezone: 'UTC', start_date: '2026-10-25', end_date: '2026-10-25' })
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    const request = fetchMock.mock.calls[0]?.[0] as Request | undefined
    expect(request).toBeInstanceOf(Request)
    expect(request?.headers.get('x-api-key')).toBe('sk-public-fixture')
    expect(request?.headers.get('Authorization')).toBeNull(); expect(request?.headers.get('Cookie')).toBeNull()
    expect(request?.credentials).toBe('omit')
    expect(new URL(request!.url).pathname).toBe('/v1/usage')
    expect(request?.url).not.toContain('sk-public-fixture')
    localStorage.setItem('auth_token', 'different-management-session')
    finish(new Response(JSON.stringify({ mode: 'unrestricted', balance: 3.5 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    await expect(result).resolves.toEqual({ mode: 'unrestricted', balance: 3.5 })
    expect(recoverError).not.toHaveBeenCalled(); expect(acceptResponse).not.toHaveBeenCalled()
  })
  it('keeps the gateway error message without changing a logged-in session', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ error: { message: 'Invalid API key fixture' } }), { status: 401, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const { configureHttpClient } = await import('@/shared/http'), recoverError = vi.fn()
    configureHttpClient({ recoverError })
    const { getPublicKeyUsage } = await import('../api/publicKeyUsage')
    await expect(getPublicKeyUsage('sk-invalid-fixture', { days: 7, timezone: 'UTC' })).rejects.toThrow('Invalid API key fixture')
    expect(localStorage.getItem('auth_token')).toBe('management-session-fixture'); expect(recoverError).not.toHaveBeenCalled()
  })
})
