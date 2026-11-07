import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, patch, remove, credentialGet, credentialCreate } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  remove: vi.fn(),
  credentialGet: vi.fn(),
  credentialCreate: vi.fn()
}))

vi.mock('@/shared/http', () => ({
  apiClient: {
    get,
    post,
    patch,
    delete: remove
  }
}))

import { passkeyAPI } from '@/features/identity/api/passkey'

class FakePublicKeyCredential {
  id = 'credential-id'
  rawId = Uint8Array.from([1, 2, 3]).buffer
  type = 'public-key'
  authenticatorAttachment = 'platform'
  response: Record<string, unknown> = {
    authenticatorData: Uint8Array.from([4, 5]).buffer,
    clientDataJSON: Uint8Array.from([6, 7]).buffer,
    signature: Uint8Array.from([8, 9]).buffer,
    userHandle: Uint8Array.from([10, 11]).buffer
  }

  getClientExtensionResults(): AuthenticationExtensionsClientOutputs {
    return {}
  }
}

class FakeRegistrationCredential extends FakePublicKeyCredential {
  constructor() {
    super()
    this.response = {
      attestationObject: Uint8Array.from([12, 13]).buffer,
      clientDataJSON: Uint8Array.from([6, 7]).buffer,
      getTransports: () => ['internal']
    }
  }
}

