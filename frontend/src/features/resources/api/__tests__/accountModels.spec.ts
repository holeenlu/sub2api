import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/shared/http'
import { saveAccountModels } from '../accountModels'

const originalAdapter = apiClient.defaults.adapter
let requests: InternalAxiosRequestConfig[]
let conflict = false
const adapter: AxiosAdapter = async config => {
  requests.push(config)
  const response = { config, status: conflict ? 409 : 200, statusText: conflict ? 'Conflict' : 'OK', headers: new AxiosHeaders(), data: conflict ? { code: 409, reason: 'MODEL_ALLOWLIST_VERSION_CONFLICT', message: 'Version conflict' } : { code: 0, data: { models: null } } }
  if (conflict) throw new AxiosError('Conflict', AxiosError.ERR_BAD_REQUEST, config, undefined, response)
  return response
}
beforeEach(() => {
  requests = []; conflict = false
  localStorage.setItem('auth_token', 'fixture-model-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 3 }))
  apiClient.defaults.adapter = adapter
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe('account model CAS over the real shared HTTP client', () => {
  it('sends PUT expected/models, the shared session token and caller signal; unwraps null', async () => {
    const controller = new AbortController()
    expect(await saveAccountModels(42, ['seed'], [], { signal: controller.signal })).toEqual({ models: null })
    expect(requests).toHaveLength(1)
    const request = requests[0]
    if (!request) throw new Error('Expected HTTP request')
    expect(request.url).toBe('/admin/models/accounts/42/allowlist')
    expect(request.method).toBe('put')
    expect(JSON.parse(request.data)).toEqual({ expected: ['seed'], models: [] })
    expect(request.headers.get('Authorization')).toBe('Bearer fixture-model-session')
    expect(request.signal).toBe(controller.signal)
  })
  it('propagates a server CAS conflict without retrying or replacing expected', async () => {
    conflict = true
    await expect(saveAccountModels(42, ['seed'], ['new'])).rejects.toMatchObject({ status: 409, message: 'Version conflict' })
    expect(requests).toHaveLength(1)
    const request = requests[0]
    if (!request) throw new Error('Expected HTTP request')
    expect(JSON.parse(request.data)).toEqual({ expected: ['seed'], models: ['new'] })
  })
  it('does not send an already cancelled write', async () => {
    const controller = new AbortController(); controller.abort()
    await expect(saveAccountModels(42, [], ['new'], { signal: controller.signal })).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(requests).toHaveLength(0)
  })
})
