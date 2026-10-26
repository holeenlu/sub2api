import Select from '@/components/common/Select.vue'
import { createPinia } from 'pinia'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AccountForm from '../components/AccountForm.vue'
import en from '@/i18n/locales/en/vendor'
import nativeEn from '@/i18n/locales/en'
import type { OpenAIAuthorizationMode, ResourceScope, Vendor, VendorAccount } from '../types'
const selector = (w: VueWrapper, label: string) =>
  w.findAllComponents(Select).find((s) => s.props('ariaLabel') === label)!
async function choose(w: VueWrapper, label: string, value: string) {
  if (label === en.platform || label === en.method || label === en.authorization.mode) {
    const field = w
      .findAll('fieldset')
      .find((f) => f.find('legend').exists() && f.find('legend').text() === label)!
    await field.get('input[value="' + value + '"]').setValue(true)
    await flushPromises()
    return
  }
  if (label === en.proxy && !selector(w, label)) {
    await w.get('input[value="existing"]').setValue(true)
    await flushPromises()
  }
  const control = selector(w, label)
  expect(control).toBeDefined()
  await control.find('button').trigger('click')
  const option = (control.props('options') as { value: string; label: string }[]).find(
    (o) => o.value === value
  )!
  expect(option).toBeDefined()
  await control
    .findAll('[role=option]')
    .find((o) => o.text() === option.label)!
    .trigger('click')
  await flushPromises()
}
const mocks = vi.hoisted(() => ({
  proxies: vi.fn(),
  startOAuth: vi.fn(),
  rebindOAuth: vi.fn(),
  completeOAuth: vi.fn(),
  updateAccount: vi.fn(),
  createAccount: vi.fn(),
  createProxy: vi.fn()
}))
vi.mock('../api', () => ({
  resourceAPI: () => ({
    proxies: mocks.proxies,
    updateAccount: mocks.updateAccount,
    createAccount: mocks.createAccount,
    createProxy: mocks.createProxy
  }),
  vendorAPI: mocks
}))
const vendor: Vendor = {
  id: 1,
  user_id: 3,
  name: 'Supplier',
  status: 'active',
  policy_version: 1,
  platforms: ['anthropic', 'openai'],
  group_ids: [5],
  groups: [
    { id: 5, name: 'Claude', platform: 'anthropic', is_default: true },
    { id: 6, name: 'OpenAI', platform: 'openai' }
  ],
  account_limit: 100,
  proxy_limit: 100,
  allow_direct: false,
  account_defaults: { concurrency: 10, priority: 1 },
  contact_name: '',
  settlement_email: '',
  binance_id: '',
  default_coefficient: '1',
  group_coefficients: {},
  contract_version: 1
}
function render(
  scope: ResourceScope = { admin: false },
  policy = vendor,
  account?: VendorAccount,
  authorization: { reauthorize?: boolean; authorizationMode?: OpenAIAuthorizationMode } = {}
) {
  return mount(AccountForm, {
    props: { vendor: policy, scope, account, ...authorization },
    global: {
      plugins: [
        createPinia(),
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: nativeEn }
        })
      ],
      stubs: {
        teleport: true,
        BaseDialog: { template: '<div><slot /><footer><slot name="footer" /></footer></div>' },
      }
    }
  })
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.proxies.mockResolvedValue({
    items: [
      {
        id: 1,
        name: 'One',
        status: 'active',
        protocol: 'http',
        host: 'one.test',
        port: 80
      },
      {
        id: 2,
        name: 'Two',
        status: 'active',
        protocol: 'http',
        host: 'two.test',
        port: 80
      },
      {
        id: 9,
        name: 'Expired',
        status: 'inactive',
        protocol: 'http',
        host: 'bad.test',
        port: 80
      }
    ],
    total: 3
  })
  mocks.startOAuth.mockResolvedValue({
    auth_url: 'https://oauth.example/authorize',
    session_id: 'lease1',
    expires_at: '2099-01-01T00:00:00Z'
  })
  mocks.rebindOAuth.mockResolvedValue({ rebound: true })
})
describe('supplier account authorization', () => {
  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'allows an omitted name and preserves the server label after OAuth in scope %j',
    async scope => {
      mocks.completeOAuth.mockResolvedValue({ id: 42, name: 'verified@example.invalid' })
      mocks.updateAccount.mockResolvedValue({ id: 42 })
      const wrapper = render(scope)
      await flushPromises()
      const name = wrapper.get('[data-testid="account-name"]')
      expect(name.attributes('required')).toBeUndefined()
      expect(name.attributes('placeholder')).toBe('claude-max-01')
      expect(wrapper.get('textarea').attributes('placeholder')).toBe(en.accountNotesPlaceholder)
      await wrapper.get('textarea').setValue('  source channel  ')
      await choose(wrapper, en.proxy, '1')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(mocks.startOAuth.mock.calls[0][1]).not.toHaveProperty('name')
      await wrapper.findAll('textarea')[1].setValue('code#state')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(mocks.updateAccount).toHaveBeenCalledWith(42, expect.objectContaining({ vendor_notes: 'source channel' }))
      expect(mocks.updateAccount.mock.calls[0][1]).not.toHaveProperty('name')
      expect(wrapper.emitted('saved')).toHaveLength(1)
      wrapper.unmount()
    }
  )
  it.each(['VENDOR_OAUTH_RESTART_REQUIRED', 'VENDOR_IDENTITY_KEY_MISMATCH', 'ERR_NETWORK'])(
    'handles authorization failure %s without losing account metadata', async reason => {
      mocks.completeOAuth.mockRejectedValueOnce({ reason, message: 'Authorization failed' })
      const wrapper = render()
      await flushPromises()
      await wrapper.get('[data-testid="account-name"]').setValue('Keep name')
      await choose(wrapper, en.proxy, '1')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      await wrapper.findAll('textarea')[1].setValue('callback#state')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect((wrapper.get('[data-testid="account-name"]').element as HTMLInputElement).value).toBe('Keep name')
      if (reason === 'VENDOR_OAUTH_RESTART_REQUIRED') {
        expect(wrapper.findAll('textarea:not(:disabled)')).toHaveLength(1)
        expect(wrapper.find('a').exists()).toBe(false)
        expect(wrapper.get('[role=alert]').text()).toBe(en.oauthRestartRequired)
        await wrapper.get('form').trigger('submit')
        await flushPromises()
        expect(mocks.startOAuth).toHaveBeenCalledTimes(2)
        expect(mocks.completeOAuth).toHaveBeenCalledTimes(1)
      } else {
        expect((wrapper.findAll('textarea')[1].element as HTMLTextAreaElement).value).toBe('callback#state')
        expect(wrapper.get('[role=alert]').text()).toBe(reason === 'ERR_NETWORK' ? 'Authorization failed' : en.identityKeyMismatch)
      }
      wrapper.unmount()
    }
  )
  it('allows an omitted name for manual token authorization', async () => {
    mocks.createAccount.mockResolvedValue({ id: 43, name: 'verified@example.invalid' })
    const wrapper = render()
    await flushPromises()
    await choose(wrapper, en.proxy, '1')
    await choose(wrapper, en.method, 'manual')
    await wrapper.get('textarea[autocomplete="off"]').setValue('verified-setup-token')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.createAccount).toHaveBeenCalledWith(expect.objectContaining({ token: 'verified-setup-token' }))
    expect(mocks.createAccount.mock.calls[0][0]).not.toHaveProperty('name')
    wrapper.unmount()
  })
  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'drops defaults from another platform before adding multiple permitted groups in %j',
    async (scope) => {
      const wrapper = render(scope, {
        ...vendor,
        group_ids: [6, 5, 7],
        groups: [
          {
            id: 6,
            name: 'Default OpenAI',
            platform: 'openai',
            is_default: true
          },
          { id: 5, name: 'Claude', platform: 'anthropic' },
          { id: 7, name: 'Claude second group', platform: 'anthropic' }
        ]
      })
      await flushPromises()
      expect(wrapper.find('input[type=checkbox][value="6"]').exists()).toBe(false)
      expect(
        (wrapper.get('input[type=checkbox][value="5"]').element as HTMLInputElement).checked
      ).toBe(true)
      await wrapper.get('input[type=checkbox][value="7"]').setValue(true)
      await wrapper.find('[data-testid="account-name"]').setValue('Multi-group account')
      await choose(wrapper, en.proxy, '1')
      await wrapper.find('form').trigger('submit')
      await flushPromises()
      expect(mocks.startOAuth).toHaveBeenCalledWith(
        'anthropic',
        expect.objectContaining({ group_ids: [5, 7] }),
        scope
      )
      expect(wrapper.find('[role=alert]').exists()).toBe(false)
      wrapper.unmount()
    }
  )
  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'rebinds an unfinished lease while preserving authorization and scope %j',
    async (scope) => {
      const wrapper = render(scope)
      await flushPromises()
      await wrapper.find('[data-testid="account-name"]').setValue('Preserved name')
      await wrapper.find('textarea').setValue('Preserved notes')
      await choose(wrapper, en.proxy, '1')
      await wrapper.find('form').trigger('submit.prevent')
      await flushPromises()
      const callback = wrapper.findAll('textarea')[1]
      await callback.setValue('private-code#state')
      await choose(wrapper, en.proxy, '2')
      await flushPromises()
      expect(mocks.rebindOAuth).toHaveBeenCalledWith(
        'anthropic',
        'lease1',
        {
          proxy_id: 2,
          direct: false
        },
        scope
      )
      expect((callback.element as HTMLTextAreaElement).value).toBe('private-code#state')
      expect((wrapper.find('[data-testid="account-name"]').element as HTMLInputElement).value).toBe(
        'Preserved name'
      )
      expect(mocks.startOAuth).toHaveBeenCalledTimes(1)
      expect(mocks.startOAuth).toHaveBeenCalledWith(
        'anthropic',
        expect.objectContaining({ name: 'Preserved name' }),
        scope
      )
      await choose(wrapper, en.platform, 'openai')
      await flushPromises()
      expect(wrapper.find('a').exists()).toBe(false)
      expect(wrapper.findAll('textarea:not(:disabled)')).toHaveLength(1)
      expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe(
        'Preserved notes'
      )
      wrapper.unmount()
    }
  )
  it('rolls back failed proxy changes without deleting the callback', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.find('[data-testid="account-name"]').setValue('Account')
    await choose(wrapper, en.proxy, '1')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    await wrapper.findAll('textarea')[1].setValue('code#state')
    mocks.rebindOAuth.mockRejectedValueOnce({
      message: 'Session already claimed'
    })
    await choose(wrapper, en.proxy, '2')
    await flushPromises()
    expect(selector(wrapper, en.proxy).props('modelValue')).toBe('1')
    expect((wrapper.findAll('textarea')[1].element as HTMLTextAreaElement).value).toBe('code#state')
    expect(wrapper.text()).toContain('Session already claimed')
    wrapper.unmount()
  })
  it('excludes unavailable proxy choices and blocks empty permitted groups', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('input[value="existing"]').setValue(true)
    expect(
      selector(wrapper, en.proxy)
        .props('options')
        .map((o) => o.label)
        .join(' ')
    ).not.toContain('Expired')
    await wrapper.find('[data-testid="account-name"]').setValue('Account')
    await choose(wrapper, en.proxy, '1')
    await wrapper.find('input[type=checkbox]').setValue(false)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(mocks.startOAuth).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(en.requiredGroups)
    wrapper.unmount()
  })
})

