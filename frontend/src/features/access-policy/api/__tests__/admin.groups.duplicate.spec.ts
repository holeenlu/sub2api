import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { extractApiErrorCode, extractApiErrorMessage } from '@/shared/lib/apiError'

const { post } = vi.hoisted(() => ({
  post: vi.fn()
}))

vi.mock('@/shared/http', () => ({
  apiClient: { post }
}))

let duplicate: typeof import('@/features/access-policy/groups').duplicate

describe('admin group duplicate API', () => {
  beforeEach(async () => {
    vi.resetModules()
    localStorage.clear()
    sessionStorage.clear()
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 1 }))
    localStorage.setItem('auth_token', 'actor-7-session-a')
    post.mockReset()
    post.mockResolvedValue({ data: { id: 43, name: 'primary (Copy)', status: 'inactive' } })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue('11111111-1111-4111-8111-111111111111')
    ;({ duplicate } = await import('@/features/access-policy/groups'))
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends a stable idempotency key with the duplicate request', async () => {
    const group = await duplicate(42)

    expect(post).toHaveBeenCalledWith('/admin/groups/42/duplicate', undefined, expect.objectContaining({
      headers: {
        'Idempotency-Key': 'group-duplicate-7-42-11111111-1111-4111-8111-111111111111'
      }
    }))
    expect(group).toEqual({ id: 43, name: 'primary (Copy)', status: 'inactive' })
    expect(sessionStorage.length).toBe(0)
  })

  it('reuses the operation key after an ambiguous failed request', async () => {
    post.mockRejectedValueOnce(new Error('network timeout'))
    await expect(duplicate(99)).rejects.toThrow('network timeout')

    post.mockResolvedValueOnce({ data: { id: 100, name: 'retry (Copy)' } })
    await duplicate(99)

    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[1][2].headers).toEqual(post.mock.calls[0][2].headers)
    expect(sessionStorage.length).toBe(0)
  })

  it('reuses the operation key after a page reload', async () => {
    post.mockRejectedValueOnce(new Error('network timeout'))
    await expect(duplicate(77)).rejects.toThrow('network timeout')
    const firstHeaders = post.mock.calls[0][2].headers

    vi.resetModules()
    post.mockResolvedValueOnce({ data: { id: 78, name: 'reload (Copy)' } })
    const { duplicate: duplicateAfterReload } = await import('@/features/access-policy/groups')
    await duplicateAfterReload(77)

    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[1][2].headers).toEqual(firstHeaders)
    expect(sessionStorage.length).toBe(0)
  })

  it('does not reuse an operation key across administrators for the same group', async () => {
    post.mockRejectedValueOnce(new Error('first admin timeout'))
    await expect(duplicate(55)).rejects.toThrow('first admin timeout')
    const firstAdminHeaders = post.mock.calls[0][2].headers

    localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
    vi.mocked(globalThis.crypto.randomUUID).mockReturnValueOnce(
      '22222222-2222-4222-8222-222222222222'
    )
    post.mockResolvedValueOnce({ data: { id: 56, name: 'second admin copy' } })
    await duplicate(55)

    expect(post.mock.calls[1][2].headers).not.toEqual(firstAdminHeaders)
    expect(post.mock.calls[1][2].headers).toEqual({
      'Idempotency-Key': 'group-duplicate-8-55-22222222-2222-4222-8222-222222222222'
    })
    expect(sessionStorage.getItem('sub2api:admin:group-duplicate:7:55')).toBe(
      firstAdminHeaders['Idempotency-Key']
    )
    expect(sessionStorage.getItem('sub2api:admin:group-duplicate:8:55')).toBeNull()
  })

  it('does not send an untracked command when the current owner is unavailable', async () => {
    localStorage.setItem('auth_user', '{invalid json')
    await expect(duplicate(66)).rejects.toMatchObject({ code: 'DUPLICATE_SESSION_UNAVAILABLE' })
    expect(post).not.toHaveBeenCalled()
    expect(sessionStorage.length).toBe(0)
  })

  it('does not let a late acknowledgment erase a newer unconfirmed operation', async () => {
    vi.mocked(globalThis.crypto.randomUUID)
      .mockReturnValueOnce('11111111-1111-4111-8111-111111111111')
      .mockReturnValueOnce('22222222-2222-4222-8222-222222222222')
      .mockReturnValueOnce('33333333-3333-4333-8333-333333333333')
    let acknowledgeOld!: (value: { data: { id: number } }) => void
    const oldResponse = new Promise<{ data: { id: number } }>((resolve) => {
      acknowledgeOld = resolve
    })
    post.mockResolvedValueOnce({ data: { id: 882 } })
      .mockReturnValueOnce(oldResponse)
      .mockRejectedValueOnce(new Error('new operation timeout'))

    const first = duplicate(881)
    const late = duplicate(881)
    await first
    await expect(duplicate(881)).rejects.toThrow('new operation timeout')
    const pendingHeaders = post.mock.calls[2][2].headers
    expect(pendingHeaders).not.toEqual(post.mock.calls[0][2].headers)

    acknowledgeOld({ data: { id: 882 } })
    await late
    await duplicate(881)
    expect(post.mock.calls[3][2].headers).toEqual(pendingHeaders)
  })

  it.each([null, '{invalid json', '{}', '{"id":0}', '{"id":1.5}', '{"id":9007199254740992}'])(
    'does not issue an untracked copy for unavailable owner %s', async (user) => {
      if (user === null) localStorage.removeItem('auth_user')
      else localStorage.setItem('auth_user', user)
      await expect(duplicate(661)).rejects.toMatchObject({ code: 'DUPLICATE_SESSION_UNAVAILABLE' })
      expect(post).not.toHaveBeenCalled()
      expect(sessionStorage.length).toBe(0)
    }
  )

  it('does not issue a copy for a logged-out stored user', async () => {
    localStorage.removeItem('auth_token')
    await expect(duplicate(662)).rejects.toMatchObject({ code: 'DUPLICATE_SESSION_UNAVAILABLE' })
    expect(post).not.toHaveBeenCalled()
  })

  it.each([
    { code: 'ERR_CANCELED' },
    { status: 403, code: 'ADMIN_PERMISSION_DENIED' },
    { status: 409, code: 'IDEMPOTENCY_KEY_CONFLICT' },
    { status: 503, code: 'IDEMPOTENCY_STORE_UNAVAILABLE' }
  ])('retains the key without automatic replay after $code', async (error) => {
    post.mockRejectedValueOnce(error)
    await expect(duplicate(663)).rejects.toBe(error)
    expect(post).toHaveBeenCalledTimes(1)
    const headers = post.mock.calls[0][2].headers
    expect(sessionStorage.getItem('sub2api:admin:group-duplicate:7:663')).toBe(headers['Idempotency-Key'])
    await duplicate(663)
    expect(post.mock.calls[1][2].headers).toEqual(headers)
  })

  it.each(['actor', 'role', 'policy', 'login'] as const)(
    'rejects a late result after a change of %s without losing the original operation', async (change) => {
      let finish!: (value: { data: { id: number } }) => void
      post.mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
      const pending = duplicate(664)
      const rejected = expect(pending).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      const headers = post.mock.calls[0][2].headers
      if (change === 'actor') {
        localStorage.setItem('auth_user', JSON.stringify({ id: 8, role: 'admin', policy_version: 1 }))
        localStorage.setItem('auth_token', 'actor-8-session')
      } else if (change === 'role') {
        localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'user', policy_version: 1 }))
      } else if (change === 'policy') {
        localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 2 }))
      } else {
        localStorage.setItem('auth_token', 'actor-7-session-b')
      }
      finish({ data: { id: 665 } })
      await rejected
      expect(post).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem('sub2api:admin:group-duplicate:7:664')).toBe(headers['Idempotency-Key'])
      localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 3 }))
      localStorage.setItem('auth_token', 'actor-7-session-c')
      await duplicate(664)
      expect(post.mock.calls[1][2].headers).toEqual(headers)
    }
  )

  it('keeps recovery across reload and same-actor login/permission changes', async () => {
    post.mockRejectedValueOnce(new Error('unconfirmed'))
    await expect(duplicate(666)).rejects.toThrow('unconfirmed')
    const headers = post.mock.calls[0][2].headers
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 5 }))
    localStorage.setItem('auth_token', 'actor-7-renewed-login')
    vi.resetModules()
    const reloaded = await import('@/features/access-policy/groups')
    await reloaded.duplicate(666)
    expect(post.mock.calls[1][2].headers).toEqual(headers)
    expect(sessionStorage.length).toBe(0)
  })

  it('cannot clear a newer persisted key owned by another loaded instance', async () => {
    let finish!: (value: { data: { id: number } }) => void
    post.mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
    const pending = duplicate(667)
    const newerKey = 'group-duplicate-7-667-newer-unconfirmed-intent'
    sessionStorage.setItem('sub2api:admin:group-duplicate:7:667', newerKey)
    finish({ data: { id: 668 } })
    await pending
    expect(sessionStorage.getItem('sub2api:admin:group-duplicate:7:667')).toBe(newerKey)
    await duplicate(667)
    expect(post.mock.calls[1][2].headers['Idempotency-Key']).toBe(newerKey)
  })

  it('keeps different targets independent without changing endpoint or payload', async () => {
    post.mockRejectedValueOnce(new Error('source one timeout'))
    await expect(duplicate(669)).rejects.toThrow('source one timeout')
    await duplicate(670)
    await duplicate(669)
    expect(post.mock.calls.map(([url, body]) => [url, body])).toEqual([
      ['/admin/groups/669/duplicate', undefined],
      ['/admin/groups/670/duplicate', undefined],
      ['/admin/groups/669/duplicate', undefined]
    ])
    expect(post.mock.calls[1][2].headers).not.toEqual(post.mock.calls[0][2].headers)
    expect(post.mock.calls[2][2].headers).toEqual(post.mock.calls[0][2].headers)
  })

  it('retains in-memory retry ownership if sessionStorage is unavailable', async () => {
    const nativeGet = Storage.prototype.getItem
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (key) {
      if (this === sessionStorage) throw new Error('storage blocked')
      return nativeGet.call(this, key)
    })
    const nativeSet = Storage.prototype.setItem
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (key, value) {
      if (this === sessionStorage) throw new Error('storage blocked')
      nativeSet.call(this, key, value)
    })
    post.mockRejectedValueOnce(new Error('timeout'))
    await expect(duplicate(671)).rejects.toThrow('timeout')
    await duplicate(671)
    expect(post.mock.calls[1][2].headers).toEqual(post.mock.calls[0][2].headers)
  })

  it('uses the existing localized fallback for a local validation failure', async () => {
    localStorage.removeItem('auth_token')
    const failure: unknown = await duplicate(675).catch((error: unknown) => error)
    expect(extractApiErrorCode(failure)).toBe('DUPLICATE_SESSION_UNAVAILABLE')
    expect(extractApiErrorMessage(failure, '复制失败')).toBe('复制失败')
    expect((failure as { message?: string }).message || '复制失败').toBe('复制失败')
    expect(post).not.toHaveBeenCalled()
  })

})