describe('passkey api', () => {
  function deferred<T>() {
    let resolve!: (value: T) => void
    const promise = new Promise<T>(yes => { resolve = yes })
    return { promise, resolve }
  }
  // Synthetic fields mirror the backend's go-webauthn registration contract.
  const registrationOptions = {
    challenge: 'AQID',
    rp: { id: 'sub2api.example.com', name: 'Sub2API' },
    user: { id: 'AQID', name: 'user@example.com', displayName: 'user' },
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }, { type: 'public-key', alg: -257 }]
  }
  const beginResponse = { data: { session_token: 'synthetic-ceremony', options: { publicKey: registrationOptions } } }
  beforeEach(() => {
    localStorage.clear()
    get.mockReset()
    post.mockReset()
    patch.mockReset()
    remove.mockReset()
    credentialGet.mockReset()
    credentialCreate.mockReset()

    vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential)
    Object.defineProperty(window, 'PublicKeyCredential', {
      configurable: true,
      value: FakePublicKeyCredential
    })
    Object.defineProperty(navigator, 'credentials', {
      configurable: true,
      value: { get: credentialGet, create: credentialCreate }
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('converts assertion options and response bytes to WebAuthn JSON', async () => {
    post
      .mockResolvedValueOnce({
        data: {
          session_token: 'one-time-session',
          options: {
            publicKey: {
              challenge: 'AQID',
              rpId: 'sub2api.example.com',
              userVerification: 'required'
            }
          }
        }
      })
      .mockResolvedValueOnce({
        data: {
          access_token: 'access',
          token_type: 'Bearer',
          user: { id: 1 }
        }
      })
    credentialGet.mockResolvedValue(new FakePublicKeyCredential())

    await passkeyAPI.login()

    expect(post).toHaveBeenNthCalledWith(1, '/auth/passkey/login/begin')
    const request = credentialGet.mock.calls[0][0] as CredentialRequestOptions
    if (!request.publicKey || !(request.publicKey.challenge instanceof ArrayBuffer)) throw new Error('Expected native challenge buffer')
    expect(Array.from(new Uint8Array(request.publicKey.challenge))).toEqual([1, 2, 3])
    expect(request.publicKey.userVerification).toBe('required')

    expect(post).toHaveBeenNthCalledWith(2, '/auth/passkey/login/finish', {
      session_token: 'one-time-session',
      credential: {
        id: 'credential-id',
        rawId: 'AQID',
        type: 'public-key',
        authenticatorAttachment: 'platform',
        clientExtensionResults: {},
        response: {
          authenticatorData: 'BAU',
          clientDataJSON: 'Bgc',
          signature: 'CAk',
          userHandle: 'Cgs'
        }
      }
    })
  })

  it('sends Tencent captcha proof only with the passkey begin request', async () => {
    post
      .mockResolvedValueOnce({
        data: {
          session_token: 'one-time-session',
          options: {
            publicKey: {
              challenge: 'AQID',
              rpId: 'sub2api.example.com',
              userVerification: 'required'
            }
          }
        }
      })
      .mockResolvedValueOnce({ data: { access_token: 'access', token_type: 'Bearer', user: { id: 1 } } })
    credentialGet.mockResolvedValue(new FakePublicKeyCredential())

    await passkeyAPI.login({
      tencent_captcha_ticket: 'ticket-value',
      tencent_captcha_randstr: '@rand-value'
    })

    expect(post).toHaveBeenNthCalledWith(1, '/auth/passkey/login/begin', {
      tencent_captcha_ticket: 'ticket-value',
      tencent_captcha_randstr: '@rand-value'
    })
    expect(post.mock.calls[1][1]).not.toEqual(expect.objectContaining({
      tencent_captcha_ticket: expect.anything(),
      tencent_captcha_randstr: expect.anything()
    }))
  })

  it('sends the account password when beginning registration', async () => {
    post
      .mockResolvedValueOnce({
        data: {
          session_token: 'register-session',
          options: {
            publicKey: {
              ...registrationOptions,
              user: { id: 'BAU', name: 'user@example.com', displayName: 'user' }
            }
          }
        }
      })
      .mockResolvedValueOnce({
        data: { id: 3, name: 'Laptop', created_at: '2026-07-28T00:00:00Z', backup: false }
      })
    credentialCreate.mockResolvedValue(new FakeRegistrationCredential())

    await passkeyAPI.register('Laptop', 'hunter2')

    expect(post).toHaveBeenNthCalledWith(1, '/user/passkeys/register/begin', {
      password: 'hunter2'
    })
    expect(credentialCreate).toHaveBeenCalledWith({
      publicKey: {
        ...registrationOptions,
        challenge: Uint8Array.from([1, 2, 3]).buffer,
        user: { id: Uint8Array.from([4, 5]).buffer, name: 'user@example.com', displayName: 'user' }
      }
    })
    expect(post).toHaveBeenNthCalledWith(2, '/user/passkeys/register/finish', {
      session_token: 'register-session',
      name: 'Laptop',
      credential: expect.objectContaining({
        response: {
          attestationObject: 'DA0',
          clientDataJSON: 'Bgc',
          transports: ['internal']
        }
      })
    })
  })

  it.each(['login', 'register'])('rejects invalid %s options before opening the browser or finishing', async operation => {
    post.mockResolvedValueOnce({ data: { session_token: 'session', options: { publicKey: { ...registrationOptions, challenge: 123 } } } })
    await expect(operation === 'login' ? passkeyAPI.login() : passkeyAPI.register('Laptop', 'password')).rejects.toThrow('Invalid Passkey options: challenge')
    expect(credentialGet).not.toHaveBeenCalled()
    expect(credentialCreate).not.toHaveBeenCalled()
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('retains a null assertion userHandle in the finish payload', async () => {
    const credential = new FakePublicKeyCredential()
    credential.response.userHandle = null
    credentialGet.mockResolvedValueOnce(credential)
    post.mockResolvedValueOnce(beginResponse).mockResolvedValueOnce({ data: { access_token: 'access' } })
    await passkeyAPI.login()
    expect(post).toHaveBeenLastCalledWith('/auth/passkey/login/finish', {
      session_token: 'synthetic-ceremony',
      credential: expect.objectContaining({ response: { authenticatorData: 'BAU', clientDataJSON: 'Bgc', signature: 'CAk', userHandle: null } })
    })
  })

  it('keeps registration compatible when getTransports is unavailable', async () => {
    const credential = new FakeRegistrationCredential()
    delete credential.response.getTransports
    credentialCreate.mockResolvedValueOnce(credential)
    post.mockResolvedValueOnce(beginResponse).mockResolvedValueOnce({ data: { id: 10 } })
    await passkeyAPI.register('Laptop', 'password')
    expect(post).toHaveBeenLastCalledWith('/user/passkeys/register/finish', {
      session_token: 'synthetic-ceremony', name: 'Laptop',
      credential: expect.objectContaining({ response: { attestationObject: 'DA0', clientDataJSON: 'Bgc', transports: [] } })
    })
  })

  it.each(['login', 'register'])('preserves browser cancellation for %s without a finish request', async operation => {
    const cancelled = new DOMException('Cancelled', 'NotAllowedError')
    post.mockResolvedValueOnce(beginResponse)
    credentialGet.mockRejectedValueOnce(cancelled)
    credentialCreate.mockRejectedValueOnce(cancelled)
    await expect(operation === 'login' ? passkeyAPI.login() : passkeyAPI.register('Laptop', 'password')).rejects.toBe(cancelled)
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('sends the account password when revoking a credential', async () => {
    remove.mockResolvedValue({ data: null })

    await passkeyAPI.remove(12, 'hunter2')

    expect(remove).toHaveBeenCalledWith('/user/passkeys/12', {
      data: { password: 'hunter2' }
    })
  })

  it.each(['begin', 'browser', 'finish'])('discards registration after abort at the %s boundary', async phase => {
    const pending = deferred<unknown>()
    const controller = new AbortController()
    post.mockResolvedValueOnce(beginResponse).mockResolvedValueOnce({ data: { id: 10 } })
    credentialCreate.mockResolvedValue(new FakeRegistrationCredential())
    if (phase === 'begin') post.mockReset().mockReturnValueOnce(pending.promise)
    else if (phase === 'browser') credentialCreate.mockReturnValueOnce(pending.promise)
    else post.mockReset().mockResolvedValueOnce(beginResponse).mockReturnValueOnce(pending.promise)
    const operation = passkeyAPI.register('fixture', 'synthetic-password', { signal: controller.signal })
    const settled = expect(operation).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    await vi.waitFor(() => expect(phase === 'browser' ? credentialCreate : post).toHaveBeenCalledTimes(phase === 'finish' ? 2 : 1))
    controller.abort()
    pending.resolve(phase === 'begin' ? beginResponse : phase === 'browser' ? new FakeRegistrationCredential() : { data: { id: 10 } })
    await settled
    expect(post).toHaveBeenCalledTimes(phase === 'finish' ? 2 : 1)
    if (phase === 'begin') expect(credentialCreate).not.toHaveBeenCalled()
    else expect(credentialCreate.mock.calls[0][0].signal).toBe(controller.signal)
    expect(post.mock.calls[0][2].signal).toBe(controller.signal)
  })

  it.each(['begin', 'finish'])('rejects login after session replacement during %s', async phase => {
    const pending = deferred<unknown>()
    const token = (sid: string) => 'header.' + btoa(JSON.stringify({ user_id: 7, role: 'user', sid })) + '.signature'
    localStorage.setItem('auth_token', token('first'))
    credentialGet.mockResolvedValue(new FakePublicKeyCredential())
    if (phase === 'begin') post.mockReturnValueOnce(pending.promise)
    else post.mockResolvedValueOnce(beginResponse).mockReturnValueOnce(pending.promise)
    const operation = passkeyAPI.login()
    const settled = expect(operation).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    await vi.waitFor(() => expect(post).toHaveBeenCalledTimes(phase === 'begin' ? 1 : 2))
    localStorage.setItem('auth_token', token('replacement'))
    pending.resolve(phase === 'begin' ? beginResponse : { data: { access_token: 'obsolete' } })
    await settled
    if (phase === 'begin') expect(credentialGet).not.toHaveBeenCalled()
    expect(post).toHaveBeenCalledTimes(phase === 'begin' ? 1 : 2)
  })

  it('forwards signal without changing list, rename, and revocation payloads', async () => {
    const options = { signal: new AbortController().signal }
    get.mockResolvedValueOnce({ data: [] })
    await passkeyAPI.list(options)
    await passkeyAPI.rename(10, 'Laptop', options)
    await passkeyAPI.remove(10, 'synthetic-password', options)
    expect(get).toHaveBeenCalledWith('/user/passkeys', options)
    expect(patch).toHaveBeenCalledWith('/user/passkeys/10', { name: 'Laptop' }, options)
    expect(remove).toHaveBeenCalledWith('/user/passkeys/10', { ...options, data: { password: 'synthetic-password' } })
  })

  it('does not start a registration which was already aborted', async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(passkeyAPI.register('fixture', 'synthetic-password', { signal: controller.signal })).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(post).not.toHaveBeenCalled()
    expect(credentialCreate).not.toHaveBeenCalled()
  })

  it.each([false, true])('WebAuthn registration preserves same-session rotation=%s and rejects a replacement session', async sameSession => {
    const token = (sid: string, jti: string) => 'header.' + btoa(JSON.stringify({ user_id: 7, role: 'user', sid, jti })) + '.signature'
    localStorage.setItem('auth_token', token('original', 'first'))
    post.mockResolvedValueOnce({ data: { session_token: 'ceremony', options: { publicKey: registrationOptions } } })
    post.mockResolvedValueOnce({ data: { id: 7, name: 'Security key' } })
    let finish!: (credential: FakeRegistrationCredential) => void
    credentialCreate.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const operation = passkeyAPI.register('Security key', 'password')
    const settled = sameSession ? expect(operation).resolves.toMatchObject({ id: 7 }) : expect(operation).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    await vi.waitFor(() => expect(credentialCreate).toHaveBeenCalledOnce())
    localStorage.setItem('auth_token', token(sameSession ? 'original' : 'replacement', 'second'))
    finish(new FakeRegistrationCredential())
    await settled
    expect(post).toHaveBeenCalledTimes(sameSession ? 2 : 1)
    localStorage.clear()
  })

})