describe('OpenAI native and BPS authorization', () => {
  const openai = { ...vendor, platforms: ['openai'] as const, openai_authorization_modes: ['native', 'bps'] as OpenAIAuthorizationMode[] }
  const policy = { ...openai, platforms: [...openai.platforms] }
  const existing = { id: 42, name: 'Existing OpenAI', platform: 'openai', type: 'oauth', proxy_id: 1, group_ids: [6], groups: [vendor.groups[1]], authorization_modes: ['native'] } as VendorAccount

  it.each([
    [undefined, ['native']],
    [[], []],
    [['bps'], ['bps']],
    [['native', 'bps'], ['native', 'bps']]
  ] as [OpenAIAuthorizationMode[] | undefined, OpenAIAuthorizationMode[]][])(
    'uses effective mode permissions %j, defaulting only missing legacy fields', async (modes, expected) => {
      const w = render({ admin: false }, { ...policy, openai_authorization_modes: modes })
      await flushPromises()
      expect(w.findAll('[data-testid=account-authorization-mode] input').map(i => i.attributes('value'))).toEqual(expected)
      await w.find('[data-testid="account-name"]').setValue('OpenAI account')
      await choose(w, en.proxy, '1')
      await w.get('form').trigger('submit')
      await flushPromises()
      if (expected.length) expect(mocks.startOAuth).toHaveBeenCalledWith('openai', expect.objectContaining({ authorization_mode: expected[0], authorization_action: 'create' }), { admin: false })
      else expect(mocks.startOAuth).not.toHaveBeenCalled()
      w.unmount()
    }
  )

  it.each([
    ['bps', ['native'], 'attach'],
    ['native', ['bps'], 'attach'],
    ['bps', ['native', 'bps'], 'reauthorize']
  ] as const)('binds %s authorization to the existing account with action %s', async (mode, grants, action) => {
    const redirect = mode === 'native' ? 'http://localhost:1455/auth/callback' : 'https://bps.openai.com/example/callback'
    mocks.startOAuth.mockResolvedValueOnce({auth_url:'https://auth.openai.com/oauth/authorize?redirect_uri='+encodeURIComponent(redirect),session_id:'lease1',expires_at:'2099-01-01T00:00:00Z'})
    const scope = { admin: true, vendorId: 8 }
    const w = render(scope, policy, { ...existing, authorization_modes: [...grants] }, { reauthorize: true, authorizationMode: mode })
    await flushPromises()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.startOAuth).toHaveBeenCalledWith('openai', expect.objectContaining({ authorization_mode: mode, authorization_action: action, target_account_id: 42, group_ids: [6], proxy_id: 1 }), scope)
    expect(w.get('textarea[required]').attributes('placeholder')).toBe(redirect + '?code=...&state=...')
    expect(w.get('[data-authorization-guide]').text()).toContain(en.oauthGuide[mode === 'native' ? 'nativeLogin' : 'bpsLogin'])
    expect(w.text()).toContain(en.oauthGuide[mode === 'native' ? 'nativeCallbackHelp' : 'bpsCallbackHelp'])
    expect(w.text()).toContain(en.leaseExpires)
    mocks.completeOAuth.mockResolvedValueOnce({ ...existing, authorization_modes: ['native', 'bps'] })
    mocks.updateAccount.mockResolvedValueOnce(existing)
    await w.get('textarea[required]').setValue('https://bps.openai.com/callback?code=example&state=test')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.completeOAuth).toHaveBeenCalledWith('openai', 'lease1', expect.any(String), scope)
    expect(mocks.updateAccount.mock.lastCall?.[1]).not.toHaveProperty('credentials')
    expect(w.emitted('saved')).toHaveLength(1)
    expect(w.find('textarea[required]').exists()).toBe(false)
    w.unmount()
  })

  it('imports only the selected mode and clears manual credentials after storing on the server', async () => {
    const w = render({ admin: false }, policy, existing, { reauthorize: true, authorizationMode: 'bps' })
    mocks.createAccount.mockResolvedValueOnce(existing)
    await flushPromises()
    await choose(w, en.method, 'manual')
    expect(w.text()).toContain(en.authorization.tokenHelp)
    expect(w.get('textarea[required]').attributes('placeholder')).toBe(en.oauthGuide.refreshPlaceholder)
    await w.get('textarea[required]').setValue('test-only-bps-refresh')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.createAccount).toHaveBeenCalledWith(expect.objectContaining({ platform: 'openai', authorization_mode: 'bps', authorization_action: 'attach', target_account_id: 42, method: 'refresh_token', token: 'test-only-bps-refresh' }))
    expect((w.get('textarea[required]').element as HTMLTextAreaElement).value).toBe('')
    expect(mocks.startOAuth).not.toHaveBeenCalled()
    w.unmount()
  })

  it('discards a native lease and callback when switching to BPS', async () => {
    const w = render({ admin: false }, policy, existing, { reauthorize: true })
    await flushPromises()
    await w.get('form').trigger('submit')
    await flushPromises()
    await w.get('textarea[required]').setValue('old-native-code')
    await choose(w, en.authorization.mode, 'bps')
    expect(w.find('a').exists()).toBe(false)
    expect(w.find('textarea[required]').exists()).toBe(false)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.startOAuth).toHaveBeenLastCalledWith('openai', expect.objectContaining({ authorization_mode: 'bps', authorization_action: 'attach' }), { admin: false })
    expect(mocks.completeOAuth).not.toHaveBeenCalled()
    w.unmount()
  })

  it('ignores a late lease after mode permission is revoked and blocks read-only writes', async () => {
    let resolve!: (value: unknown) => void
    mocks.startOAuth.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const w = render({ admin: false }, policy, existing, { reauthorize: true })
    await flushPromises()
    await w.get('form').trigger('submit')
    await w.setProps({ vendor: { ...policy, openai_authorization_modes: [] } })
    resolve({ auth_url: 'https://oauth.example/stale', session_id: 'stale', expires_at: '' })
    await flushPromises()
    expect(w.find('a').exists()).toBe(false)
    await w.setProps({ scope: { admin: false, readOnly: true, vendorId: 8 }, vendor: policy })
    await w.get('form').trigger('submit')
    expect(mocks.startOAuth).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('uses the account policy revocation even when the cached vendor policy still permits BPS', async () => {
    const w = render({ admin: false }, policy, existing, { reauthorize: true, authorizationMode: 'bps' })
    await flushPromises()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.startOAuth).toHaveBeenCalledTimes(1)
    await w.setProps({ account: { ...existing, authorizations: [
      { mode: 'native', authorized: true, enabled: true, status: 'active', can_refresh: true },
      { mode: 'bps', authorized: true, enabled: true, status: 'policy_revoked', can_refresh: false }
    ] } })
    expect(w.find('a').exists()).toBe(false)
    expect(w.find('input[value="bps"]').exists()).toBe(false)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.startOAuth).toHaveBeenLastCalledWith('openai', expect.objectContaining({ authorization_mode: 'native', authorization_action: 'reauthorize' }), { admin: false })
    expect(mocks.completeOAuth).not.toHaveBeenCalled()
    w.unmount()
  })
})

