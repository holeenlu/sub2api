import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SIWCAccountSettings from '../SIWCAccountSettings.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }))
const copy = vi.hoisted(() => vi.fn())
vi.mock('@/api/client', () => ({ apiClient: api }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const pending = { bound: false, enabled: false, registered: false, identity_verified: false, direct_access: false }
const authorized = { bound: true, enabled: false, registered: true, identity_verified: true, direct_access: true, client_id: 'oaiapp_selected', email: 'verified@example.test', subject: 'subscriber' }
const authURL = 'https://auth.openai.com/api/accounts/authorize?' + new URLSearchParams({
  client_id: 'dynamic_agent_client', state: 'current-state', redirect_uri: 'http://127.0.0.1:1455/auth/callback'
})
const callbackURL = 'http://127.0.0.1:1455/auth/callback?state=current-state&code=one-use&client_id=oaiapp_selected'
const render = (props = {}) => mount(SIWCAccountSettings, { props: { accountId: 1, enabled: false, active: true, ...props } })
async function start(wrapper: ReturnType<typeof render>) {
  await flushPromises()
  await wrapper.get('[data-testid="siwc-authorize"]').trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  api.get.mockResolvedValue({ data: pending })
  api.post.mockResolvedValue({ data: { auth_url: authURL, session_id: 'session-1' } })
  copy.mockResolvedValue(true)
})

describe('SIWC manual authorization', () => {
  it('keeps saved protocol selection separate from authorization and requires verified direct permission', async () => {
    api.get.mockResolvedValue({ data: authorized })
    const wrapper = render({ enabled: true })
    await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('select').element.value).toBe('siwc')
    expect(wrapper.get('[data-testid="siwc-identity"]').text()).toBe('verified@example.test')
    expect(wrapper.get('[data-testid="siwc-registration"]').text()).toContain('oaiapp_selected')
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    api.get.mockResolvedValue({ data: { ...authorized, direct_access: false } })
    await wrapper.get('[data-testid="siwc-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toBe('siwc.identityOnly')
    expect(wrapper.get('option[value="siwc"]').attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('codex')
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
    wrapper.unmount()
  })

  it('uses the full authorization link and saves callback without auto-enabling the protocol', async () => {
    const wrapper = render()
    await start(wrapper)
    expect(api.post).toHaveBeenCalledWith('/admin/accounts/1/siwc/authorize')
    expect(wrapper.get('textarea').element.value).toBe(authURL)
    expect(wrapper.get('[data-testid="siwc-open"]').attributes('href')).toBe(authURL)
    expect(wrapper.text()).toContain('siwc.listenerHint')
    await wrapper.get('[data-testid="siwc-copy"]').trigger('click')
    expect(copy).toHaveBeenCalledWith(authURL)
    api.get.mockResolvedValue({ data: authorized })
    await wrapper.get('[data-testid="siwc-callback"]').setValue(callbackURL)
    await wrapper.get('[data-testid="siwc-complete"]').trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenLastCalledWith('/admin/accounts/1/siwc/callback', { session_id: 'session-1', callback_url: callbackURL })
    expect(wrapper.find('[data-testid="siwc-browser-flow"]').exists()).toBe(false)
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    await wrapper.get('select').setValue('siwc')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    wrapper.unmount()
  })

  it.each([
    [callbackURL.replace('127.0.0.1', 'localhost'), 'siwc.invalidCallback'],
    [callbackURL.replace('current-state', 'other-state'), 'siwc.stateMismatch'],
    [callbackURL + '&state=duplicate', 'siwc.invalidCallback'],
    [callbackURL + '&error=access_denied', 'siwc.invalidCallback'],
  ])('rejects a malformed or mismatched callback without consuming the pending attempt (%s)', async (callback, message) => {
    const wrapper = render()
    await start(wrapper)
    await wrapper.get('[data-testid="siwc-callback"]').setValue(callback)
    await wrapper.get('[data-testid="siwc-complete"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(message)
    expect(api.post).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="siwc-browser-flow"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('displays denial and exchange errors, clears one-use material, and leaves selected routing unchanged', async () => {
    const wrapper = render()
    await start(wrapper)
    api.post.mockRejectedValueOnce({ code: 'OPENAI_SIWC_CONSENT_REQUIRED', message: 'Permission declined' })
    await wrapper.get('[data-testid="siwc-callback"]').setValue('http://127.0.0.1:1455/auth/callback?state=current-state&error=access_denied')
    await wrapper.get('[data-testid="siwc-complete"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Permission declined')
    expect(wrapper.find('[data-testid="siwc-browser-flow"]').exists()).toBe(false)
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('rejects an expired attempt and a changed returning registration locally', async () => {
    const wrapper = render()
    await start(wrapper)
    api.post.mockRejectedValueOnce({ code: 'OPENAI_SIWC_AUTH_CHANGED', message: 'Sign-in expired' })
    await wrapper.get('[data-testid="siwc-callback"]').setValue(callbackURL)
    await wrapper.get('[data-testid="siwc-complete"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Sign-in expired')
    api.post.mockResolvedValueOnce({ data: { auth_url: authURL.replace('dynamic_agent_client', 'oaiapp_other'), session_id: 'returning' } })
    await wrapper.get('[data-testid="siwc-authorize"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="siwc-callback"]').setValue(callbackURL)
    await wrapper.get('[data-testid="siwc-complete"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('siwc.clientMismatch')
    expect(api.post).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('reports unconfirmed remote revocation and disables only SIWC selection after local disconnect', async () => {
    api.get.mockResolvedValueOnce({ data: authorized }).mockResolvedValue({ data: { ...pending, registered: true, client_id: authorized.client_id } })
    api.delete.mockResolvedValue({ data: { bound: false, remote_revocation_confirmed: false, message: 'Disconnected locally' } })
    const wrapper = render({ enabled: true })
    await flushPromises()
    await wrapper.get('[data-testid="siwc-disconnect"]').trigger('click')
    await flushPromises()
    expect(api.delete).toHaveBeenCalledWith('/admin/accounts/1/siwc')
    expect(wrapper.find('[data-testid="siwc-revocation-unconfirmed"]').exists()).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toBe('siwc.registered')
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
    wrapper.unmount()
  })

  it('discards stale account replies and clears transient secrets when the dialog closes', async () => {
    let resolveOld!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = render()
    await wrapper.setProps({ accountId: 2 })
    await flushPromises()
    resolveOld({ data: authorized })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toBe('siwc.unbound')
    await start(wrapper)
    await wrapper.get('[data-testid="siwc-callback"]').setValue(callbackURL)
    await wrapper.setProps({ active: false })
    expect(wrapper.find('[data-testid="siwc-browser-flow"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('exposes no credential mutation controls without authorization permission', async () => {
    api.get.mockResolvedValue({ data: authorized })
    const wrapper = render({ canAuthorize: false })
    await flushPromises()
    expect(wrapper.find('[data-testid="siwc-authorize"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="siwc-disconnect"]').exists()).toBe(false)
    expect(wrapper.get('select').exists()).toBe(true)
    wrapper.unmount()
  })
})
