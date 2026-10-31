import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'

vi.mock('axios', () => ({
  default: {
    post: vi.fn()
  }
}))

const mockedPost = vi.mocked(axios.post)

function accessToken(label: string, session = 'session-a', userID = 7): string {
  return `header.${btoa(JSON.stringify({ user_id: userID, role: 'user', sid: session, token_version: 0, label }))}.signature`
}

function seedSession(overrides: Partial<Record<string, string>> = {}): void {
  localStorage.setItem('auth_token', overrides.auth_token || accessToken('old-access'))
  localStorage.setItem('refresh_token', overrides.refresh_token || 'old-refresh')
  localStorage.setItem('token_expires_at', overrides.token_expires_at || String(Date.now() - 1))
  localStorage.setItem('auth_user', JSON.stringify({ id: 7, email: 'admin@example.com' }))
}

function refreshedResponse() {
  return {
    data: {
      code: 0,
      message: 'ok',
      data: {
        access_token: accessToken('new-access'),
        refresh_token: 'new-refresh',
        expires_in: 3600,
        token_type: 'Bearer'
      }
    }
  }
}

describe('refreshAuthTokens', () => {
  beforeEach(() => {
    localStorage.clear()
    mockedPost.mockReset()
    vi.resetModules()
    Object.defineProperty(navigator, 'locks', {
      configurable: true,
      value: undefined
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  it('shares one refresh request between concurrent callers in the same document', async () => {
    seedSession()
    let resolveRequest!: (value: ReturnType<typeof refreshedResponse>) => void
    mockedPost.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveRequest = resolve
      })
    )
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    const first = refreshAuthTokens({ failedAccessToken: accessToken('old-access') })
    const second = refreshAuthTokens({ failedAccessToken: accessToken('old-access') })

    expect(mockedPost).toHaveBeenCalledTimes(1)
    resolveRequest(refreshedResponse())

    await expect(first).resolves.toMatchObject({ access_token: accessToken('new-access') })
    await expect(second).resolves.toMatchObject({ refresh_token: 'new-refresh' })
    expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
  })

  it('adopts tokens refreshed by another tab after acquiring the Web Lock', async () => {
    seedSession()
    const request = vi.fn(async (_name: string, callback: () => Promise<unknown>) => {
      localStorage.setItem('auth_token', accessToken('peer-access'))
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      localStorage.setItem('refresh_token', 'peer-refresh')
      return callback()
    })
    Object.defineProperty(navigator, 'locks', {
      configurable: true,
      value: { request }
    })
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    const result = await refreshAuthTokens({ failedAccessToken: accessToken('old-access') })

    expect(request).toHaveBeenCalledTimes(1)
    expect(mockedPost).not.toHaveBeenCalled()
    expect(result).toMatchObject({
      access_token: accessToken('peer-access'),
      refresh_token: 'peer-refresh'
    })
  })

  it('does not mistake boundary timer jitter for a completed peer refresh', async () => {
    vi.useFakeTimers()
    seedSession({ token_expires_at: String(Date.now() + 120_001) })
    mockedPost.mockResolvedValueOnce(refreshedResponse())
    const request = vi.fn(async (_name: string, callback: () => Promise<unknown>) => callback())
    Object.defineProperty(navigator, 'locks', {
      configurable: true,
      value: { request }
    })
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    await expect(refreshAuthTokens()).resolves.toMatchObject({ access_token: accessToken('new-access') })

    expect(request).toHaveBeenCalledTimes(1)
    expect(mockedPost).toHaveBeenCalledTimes(1)
  })

  it('recovers when a peer publishes the rotated token just after this request fails', async () => {
    seedSession()
    mockedPost.mockRejectedValueOnce(new Error('refresh token already used'))
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    window.setTimeout(() => {
      localStorage.setItem('auth_token', accessToken('peer-access'))
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      localStorage.setItem('refresh_token', 'peer-refresh')
    }, 10)

    await expect(
      refreshAuthTokens({ failedAccessToken: accessToken('old-access') })
    ).resolves.toMatchObject({
      access_token: accessToken('peer-access'),
      refresh_token: 'peer-refresh'
    })
  })

  it('waits for a slow peer after losing a refresh-token race without Web Locks', async () => {
    vi.useFakeTimers()
    seedSession()
    let resolveWinningRequest!: (value: ReturnType<typeof refreshedResponse>) => void
    mockedPost.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveWinningRequest = resolve
      })
    )
    const firstTab = await import('@/shared/http/tokenRefresh')
    vi.resetModules()
    const secondTab = await import('@/shared/http/tokenRefresh')

    const winner = firstTab.refreshAuthTokens({ failedAccessToken: accessToken('old-access') })
    mockedPost.mockRejectedValueOnce({ response: { status: 401 } })
    const loser = secondTab.refreshAuthTokens({ failedAccessToken: accessToken('old-access') })

    window.setTimeout(() => resolveWinningRequest(refreshedResponse()), 1_500)
    await vi.advanceTimersByTimeAsync(1_600)

    await expect(winner).resolves.toMatchObject({ access_token: accessToken('new-access') })
    await expect(loser).resolves.toMatchObject({ refresh_token: 'new-refresh' })
    expect(mockedPost).toHaveBeenCalledTimes(2)
    expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
  })

  it('does not adopt a token from a different signed-in user', async () => {
    vi.useFakeTimers()
    seedSession()
    mockedPost.mockRejectedValueOnce(new Error('refresh token already used'))
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    window.setTimeout(() => {
      localStorage.setItem('auth_user', JSON.stringify({ id: 8, email: 'other@example.com' }))
      localStorage.setItem('auth_token', accessToken('other-access', 'session-b', 8))
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      localStorage.setItem('refresh_token', 'other-refresh')
    }, 10)

    const rejection = expect(
      refreshAuthTokens({ failedAccessToken: accessToken('old-access') })
    ).rejects.toThrow('refresh token already used')
    await vi.advanceTimersByTimeAsync(1_100)
    await rejection
  })

  it('does not restore a session that was logged out while refresh was in flight', async () => {
    vi.useFakeTimers()
    seedSession()
    let resolveRequest!: (value: ReturnType<typeof refreshedResponse>) => void
    mockedPost.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveRequest = resolve
      })
    )
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    const pending = refreshAuthTokens({ failedAccessToken: accessToken('old-access') })
    localStorage.clear()
    resolveRequest(refreshedResponse())

    const rejection = expect(pending).rejects.toThrow('Session changed during token refresh')
    await vi.advanceTimersByTimeAsync(1_100)
    await rejection
    expect(localStorage.getItem('auth_token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('does not share refresh work or adopt results across two logins of the same user', async () => {
    vi.useFakeTimers()
    seedSession()
    let finishOld!: (value: ReturnType<typeof refreshedResponse>) => void
    mockedPost.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')
    const old = refreshAuthTokens()
    const rejected = expect(old).rejects.toThrow('Session changed during token refresh')
    seedSession({ auth_token: accessToken('second-login', 'session-b'), refresh_token: 'second-refresh' })
    const currentResult = refreshedResponse()
    currentResult.data.data.access_token = accessToken('new-access', 'session-b')
    mockedPost.mockResolvedValueOnce(currentResult)
    await expect(refreshAuthTokens()).resolves.toMatchObject({ access_token: accessToken('new-access', 'session-b') })
    expect(mockedPost).toHaveBeenCalledTimes(2)
    finishOld(refreshedResponse())
    await vi.advanceTimersByTimeAsync(1_100)
    await rejected
    expect(localStorage.getItem('auth_token')).toBe(accessToken('new-access', 'session-b'))
  })

  it.each([
    { kind: 'network failure', error: new Error('Network Error'), waitMs: 1_100 },
    { kind: 'rate limit', error: { response: { status: 429 } }, waitMs: 31_100 },
    { kind: 'upstream failure', error: { response: { status: 503 } }, waitMs: 1_100 }
  ])('preserves stored credentials and the original $kind', async ({ error, waitMs }) => {
    vi.useFakeTimers()
    seedSession()
    mockedPost.mockRejectedValueOnce(error)
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    const rejection = expect(refreshAuthTokens()).rejects.toBe(error)
    await vi.advanceTimersByTimeAsync(waitMs)
    await rejection

    expect(localStorage.getItem('auth_token')).toBe(accessToken('old-access'))
    expect(localStorage.getItem('refresh_token')).toBe('old-refresh')
    expect(mockedPost).toHaveBeenCalledTimes(1)
  })

  it('clears failed single-flight work so a later request can retry', async () => {
    vi.useFakeTimers()
    seedSession()
    mockedPost.mockRejectedValueOnce(new Error('Network Error'))
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')
    const rejection = expect(refreshAuthTokens()).rejects.toThrow('Network Error')
    await vi.advanceTimersByTimeAsync(1_100)
    await rejection

    mockedPost.mockResolvedValueOnce(refreshedResponse())
    await expect(refreshAuthTokens()).resolves.toMatchObject({ refresh_token: 'new-refresh' })
    expect(mockedPost).toHaveBeenCalledTimes(2)
  })

  it('uses the existing refresh storage key and rejects before a request when it is absent', async () => {
    seedSession()
    localStorage.removeItem('refresh_token')
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    await expect(refreshAuthTokens()).rejects.toThrow('No refresh token available')
    expect(mockedPost).not.toHaveBeenCalled()
  })

  it('retains the same-origin supplier refresh endpoint and bounded request timeout', async () => {
    vi.stubEnv('VITE_VENDOR_PORTAL', 'true')
    vi.stubEnv('VITE_API_BASE_URL', 'https://platform.example/api/v1')
    seedSession()
    mockedPost.mockResolvedValueOnce(refreshedResponse())
    const { refreshAuthTokens } = await import('@/shared/http/tokenRefresh')

    await refreshAuthTokens()

    expect(mockedPost).toHaveBeenCalledWith('/api/v1/auth/refresh', { refresh_token: 'old-refresh' }, {
      headers: { 'Content-Type': 'application/json' },
      timeout: 30_000
    })
  })

})