it('keeps edited metadata after OAuth and retries only its save when authorization is already committed', async () => {
  mocks.completeOAuth.mockResolvedValue({ id: 42 })
  mocks.updateAccount
    .mockRejectedValueOnce(new Error('Metadata save interrupted'))
    .mockResolvedValue({ id: 42 })
  const w = render()
  await flushPromises()
  await w.find('[data-testid="account-name"]').setValue('Original')
  await choose(w, en.proxy, '1')
  await w.find('form').trigger('submit')
  await flushPromises()
  await w.find('[data-testid="account-name"]').setValue('Final name')
  await w.find('textarea').setValue('Final notes')
  await w.findAll('textarea')[1].setValue('code#state')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(w.text()).toContain(en.authorizationSaved)
  expect(mocks.updateAccount).toHaveBeenLastCalledWith(
    42,
    expect.objectContaining({
      name: 'Final name',
      vendor_notes: 'Final notes'
    })
  )
  expect(w.findAll('textarea')).toHaveLength(1)
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(mocks.completeOAuth).toHaveBeenCalledTimes(1)
  expect(mocks.updateAccount).toHaveBeenCalledTimes(2)
  expect(w.emitted('saved')).toHaveLength(1)
  w.unmount()
})

it('uses catalog platform names and human authorization labels without changing method wire values', async () => {
  const w = render()
  await flushPromises()
  expect(w.find('select').exists()).toBe(false)
  const radioOptions = (label: string) =>
    w
      .findAll('fieldset')
      .find((f) => f.find('legend').exists() && f.find('legend').text() === label)!
      .findAll('input[type=radio]')
      .map((input) => ({ value: input.attributes('value'), label: input.attributes('aria-label') }))
  expect(radioOptions(en.platform)).toEqual([
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'openai', label: 'OpenAI' }
  ])
  expect(radioOptions(en.method)).toEqual([
    { value: 'oauth', label: 'OAuth' },
    { value: 'setup-token', label: 'OAuth Token' },
    { value: 'manual', label: 'Setup Token' }
  ])
  await w.get('input[value="existing"]').setValue(true)
  expect(selector(w, en.proxy).props('options')).toContainEqual({
    value: '1',
    label: 'One · HTTP · one.test:80'
  })
  await choose(w, en.method, 'setup-token')
  await choose(w, en.proxy, '1')
  await w.find('[data-testid="account-name"]').setValue('OAuth Token account')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(mocks.startOAuth).toHaveBeenLastCalledWith(
    'anthropic',
    expect.objectContaining({ method: 'setup-token', proxy_id: 1 }),
    { admin: false }
  )
  await choose(w, en.platform, 'openai')
  expect(radioOptions(en.method)).toEqual([
    { value: 'oauth', label: 'OAuth' },
    { value: 'manual', label: 'Refresh Token' }
  ])
  await choose(w, en.method, 'manual')
  expect(w.text()).toContain('Refresh Token')
  expect(w.find('a').exists()).toBe(false)
  w.unmount()
})

