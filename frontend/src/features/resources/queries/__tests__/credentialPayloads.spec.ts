import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { resourceEditorServicesKey } from '../../services'
import type { GeminiAuthUrlRequest } from '../../gemini'
import { useGeminiOAuth } from '../useGeminiOAuth'
import { useGrokOAuth } from '../useGrokOAuth'
import { useAntigravityOAuth } from '../useAntigravityOAuth'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const cleanups: (() => void)[] = []
afterEach(() => cleanups.splice(0).forEach(cleanup => cleanup()))

function setup() {
  const services = editorTestServices()
  const authURL = { auth_url: 'https://auth.example.test/authorize', session_id: 'session', state: 'state' }
  const token = { access_token: 'access-token', expires_at: 1_900_000_000 }
  for (const provider of Object.values(services.credentialAuthorization)) {
    provider.generateAuthUrl = vi.fn().mockResolvedValue(authURL)
    provider.exchangeCode = vi.fn().mockResolvedValue(token)
  }
  let flows: {
    gemini: ReturnType<typeof useGeminiOAuth>
    grok: ReturnType<typeof useGrokOAuth>
    antigravity: ReturnType<typeof useAntigravityOAuth>
  } | undefined
  const wrapper = mount(defineComponent({
    setup() {
      flows = { gemini: useGeminiOAuth(), grok: useGrokOAuth(), antigravity: useAntigravityOAuth() }
      return () => h('div')
    }
  }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
  cleanups.push(() => wrapper.unmount())
  if (!flows) throw new Error('OAuth flows were not mounted')
  return { flows, services, token }
}

describe('typed resource OAuth requests', () => {
  it.each(['gemini', 'grok', 'antigravity'] as const)('%s preserves proxy omission and trimmed code without changing session/state', async provider => {
    const { flows, services, token } = setup()
    const flow = flows[provider]
    const port = services.credentialAuthorization[provider]
    for (const proxy of [undefined, null, 0, 7]) {
      const proxyPayload = proxy ? { proxy_id: proxy } : {}
      expect(await flow.generateAuthUrl(proxy)).toBe(true)
      expect(port.generateAuthUrl).toHaveBeenLastCalledWith(proxyPayload)
      expect(await flow.exchangeAuthCode({
        code: '  code  ', sessionId: 'session', state: ' state ', proxyId: proxy
      })).toBe(token)
      expect(port.exchangeCode).toHaveBeenLastCalledWith({
        session_id: 'session', state: ' state ', code: 'code', ...proxyPayload
      })
    }
  })

  it.each<GeminiAuthUrlRequest['oauth_type']>(['code_assist', 'google_one', 'ai_studio'])('Gemini preserves %s and trims optional project/tier fields', async oauthType => {
    const { flows, services } = setup()
    await flows.gemini.generateAuthUrl(7, ' project ', oauthType, ' tier ')
    expect(services.credentialAuthorization.gemini.generateAuthUrl).toHaveBeenLastCalledWith({
      proxy_id: 7, project_id: 'project', oauth_type: oauthType, tier_id: 'tier'
    })
    await flows.gemini.exchangeAuthCode({ code: ' code ', sessionId: 'session', state: 'state', oauthType, tierId: ' tier ' })
    expect(services.credentialAuthorization.gemini.exchangeCode).toHaveBeenLastCalledWith({
      session_id: 'session', state: 'state', code: 'code', oauth_type: oauthType, tier_id: 'tier'
    })
    await flows.gemini.generateAuthUrl(null, '  ', oauthType, '  ')
    expect(services.credentialAuthorization.gemini.generateAuthUrl).toHaveBeenLastCalledWith({ oauth_type: oauthType })
  })

  it.each(['gemini', 'grok', 'antigravity'] as const)('%s never exchanges incomplete authorization input', async provider => {
    const { flows, services } = setup()
    for (const params of [
      { code: '  ', sessionId: 'session', state: 'state' },
      { code: 'code', sessionId: '', state: 'state' },
      { code: 'code', sessionId: 'session', state: '' }
    ]) {
      expect(await flows[provider].exchangeAuthCode(params)).toBeNull()
    }
    expect(services.credentialAuthorization[provider].exchangeCode).not.toHaveBeenCalled()
  })

  it.each([
    { expiresAt: 1_900_000_000.75, expected: '1900000000' },
    { expiresAt: ' 1900000000 ', expected: '1900000000' },
    { expiresAt: 0, expected: '0' },
    { expiresAt: '', expected: undefined },
    { expiresAt: Number.NaN, expected: undefined },
    { expiresAt: Number.POSITIVE_INFINITY, expected: undefined }
  ])('Gemini retains the existing expiry projection for $expiresAt', ({ expiresAt, expected }) => {
    const { flows } = setup()
    expect(flows.gemini.buildCredentials({ expires_at: expiresAt }).expires_at).toBe(expected)
  })
})
