import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'

const { stepUp } = vi.hoisted(() => ({ stepUp: vi.fn() }))
vi.mock('@/features/identity', () => ({
  requestAdminStepUp: stepUp,
  StepUpCancelledError: class extends Error {
    constructor() { super('Step-up cancelled') }
  }
}))
vi.mock('@/i18n', () => ({
  getLocale: () => 'en',
  i18n: { global: { t: (key: string) => key } }
}))

function signIn(id = 7, sid = 'session-a', policy = 1, issuedAt = 1): void {
  localStorage.setItem('auth_user', JSON.stringify({ id, role: 'admin', policy_version: policy }))
  localStorage.setItem('auth_token', 'header.' + btoa(JSON.stringify({
    user_id: id, role: 'admin', sid, token_version: 1, iat: issuedAt
  })) + '.signature')
}

function success(config: InternalAxiosRequestConfig): AxiosResponse {
  return { status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: { id: 702 } } }
}

// Only the adapter is replaced: requests use the real shared client and, for
// step-up cases, the app's actual recovery policy. No request reaches a server.
describe.each(['account', 'group'] as const)('%s duplicate session ownership', (kind) => {
  let apiClient: typeof import('@/shared/http').apiClient
  let duplicate: (id: number) => Promise<unknown>
  const adapter = vi.fn<(config: InternalAxiosRequestConfig) => Promise<AxiosResponse>>()
  const storageKey = 'sub2api:admin:' + kind + '-duplicate:7:701'

  beforeEach(async () => {
    vi.resetModules()
    localStorage.clear()
    sessionStorage.clear()
    signIn()
    stepUp.mockReset()
    adapter.mockReset().mockImplementation(async config => success(config))
    ;({ apiClient } = await import('@/shared/http'))
    apiClient.defaults.adapter = adapter
    duplicate = kind === 'account'
      ? (await import('@/features/resources/accounts')).duplicate
      : (await import('@/features/access-policy/groups')).duplicate
  })

  afterEach(() => { vi.restoreAllMocks() })

  it.each(['actor', 'policy', 'login'] as const)(
    'fences a change of %s before Axios dispatch without rotating the uncertain key', async change => {
      const pending = duplicate(701)
      const rejected = expect(pending).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      const originalKey = sessionStorage.getItem(storageKey)
      expect(originalKey).toBeTruthy()
      if (change === 'actor') signIn(8, 'session-b')
      else if (change === 'policy') signIn(7, 'session-a', 2)
      else signIn(7, 'session-b')
      await rejected
      expect(adapter).not.toHaveBeenCalled()
      expect(sessionStorage.getItem(storageKey)).toBe(originalKey)

      signIn(7, 'session-c', 3)
      await duplicate(701)
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(adapter.mock.calls[0][0].headers.get('Idempotency-Key')).toBe(originalKey)
      expect(sessionStorage.getItem(storageKey)).toBeNull()
    }
  )

  it('allows same-session token rotation while binding the request to the same actor', async () => {
    const pending = duplicate(701)
    signIn(7, 'session-a', 1, 2)
    await expect(pending).resolves.toEqual({ id: 702 })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer ' + localStorage.getItem('auth_token'))
    expect(sessionStorage.length).toBe(0)
  })

  it.each(['actor', 'policy', 'login', 'cancel'] as const)(
    'does not replay after step-up is pending and %s changes', async change => {
      const { installApplicationHttp } = await import('@/app/http/install')
      installApplicationHttp()
      let finish!: (verified: boolean) => void
      stepUp.mockReturnValueOnce(new Promise<boolean>(resolve => { finish = resolve }))
      adapter.mockImplementationOnce(async config => {
        throw new AxiosError('Step-up required', 'ERR_BAD_REQUEST', config, undefined, {
          status: 403, statusText: 'Forbidden', config, headers: {},
          data: { code: 'STEP_UP_REQUIRED', message: 'Step-up required' }
        })
      })
      const pending = duplicate(701)
      const rejected = expect(pending).rejects.toBeInstanceOf(Error)
      await vi.waitFor(() => expect(stepUp).toHaveBeenCalledTimes(1))
      const originalKey = sessionStorage.getItem(storageKey)
      if (change === 'actor') signIn(8, 'session-b')
      else if (change === 'policy') signIn(7, 'session-a', 2)
      else if (change === 'login') signIn(7, 'session-b')
      finish(change !== 'cancel')
      await rejected
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem(storageKey)).toBe(originalKey)

      signIn(7, 'session-c', 3)
      await duplicate(701)
      expect(adapter).toHaveBeenCalledTimes(2)
      expect(adapter.mock.calls[1][0].headers.get('Idempotency-Key')).toBe(originalKey)
    }
  )

  it('preserves the exact key through an authorized step-up retry', async () => {
    const { installApplicationHttp } = await import('@/app/http/install')
    installApplicationHttp()
    stepUp.mockResolvedValueOnce(true)
    adapter.mockImplementationOnce(async config => {
      throw new AxiosError('Step-up required', 'ERR_BAD_REQUEST', config, undefined, {
        status: 403, statusText: 'Forbidden', config, headers: {},
        data: { code: 'STEP_UP_REQUIRED' }
      })
    })
    await expect(duplicate(701)).resolves.toEqual({ id: 702 })
    expect(adapter).toHaveBeenCalledTimes(2)
    expect(adapter.mock.calls[1][0].headers.get('Idempotency-Key')).toBe(
      adapter.mock.calls[0][0].headers.get('Idempotency-Key')
    )
    expect(sessionStorage.getItem(storageKey)).toBeNull()
  })
  it('does not rotate an uncertain operation when the profile catches up with its bearer', async () => {
    signIn(8, 'session-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 1 }))
    adapter.mockRejectedValueOnce(new Error('unknown response'))
    await expect(duplicate(701)).rejects.toBeDefined()
    const firstRequests = [...adapter.mock.calls]
    adapter.mockReset().mockImplementation(async config => success(config))

    // Both attempts authenticate as actor 8. A profile refresh cannot turn an
    // unconfirmed copy into a fresh intent with a different server key.
    signIn(8, 'session-b')
    await duplicate(701)
    const requests = [...firstRequests, ...adapter.mock.calls].map(([config]) => config)
    expect(new Set(requests.map(config => config.headers.get('Authorization'))).size).toBe(1)
    expect(new Set(requests.map(config => config.headers.get('Idempotency-Key'))).size).toBe(1)
  })

  it('binds the owner before operation-key storage is read', async () => {
    const read = Storage.prototype.getItem
    let changed = false
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, key) {
      if (this === sessionStorage && !changed) {
        changed = true
        signIn(8, 'session-b')
      }
      return read.call(this, key)
    })
    await expect(duplicate(701)).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(adapter).not.toHaveBeenCalled()
  })

})