it('binds the fixed dialog footer submit to the onboarding form and gates OAuth completion', async () => {
  const w = render()
  await flushPromises()
  const submit = w.get('footer button[type=submit]')
  expect(submit.attributes('form')).toBe(w.get('form').attributes('id'))
  expect(submit.attributes('disabled')).toBeDefined()
  expect(w.get('textarea[disabled]').exists()).toBe(true)
  expect(mocks.startOAuth).not.toHaveBeenCalled()
  w.unmount()
})


it('keeps authorization parameters read-only and hides the statistical multiplier', async () => {
  const account = {
    id: 42, name: 'Existing', platform: 'anthropic', type: 'oauth',
    proxy_id: 1, group_ids: [5], groups: vendor.groups,
    concurrency: 12, priority: 2, rate_multiplier: 1, status: 'active',
    archived: false, archived_at: null, schedulable: true, created_at: ''
  } as VendorAccount
  const supplier = render({ admin: false }, vendor, account)
  await flushPromises()
  expect(supplier.find('[data-testid=account-concurrency]').exists()).toBe(false)
  expect(supplier.text()).toContain('12')
  expect(supplier.text()).not.toContain(en.multiplier)
  await supplier.get('form').trigger('submit')
  await flushPromises()
  const body = mocks.updateAccount.mock.lastCall?.[1]
  expect(body).not.toHaveProperty('concurrency')
  expect(body).not.toHaveProperty('priority')
  supplier.unmount()
})


