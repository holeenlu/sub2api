import { CanceledError } from 'axios'
import { authSessionStamp, AUTH_TOKEN_KEY } from '@/shared/session'
import { apiClient } from '@/shared/http'
import type { ActionCaptchaRequestProof, AuthResponse } from './types'
import { ceremonyOptionsFromJSON, creationOptionsFromJSON, requestOptionsFromJSON } from './passkeyOptions'

export interface PasskeyCredentialSummary {
  id: number
  name: string
  created_at: string
  last_used_at?: string
  backup: boolean
}

interface PasskeyRequestOptions {
  signal?: AbortSignal
}

function ceremonyGuard(options?: PasskeyRequestOptions): () => void {
  const session = authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))
  return () => {
    if (options?.signal?.aborted || session !== authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY))) {
      throw new CanceledError('Passkey ceremony is no longer current')
    }
  }
}

function requirePasskeySupport(): void {
  if (!window.PublicKeyCredential || !navigator.credentials) {
    throw new Error('Passkeys are not supported by this browser')
  }
}

function bufferToBase64URL(value: ArrayBuffer | null): string | null {
  if (value === null) return null
  const bytes = new Uint8Array(value)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function serializeRegistrationCredential(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse
  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      attestationObject: bufferToBase64URL(response.attestationObject),
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      transports: typeof response.getTransports === 'function' ? response.getTransports() : []
    }
  }
}

function serializeAssertionCredential(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAssertionResponse
  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      authenticatorData: bufferToBase64URL(response.authenticatorData),
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      signature: bufferToBase64URL(response.signature),
      userHandle: bufferToBase64URL(response.userHandle)
    }
  }
}

async function login(proof?: ActionCaptchaRequestProof): Promise<AuthResponse> {
  const assertCurrent = ceremonyGuard()
  requirePasskeySupport()
  const { data: beginResponse } = proof
    ? await apiClient.post<unknown>('/auth/passkey/login/begin', proof)
    : await apiClient.post<unknown>('/auth/passkey/login/begin')
  assertCurrent()
  const begin = ceremonyOptionsFromJSON(beginResponse)
  const credential = await navigator.credentials.get({
    publicKey: requestOptionsFromJSON(begin.publicKey)
  })
  assertCurrent()
  if (!(credential instanceof PublicKeyCredential)) {
    throw new Error('Passkey sign-in was cancelled')
  }
  const { data } = await apiClient.post<AuthResponse>('/auth/passkey/login/finish', {
    session_token: begin.session_token,
    credential: serializeAssertionCredential(credential)
  })
  assertCurrent()
  return data
}

async function register(name: string, password: string, options?: PasskeyRequestOptions): Promise<PasskeyCredentialSummary> {
  const assertCurrent = ceremonyGuard(options)
  assertCurrent()
  requirePasskeySupport()
  const { data: beginResponse } = options
    ? await apiClient.post<unknown>('/user/passkeys/register/begin', { password }, options)
    : await apiClient.post<unknown>('/user/passkeys/register/begin', { password })
  assertCurrent()
  const begin = ceremonyOptionsFromJSON(beginResponse)
  const credential = await navigator.credentials.create({
    publicKey: creationOptionsFromJSON(begin.publicKey),
    ...(options?.signal ? { signal: options.signal } : {})
  })
  assertCurrent()
  if (!(credential instanceof PublicKeyCredential)) {
    throw new Error('Passkey creation was cancelled')
  }
  const payload = {
    session_token: begin.session_token,
    name,
    credential: serializeRegistrationCredential(credential)
  }
  const { data } = options
    ? await apiClient.post<PasskeyCredentialSummary>('/user/passkeys/register/finish', payload, options)
    : await apiClient.post<PasskeyCredentialSummary>('/user/passkeys/register/finish', payload)
  assertCurrent()
  return data
}

async function list(options?: PasskeyRequestOptions): Promise<PasskeyCredentialSummary[]> {
  const { data } = options
    ? await apiClient.get<PasskeyCredentialSummary[]>('/user/passkeys', options)
    : await apiClient.get<PasskeyCredentialSummary[]>('/user/passkeys')
  return data
}

async function rename(id: number, name: string, options?: PasskeyRequestOptions): Promise<void> {
  if (options) await apiClient.patch(`/user/passkeys/${id}`, { name }, options)
  else await apiClient.patch(`/user/passkeys/${id}`, { name })
}

async function remove(id: number, password: string, options?: PasskeyRequestOptions): Promise<void> {
  await apiClient.delete(`/user/passkeys/${id}`, { ...options, data: { password } })
}

export const passkeyAPI = {
  isSupported: () => Boolean(window.PublicKeyCredential && navigator.credentials),
  login,
  register,
  list,
  rename,
  remove
}
