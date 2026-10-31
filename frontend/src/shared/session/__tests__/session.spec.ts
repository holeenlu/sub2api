import { beforeEach, describe, expect, it } from 'vitest'
import {
  AUTH_TOKEN_KEY,
  AUTH_USER_KEY,
  REFRESH_TOKEN_KEY,
  TOKEN_EXPIRES_AT_KEY,
  authSessionStamp,
  readAuthSessionSnapshot
} from '@/shared/session'

function accessToken(claims: Record<string, unknown>): string {
  return 'header.' + btoa(JSON.stringify(claims)) + '.signature'
}

describe('browser session identity', () => {
  const claims = { user_id: 7, role: 'user', sid: 'login-a', token_version: 0 }

  it('keeps a session stable across normal token rotation', () => {
    expect(authSessionStamp(accessToken({ ...claims, iat: 10 }))).toBe(
      authSessionStamp(accessToken({ ...claims, iat: 20 }))
    )
  })

  it.each([
    { change: 'same-user new login', next: { ...claims, sid: 'login-b' } },
    { change: 'another user', next: { ...claims, user_id: 8 } },
    { change: 'changed role', next: { ...claims, role: 'admin' } },
    { change: 'revoked token generation', next: { ...claims, token_version: 1 } }
  ])('invalidates work for $change', ({ next }) => {
    expect(authSessionStamp(accessToken(next))).not.toBe(authSessionStamp(accessToken(claims)))
  })

  it('keeps the established exact-token fallback for tokens without a session id', () => {
    const first = accessToken({ user_id: 7, role: 'user', iat: 10 })
    const second = accessToken({ user_id: 7, role: 'user', iat: 20 })

    expect(authSessionStamp(first)).not.toBe(authSessionStamp(second))
    expect(authSessionStamp('opaque-token')).toBe('opaque-token')
    expect(authSessionStamp(accessToken({ sid: null }))).toContain(accessToken({ sid: null }))
    expect(authSessionStamp(null)).toBe('')
  })
})

describe('browser session storage snapshot', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('reads the existing storage contract without caching or exposing the full user record', () => {
    const token = accessToken({ user_id: 7, role: 'user', sid: 'login-a' })
    localStorage.setItem('auth_token', token)
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, email: 'private@example.test' }))
    localStorage.setItem('refresh_token', 'refresh-a')
    localStorage.setItem('token_expires_at', '2000000000000')

    const snapshot = readAuthSessionSnapshot()
    expect(snapshot).toEqual({
      accessToken: token,
      refreshToken: 'refresh-a',
      expiresAt: 2000000000000,
      userID: 7,
      session: authSessionStamp(token)
    })
    localStorage.clear()
    expect(readAuthSessionSnapshot()).toEqual({
      accessToken: null,
      refreshToken: null,
      expiresAt: null,
      userID: null,
      session: ''
    })
    expect(snapshot.refreshToken).toBe('refresh-a')
    expect([AUTH_TOKEN_KEY, AUTH_USER_KEY, REFRESH_TOKEN_KEY, TOKEN_EXPIRES_AT_KEY]).toEqual([
      'auth_token', 'auth_user', 'refresh_token', 'token_expires_at'
    ])
  })

  it.each(['not-json', 'null', '[]', '{}', '{"id":0}', '{"id":"unknown"}', '{"id":true}', '{"id":[7]}', '{"id":7.5}', '{"id":9007199254740992}'])(
    'does not invent identity from invalid stored user data: %s',
    (rawUser) => {
      localStorage.setItem(AUTH_USER_KEY, rawUser)
      expect(readAuthSessionSnapshot().userID).toBeNull()
      expect(localStorage.getItem(AUTH_USER_KEY)).toBe(rawUser)
    }
  )

  it('rejects a stale profile until it agrees with the current bearer', () => {
    const token = accessToken({ user_id: 8, role: 'admin', sid: 'login-b' })
    localStorage.setItem(AUTH_TOKEN_KEY, token)
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify({ id: 7 }))
    expect(readAuthSessionSnapshot().userID).toBeNull()
    expect(localStorage.getItem(AUTH_TOKEN_KEY)).toBe(token)
    expect(localStorage.getItem(AUTH_USER_KEY)).toBe(JSON.stringify({ id: 7 }))
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify({ id: 8 }))
    expect(readAuthSessionSnapshot().userID).toBe(8)
  })

  it.each([null, 0, -1, 7.5, '7', true, 9007199254740992])('does not correlate an invalid explicit bearer actor: %s', userID => {
    localStorage.setItem(AUTH_TOKEN_KEY, accessToken({ user_id: userID }))
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify({ id: 7 }))
    expect(readAuthSessionSnapshot().userID).toBeNull()
  })

  it('does not invent a profile from a token or reinterpret opaque token identity', () => {
    localStorage.setItem(AUTH_TOKEN_KEY, accessToken({ user_id: 7 }))
    expect(readAuthSessionSnapshot().userID).toBeNull()
    localStorage.setItem(AUTH_TOKEN_KEY, 'opaque-token')
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify({ id: 7 }))
    expect(readAuthSessionSnapshot().userID).toBe(7)
    expect(readAuthSessionSnapshot().session).toBe('opaque-token')
  })

  it.each(['', 'not-a-number', 'Infinity', '-1'])(
    'marks invalid expiry as unavailable: %s',
    (expiresAt) => {
      localStorage.setItem(TOKEN_EXPIRES_AT_KEY, expiresAt)
      expect(readAuthSessionSnapshot().expiresAt).toBeNull()
    }
  )
})