it('automatically chooses the available proxy with the fewest bound accounts', async () => {
  mocks.proxies.mockResolvedValue({items:[{id:1,name:'Busy',status:'active',account_count:8},{id:2,name:'Free',status:'active',account_count:0},{id:3,name:'Expired',status:'inactive',account_count:0}],total:3})
  const w=render(); await flushPromises()
  expect((w.get('input[value="auto"]').element as HTMLInputElement).checked).toBe(true)
  await w.get('form').trigger('submit'); await flushPromises()
  expect(mocks.startOAuth).toHaveBeenCalledWith('anthropic',expect.objectContaining({proxy_id:2,direct:false}),{admin:false})
  w.unmount()
})
it.each([['http://user:pass@proxy.example:8080','pass'],['proxy.example:8080:user:pass','pass'],['http://user:p%40ss@proxy.example:8080','p@ss']])('creates an inline proxy before starting authorization: %s',async (address,password)=>{
  mocks.createProxy.mockResolvedValue({id:20,name:'proxy.example:8080',status:'active',protocol:'http',host:'proxy.example',port:8080})
  const w=render();await flushPromises();await w.get('input[value="new"]').setValue(true)
  await w.get('input[autocomplete="off"]').setValue(address)
  await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.createProxy).toHaveBeenCalledWith({name:'proxy.example:8080',host:'proxy.example',port:8080,protocol:'http',username:'user',password},expect.any(String))
  expect(mocks.startOAuth).toHaveBeenCalledWith('anthropic',expect.objectContaining({proxy_id:20,direct:false}),{admin:false})
  expect(mocks.createProxy.mock.invocationCallOrder[0]).toBeLessThan(mocks.startOAuth.mock.invocationCallOrder[0])
  expect(w.find('input[autocomplete="off"]').exists()).toBe(false)
  w.unmount()
})
it('keeps inline proxy failures private and retries with the same idempotency key',async()=>{
  mocks.createProxy.mockRejectedValue({message:'password-secret'})
  const w=render();await flushPromises();await w.get('input[value="new"]').setValue(true)
  await w.get('input[autocomplete="off"]').setValue('host.example:8080:user:password-secret')
  await w.get('form').trigger('submit');await flushPromises();await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.createProxy.mock.calls[0][1]).toBe(mocks.createProxy.mock.calls[1][1])
  expect(mocks.startOAuth).not.toHaveBeenCalled();expect(w.text()).not.toContain('password-secret')
  w.unmount()
})
it('does not silently replace missing proxies with direct access and explains Setup Token',async()=>{
  mocks.proxies.mockResolvedValue({items:[],total:0})
  const w=render({admin:false},{...vendor,allow_direct:true});await flushPromises()
  await w.get('form').trigger('submit');await flushPromises();expect(mocks.startOAuth).not.toHaveBeenCalled()
  await choose(w,en.method,'manual')
  expect(w.get('[data-setup-token-help]').text()).toContain('claude setup-token')
  expect(w.get('[data-setup-token-help]').text()).toContain('CLAUDE_CODE_OAUTH_TOKEN')
  expect(w.get('[data-setup-token-help]').text()).toContain(en.setupTokenValidity)
  await choose(w,en.proxy,'direct');await w.findAll('textarea')[1].setValue('setup-only')
  await w.get('form').trigger('submit');await flushPromises()
  expect(mocks.createAccount).toHaveBeenCalledWith(expect.objectContaining({direct:true,proxy_id:null,method:'setup-token-manual',token:'setup-only'}))
  w.unmount()
})

it('shows an explicitly granted Composite group beside compatible native groups', async () => {
  const wrapper = render({ admin: false }, { ...vendor, groups: [...vendor.groups!, { id: 16, name: 'Coding composite', platform: 'composite' }] })
  await flushPromises()
  expect(wrapper.text()).toContain('Coding composite')
  expect(wrapper.text()).toContain('Claude')
  wrapper.unmount()
})
