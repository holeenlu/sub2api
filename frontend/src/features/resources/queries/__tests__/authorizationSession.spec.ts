import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent, h, type Ref } from 'vue'
import { mount } from '@vue/test-utils'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { useAccountOAuth, useOpenAIOAuth, useGeminiOAuth, useAntigravityOAuth, useGrokOAuth } from '..'

const transport = vi.hoisted(() => ({ generateAuthUrl: vi.fn(), exchangeCode: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: transport }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
interface Probe { start(proxy: number): Promise<boolean>; exchange(): Promise<unknown>; url: Ref<string>; loading: Ref<boolean> }
type Provider = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok'
function setup(provider: Provider) {
  const services = editorTestServices()
  for (const name of ['gemini', 'antigravity', 'grok'] as const) {
    services.credentialAuthorization[name].generateAuthUrl = transport.generateAuthUrl
    services.credentialAuthorization[name].exchangeCode = transport.exchangeCode
  }
  let probe!: Probe
  const wrapper = mount(defineComponent({ setup() {
    if (provider === 'anthropic') {
      const flow = useAccountOAuth()
      flow.authCode.value = 'fixture-code'
      probe = { start: proxy => flow.generateAuthUrl('oauth', proxy), exchange: () => flow.exchangeAuthCode('oauth', 11), url: flow.authUrl, loading: flow.loading }
    } else if (provider === 'openai') {
      const flow = useOpenAIOAuth()
      probe = { start: proxy => flow.generateAuthUrl(proxy), exchange: () => flow.exchangeAuthCode('fixture-code', 'old-session', 'old-state', 11), url: flow.authUrl, loading: flow.loading }
    } else {
      const flow = provider === 'gemini' ? useGeminiOAuth() : provider === 'antigravity' ? useAntigravityOAuth() : useGrokOAuth()
      probe = { start: proxy => flow.generateAuthUrl(proxy), exchange: () => flow.exchangeAuthCode({ code: 'fixture-code', sessionId: 'old-session', state: 'old-state', proxyId: 11 }), url: flow.authUrl, loading: flow.loading }
    }
    return () => h('div')
  } }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
  return { probe, wrapper }
}
const oldSession = { auth_url: 'https://auth.example.invalid/?state=old-state', session_id: 'old-session', state: 'old-state' }
const newSession = { auth_url: 'https://auth.example.invalid/?state=new-state', session_id: 'new-session', state: 'new-state' }
beforeEach(() => { vi.resetAllMocks() })

it.each<Provider>(['anthropic', 'openai', 'gemini', 'antigravity', 'grok'])('%s keeps the newest authorization URL when proxy requests complete out of order', async provider => {
  const old = deferred<typeof oldSession>()
  transport.generateAuthUrl.mockReturnValueOnce(old.promise).mockResolvedValueOnce(newSession)
  const { probe, wrapper } = setup(provider)
  try {
    const waiting = probe.start(11)
    expect(await probe.start(22)).toBe(true)
    old.resolve(oldSession)
    expect(await waiting).toBe(false)
    expect(probe.url.value).toBe(newSession.auth_url)
  } finally { wrapper.unmount() }
})

it.each<Provider>(['anthropic', 'openai', 'gemini', 'antigravity', 'grok'])('%s rejects a previous exchange result after a new authorization session starts', async provider => {
  const token = deferred<{ access_token: string }>()
  transport.generateAuthUrl.mockResolvedValueOnce(oldSession).mockResolvedValueOnce(newSession)
  transport.exchangeCode.mockReturnValue(token.promise)
  const { probe, wrapper } = setup(provider)
  try {
    await probe.start(11)
    const waiting = probe.exchange()
    await probe.start(22)
    token.resolve({ access_token: 'old-sensitive-token' })
    expect(await waiting).toBeNull()
    expect(probe.url.value).toBe(newSession.auth_url)
    expect(transport.exchangeCode).toHaveBeenCalledTimes(1)
  } finally { wrapper.unmount() }
})


it('does not cancel an independent Gemini capability read when authorizing a new session', async () => {
  const capability = deferred<{ fixture: string }>()
  const services = editorTestServices()
  services.credentialAuthorization.gemini.getCapabilities = vi.fn().mockReturnValue(capability.promise)
  services.credentialAuthorization.gemini.generateAuthUrl = vi.fn().mockResolvedValue(newSession)
  let flow!: ReturnType<typeof useGeminiOAuth>
  const wrapper = mount(defineComponent({ setup() { flow = useGeminiOAuth(); return () => h('div') } }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
  try {
    const reading = flow.getCapabilities()
    await flow.generateAuthUrl(22)
    const result = { fixture: 'server-capabilities' }
    capability.resolve(result)
    expect(await reading).toBe(result)
  } finally { wrapper.unmount() }
})
