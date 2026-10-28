import { defineComponent, ref, watch } from 'vue'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ReAuthAccountModal from '../ReAuthAccountModal.vue'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { accountFixture } from '../../__tests__/accountFixture'
import type { Account } from '@/features/resources/api/accountTypes';

const { api } = vi.hoisted(() => ({ api: {
  generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), refreshOpenAIToken: vi.fn(),
  applyOAuthCredentials: vi.fn(), update: vi.fn(), clearError: vi.fn(), create: vi.fn()
} }))
vi.mock('@/features/resources/accounts', () => ({ default: api }))
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

const Flow = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: { initialInputMethod: String, showRefreshTokenOption: Boolean, showSsoOption: Boolean, showEmailPasswordOption: Boolean, loading: Boolean },
  emits: ['generate-url', 'validate-refresh-token', 'import-sso', 'cookie-auth'],
  setup(props, { expose }) {
    const authCode = ref(''), oauthState = ref('state'), setupToken = ref('')
    const inputMethod = ref(props.initialInputMethod || 'manual')
    const reset = () => { authCode.value = ''; setupToken.value = ''; inputMethod.value = props.initialInputMethod || 'manual' }
    watch(() => props.initialInputMethod, value => { inputMethod.value = value || 'manual' })
    expose({ authCode, oauthState, setupToken, inputMethod, projectId: 'test-project', reset })
    return { authCode, oauthState, setupToken, inputMethod }
  },
  template: '<div><input data-test="code" v-model="authCode" /><input data-test="setup-token" v-model="setupToken" /><select data-test="input-method" v-model="inputMethod"><option value="manual">manual</option><option value="refresh_token">refresh</option><option value="sso_cookie">sso</option><option value="setup_token">setup</option></select></div>'
})
const dialogs: VueWrapper[] = []
const token = { access_token: 'test-access', refresh_token: 'test-refresh', email: 'user@example.test', email_address: 'user@example.test', expires_at: 2000000000 }
const authURL = { auth_url: 'https://auth.example.test/authorize?state=state', session_id: 'session', state: 'state' }
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function setup(account: Account, allowExtraEdit = true) {
  const services = editorTestServices()
  const actor = ref('session-1:admin:scope-0')
  services.scopeKey = () => actor.value
  for (const provider of [services.credentialAuthorization.grok, services.credentialAuthorization.gemini, services.credentialAuthorization.antigravity]) {
    provider.generateAuthUrl = vi.fn().mockResolvedValue(authURL)
    provider.exchangeCode = vi.fn().mockResolvedValue(token)
  }
  services.credentialAuthorization.grok.refreshGrokToken = vi.fn().mockResolvedValue(token)
  services.credentialAuthorization.grok.validateSSOToken = vi.fn().mockResolvedValue(token)
  services.credentialAuthorization.antigravity.refreshAntigravityToken = vi.fn().mockResolvedValue(token)
  const saved = { ...account, name: 'Server-confirmed resource', credentials_status: { has_access_token: true } }
  api.applyOAuthCredentials.mockResolvedValue(saved)
  const wrapper = mount(ReAuthAccountModal, { props: { show: true, account, allowExtraEdit }, global: {
    provide: { [resourceEditorServicesKey as symbol]: services },
    stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, OAuthAuthorizationFlow: Flow, Icon: true }
  } })
  dialogs.push(wrapper)
  return { wrapper, flow: wrapper.findComponent(Flow), saved, services, actor }
}
async function exchange(dialog: ReturnType<typeof setup>) {
  dialog.flow.vm.$emit('generate-url')
  await flushPromises()
  await dialog.flow.get('[data-test="input-method"]').setValue('manual')
  await dialog.flow.get('[data-test="code"]').setValue('code')
  await dialog.wrapper.get('button.btn-primary').trigger('click')
  await flushPromises()
}
beforeEach(() => { vi.resetAllMocks(); api.generateAuthUrl.mockResolvedValue(authURL); api.exchangeCode.mockResolvedValue(token); api.refreshOpenAIToken.mockResolvedValue(token) })
afterEach(() => { dialogs.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('one active resource reauthorization implementation', () => {
  it.each(['anthropic', 'openai', 'gemini', 'antigravity', 'grok'] as const)('commits %s code authorization once and emits the returned Account', async platform => {
    const dialog = setup(accountFixture({ platform }))
    await exchange(dialog)
    expect(dialog.services.notices.showError).not.toHaveBeenCalled()
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    expect(api.applyOAuthCredentials.mock.calls[0][0]).toBe(42)
    expect(api.applyOAuthCredentials.mock.calls[0][1].credentials.access_token).toBe('test-access')
    expect(dialog.wrapper.emitted('reauthorized')?.[0]).toEqual([dialog.saved])
    expect(dialog.wrapper.emitted('close')).toHaveLength(1)
    expect(api.update).not.toHaveBeenCalled()
    expect(api.clearError).not.toHaveBeenCalled()
    expect(api.create).not.toHaveBeenCalled()
  })

  it.each(['openai', 'antigravity', 'grok'] as const)('retains single refresh-token reauthorization for %s', async platform => {
    const dialog = setup(accountFixture({ platform }))
    dialog.flow.vm.$emit('validate-refresh-token', '  first-token\nsecond-token  ')
    await flushPromises()
    const refresh = platform === 'openai' ? api.refreshOpenAIToken : platform === 'grok' ? dialog.services.credentialAuthorization.grok.refreshGrokToken : dialog.services.credentialAuthorization.antigravity.refreshAntigravityToken
    expect(vi.mocked(refresh).mock.calls[0].slice(0, 2)).toEqual(['first-token', 7])
    expect(dialog.services.notices.showError).not.toHaveBeenCalled()
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    expect(dialog.wrapper.emitted('reauthorized')?.[0]).toEqual([dialog.saved])
    expect(api.create).not.toHaveBeenCalled()
  })

  it('uses the first SSO token to reauthorize the current Grok resource without creating accounts', async () => {
    const dialog = setup(accountFixture({ platform: 'grok' }))
    expect(dialog.flow.props('showSsoOption')).toBe(true)
    expect(dialog.flow.props('showRefreshTokenOption')).toBe(true)
    expect(dialog.flow.props('showEmailPasswordOption')).toBe(false)
    expect(dialog.flow.props('initialInputMethod')).toBe('sso_cookie')
    await flushPromises()
    expect(dialog.wrapper.find('button.btn-primary').exists()).toBe(false)
    dialog.flow.vm.$emit('import-sso', '\n first-sso\nsecond-sso')
    await flushPromises()
    expect(dialog.services.credentialAuthorization.grok.validateSSOToken).toHaveBeenCalledWith('first-sso', 7)
    expect(dialog.services.credentialAuthorization.grok.createFromSSO).not.toHaveBeenCalled()
    expect(dialog.wrapper.emitted('reauthorized')?.[0]).toEqual([dialog.saved])
  })

  it('recognizes both old and redacted Grok refresh-token presence markers', async () => {
    const dialog = setup(accountFixture({ platform: 'grok', credentials_status: { has_refresh_token: true } }))
    expect(dialog.flow.props('initialInputMethod')).toBe('refresh_token')
    await dialog.wrapper.setProps({ account: accountFixture({ id: 43, platform: 'grok', credentials: { has_refresh_token: true } }) })
    expect(dialog.flow.props('initialInputMethod')).toBe('refresh_token')
  })

  it.each([
    { tier: ' google_ai_pro ', expected: 'google_ai_pro' },
    { tier: '', expected: undefined },
    { tier: '  ', expected: undefined },
    { tier: 0, expected: undefined },
    { tier: null, expected: undefined },
    { tier: { id: 'google_ai_pro' }, expected: undefined }
  ])('only forwards a nonblank string Gemini tier from credentials: $tier', async ({ tier, expected }) => {
    const dialog = setup(accountFixture({ platform: 'gemini', credentials: { tier_id: tier } }))
    await exchange(dialog)
    const tierPayload = expected === undefined ? {} : { tier_id: expected }
    expect(dialog.services.credentialAuthorization.gemini.generateAuthUrl).toHaveBeenCalledWith({
      proxy_id: 7, project_id: 'test-project', oauth_type: 'code_assist', ...tierPayload
    })
    expect(dialog.services.credentialAuthorization.gemini.exchangeCode).toHaveBeenCalledWith({
      session_id: 'session', state: 'state', code: 'code', proxy_id: 7,
      oauth_type: 'code_assist', ...tierPayload
    })
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
  })

  it('preserves Grok OAuth metadata while excluding password and SSO fields from reauthorization', async () => {
    const dialog = setup(accountFixture({ platform: 'grok' }))
    vi.mocked(dialog.services.credentialAuthorization.grok.validateSSOToken).mockResolvedValueOnce({
      ...token, token_type: 'Bearer', id_token: 'id-token', client_id: 'client',
      scope: 'openid grok-cli:access', sub: 'subject', team_id: 'team',
      subscription_tier: 'SuperGrok', entitlement_status: 'active',
      password: 'password-secret', sso_token: 'sso-secret', sso: 'sso-secret', 'sso-rw': 'sso-secret'
    })
    dialog.flow.vm.$emit('import-sso', 'submitted-sso')
    await flushPromises()
    expect(api.applyOAuthCredentials).toHaveBeenCalledWith(42, {
      type: 'oauth',
      credentials: {
        access_token: token.access_token, refresh_token: token.refresh_token,
        expires_at: token.expires_at, email: token.email,
        token_type: 'Bearer', id_token: 'id-token', client_id: 'client',
        scope: 'openid grok-cli:access', sub: 'subject', team_id: 'team',
        subscription_tier: 'SuperGrok', entitlement_status: 'active'
      },
      extra: { email: token.email, subscription_tier: 'SuperGrok', entitlement_status: 'active' }
    })
  })

  it('retains pasted Claude setup-token and Cookie authorization', async () => {
    const dialog = setup(accountFixture({ platform: 'anthropic' }))
    await dialog.wrapper.get('input[value="setup-token-manual"]').setValue(true)
    await flushPromises()
    await dialog.flow.get('[data-test="setup-token"]').setValue('sk-ant-oat-test')
    await dialog.wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(api.applyOAuthCredentials).toHaveBeenCalledWith(42, { type: 'setup-token', credentials: { access_token: 'sk-ant-oat-test', token_type: 'oauth', scope: 'user:inference' } })
    const cookie = setup(accountFixture({ id: 43, platform: 'anthropic' }))
    cookie.flow.vm.$emit('cookie-auth', ' cookie-token ')
    await flushPromises()
    expect(api.exchangeCode).toHaveBeenLastCalledWith('/admin/accounts/cookie-auth', { session_id: '', code: 'cookie-token', proxy_id: 7 })
    expect(cookie.wrapper.emitted('reauthorized')?.[0]).toEqual([cookie.saved])
  })

  it.each(['openai', 'grok', 'anthropic'] as const)('omits extra edits when disallowed for %s', async platform => {
    const dialog = setup(accountFixture({ platform }), false)
    await exchange(dialog)
    expect(api.applyOAuthCredentials.mock.calls[0][1]).not.toHaveProperty('extra')
  })

  it.each(['actor', 'account', 'close', 'unmount'] as const)('discards exchanged credentials after %s changes', async transition => {
    const result = deferred<typeof token>()
    api.exchangeCode.mockReturnValueOnce(result.promise)
    const dialog = setup(accountFixture())
    await exchange(dialog)
    if (transition === 'actor') dialog.actor.value = 'session-2:supplier:scope-9'
    if (transition === 'account') await dialog.wrapper.setProps({ account: accountFixture({ id: 99 }) })
    if (transition === 'close') { await dialog.wrapper.setProps({ show: false }); await dialog.wrapper.setProps({ show: true }) }
    if (transition === 'unmount') dialog.wrapper.unmount()
    result.resolve(token)
    await flushPromises()
    expect(api.applyOAuthCredentials).not.toHaveBeenCalled()
    expect(dialog.wrapper.emitted('reauthorized')).toBeUndefined()
    expect(dialog.services.notices.showError).not.toHaveBeenCalled()
  })

  it('drops the completion of an already sent save after an actor switch', async () => {
    const dialog = setup(accountFixture())
    const result = deferred<Account>()
    api.applyOAuthCredentials.mockReturnValueOnce(result.promise)
    await exchange(dialog)
    expect(dialog.services.notices.showError).not.toHaveBeenCalled()
    expect(api.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    dialog.actor.value = 'session-2:admin:scope-0'
    result.resolve(dialog.saved)
    await flushPromises()
    expect(dialog.wrapper.emitted('reauthorized')).toBeUndefined()
    expect(dialog.services.notices.showSuccess).not.toHaveBeenCalled()
  })
})

