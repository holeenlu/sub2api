import Select from '@/components/common/Select.vue'
import { createPinia } from 'pinia'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProxyImport from '../components/ProxyImport.vue'
import AccountImport from '../components/AccountImport.vue'
import en from '@/i18n/locales/en/vendor'
import nativeEn from '@/i18n/locales/en'
const accountBundle = {
  type: 'sub2api-data',
  version: 1,
  accounts: [
    {
      name: 'Imported account',
      platform: 'anthropic',
      type: 'oauth',
      credentials: { refresh_token: 'secret-value' },
    },
  ],
}
const importVendor = {
  id: 8,
  allow_direct: true,
  platforms: ['anthropic', 'openai'],
  groups: [
    { id: 5, name: 'Claude group', platform: 'anthropic' },
    { id: 6, name: 'OpenAI default', platform: 'openai' },
    { id: 7, name: 'OpenAI custom', platform: 'openai' },
  ],
}
const selector = (w: VueWrapper, label: string) =>
  w.findAllComponents(Select).find((s) => s.props('ariaLabel') === label)!
async function choose(w: VueWrapper, label: string, value: string) {
  const control = selector(w, label)
  expect(control).toBeDefined()
  await control.find('button').trigger('click')
  const option = (
    control.props('options') as { value: string; label: string }[]
  ).find((o) => o.value === value)!
  expect(option).toBeDefined()
  await control
    .findAll('[role=option]')
    .find((o) => o.text() === option.label)!
    .trigger('click')
  await flushPromises()
}
const mocks = vi.hoisted(() => ({
  proxyImport: vi.fn(),
  importAccounts: vi.fn(),
  proxies: vi.fn(),
}))
vi.mock('../api', () => ({ resourceAPI: () => mocks, vendorAPI: mocks }))
const global = {
  plugins: [
    createPinia(),
    createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: nativeEn },
    }),
  ],
  stubs: { teleport: true, BaseDialog: { template: '<div><slot /></div>' } },
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.proxies.mockResolvedValue({ items: [], total: 0 })
})
describe('supplier import privacy and preview binding', () => {
  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'accepts native admin exports and automatically selects bundled proxies in %j',
    async (scope) => {
      mocks.importAccounts.mockResolvedValue({
        preview_id: 'native-bundled-proxy-preview',
        expires_at: '2099-01-01T00:00:00Z',
        rows: [{ index: 0, status: 'create', proxy_id: null, direct: false,
          proxy: { name: 'Imported proxy', endpoint: 'http://public.invalid:8080',
            username: 'NEVER-RENDER-USER', password: 'NEVER-RENDER-PASS' } }],
      })
      const wrapper = mount(AccountImport, { props: { scope, vendor: importVendor as any }, global })
      await flushPromises()
      const native = {
        type: 'sub2api-data', version: 2,
        accounts: [{ ...accountBundle.accounts[0], proxy_key: 'http|public.invalid|8080|SECRET-USER|SECRET-PASS' }],
        proxies: [{ proxy_key: 'http|public.invalid|8080|SECRET-USER|SECRET-PASS',
          protocol: 'http', host: 'public.invalid', port: 8080, username: 'SECRET-USER', password: 'SECRET-PASS' }],
      }
      await wrapper.find('textarea').setValue(JSON.stringify(native))
      await wrapper.find('input[type=checkbox]').setValue(true)
      expect(selector(wrapper, en.proxy).props('modelValue')).toBe('import')
      await wrapper.findAll('button').find((b) => b.text() === en.preview)!.trigger('click')
      await flushPromises()
      expect(mocks.importAccounts).toHaveBeenCalledWith(expect.objectContaining({
        data: native, proxy_id: null, direct: false,
        mappings: [{ index: 0, group_ids: [5], proxy_id: null, direct: false, mode: 'skip' }],
      }), true, scope)
      expect(wrapper.find('table').text()).toContain('Imported proxy')
      expect(wrapper.find('table').text()).toContain('http://public.invalid:8080')
      expect(wrapper.text()).not.toMatch(/SECRET-USER|SECRET-PASS|NEVER-RENDER/)
      await choose(wrapper, en.proxy, 'direct')
      await wrapper.findAll('button').find((b) => b.text() === en.preview)!.trigger('click')
      await flushPromises()
      expect(mocks.importAccounts).toHaveBeenLastCalledWith(expect.objectContaining({
        mappings: [expect.objectContaining({ proxy_id: null, direct: true })],
      }), true, scope)
      wrapper.unmount()
    },
  )

  it.each([
    { siwc_authorization: { credentials: { refresh_token: 'PRIVATE-RETIRED-TOKEN' } } },
    { extra: { openai_siwc: true } },
  ])('rejects retired grants without sending or displaying their credentials (%j)', async (retiredFields) => {
    const wrapper = mount(AccountImport, { props: { vendor: importVendor as any }, global })
    await flushPromises()
    await wrapper.find('textarea').setValue(JSON.stringify({
      ...accountBundle, version: 2, accounts: [{ ...accountBundle.accounts[0], ...retiredFields }]
    }))
    await choose(wrapper, en.proxy, 'direct')
    await wrapper.findAll('button').find((b) => b.text() === en.preview)!.trigger('click')
    await flushPromises()
    expect(mocks.importAccounts).not.toHaveBeenCalled()
    expect(wrapper.find('[role=alert]').text()).toBe(en.importRetiredAuthorization)
    expect(wrapper.text()).not.toContain('PRIVATE-RETIRED-TOKEN')
    wrapper.unmount()
  })

  it('does not echo malformed credential input in a JSON parser error', async () => {
    const wrapper = mount(AccountImport, {
      props: {
        vendor: {
          allow_direct: true,
          groups: [{ id: 5, name: 'Group' }],
        } as any,
      },
      global,
    })
    await flushPromises()
    await wrapper.find('textarea').setValue('DO-NOT-ECHO-SECRET{invalid')
    await wrapper.find('input[type=checkbox]').setValue(true)
    await choose(wrapper, en.proxy, 'direct')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === en.preview)!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('[role=alert]').text()).toBe(en.invalidJSON)
    expect(wrapper.find('[role=alert]').text()).not.toContain('DO-NOT-ECHO')
    expect(mocks.importAccounts).not.toHaveBeenCalled()
    await wrapper.find('textarea').setValue('{}')
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders row outcomes without echoing parsed proxy credentials and invalidates changed previews', async () => {
    mocks.proxyImport.mockResolvedValue({
      input_digest: 'digest',
      valid_count: 1,
      rows: [
        {
          line: 1,
          status: 'valid',
          code: 'READY',
          proxy: {
            host: 'private.test',
            username: 'DO-NOT-RENDER-USER',
            password: 'DO-NOT-RENDER-PASS',
          },
        },
      ],
    })
    const wrapper = mount(ProxyImport, {
      props: { scope: { admin: false } },
      global,
    })
    await wrapper
      .find('textarea')
      .setValue('http://alice:secret@example.test:8080')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === en.preview)!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('table').text()).toContain('READY')
    expect(wrapper.find('table').html()).not.toMatch(
      /DO-NOT-RENDER|password|private.test/,
    )
    expect(
      wrapper
        .findAll('button')
        .find((b) => b.text() === en.confirm)!
        .attributes('disabled'),
    ).toBeUndefined()
    await wrapper
      .find('textarea')
      .setValue('http://other:changed@example.test:8080')
    expect(
      wrapper
        .findAll('button')
        .find((b) => b.text() === en.confirm)!
        .attributes('disabled'),
    ).toBeDefined()
    wrapper.unmount()
  })
  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'confirms only the receipt in supplier scope %j',
    async (scope) => {
      mocks.importAccounts.mockResolvedValue({
        preview_id: 'scoped-preview',
        expires_at: '2099-01-01T00:00:00Z',
        rows: [{ index: 1, status: 'ready', message: 'Verified' }],
      })
      const wrapper = mount(AccountImport, {
        props: {
          scope,
          vendor: {
            allow_direct: true,
            groups: [{ id: 5, name: 'Group' }],
          } as any,
        },
        global,
      })
      await flushPromises()
      await wrapper.find('textarea').setValue(JSON.stringify(accountBundle))
      await wrapper.find('input[type=checkbox]').setValue(true)
      await choose(wrapper, en.proxy, 'direct')
      await wrapper
        .findAll('button')
        .find((b) => b.text() === en.preview)!
        .trigger('click')
      await flushPromises()
      expect(wrapper.find('table').text()).not.toContain('secret-value')
      await wrapper
        .findAll('button')
        .find((b) => b.text() === en.confirm)!
        .trigger('click')
      await flushPromises()
      expect(mocks.importAccounts).toHaveBeenLastCalledWith(
        { preview_id: 'scoped-preview' },
        false,
        scope,
      )
      wrapper.unmount()
    },
  )

  it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
    'maps mixed platforms by row, displays final safe bindings and invalidates changed mappings in %j',
    async (scope) => {
      mocks.proxies.mockResolvedValue({
        items: [
          {
            id: 1,
            name: 'Owned egress',
            protocol: 'socks5h',
            host: 'owned.test',
            port: 1080,
            status: 'active',
            username: 'NEVER-RENDER-USER',
            password: 'NEVER-RENDER-PASS',
          },
        ],
        total: 1,
      })
      mocks.importAccounts.mockResolvedValue({
        preview_id: 'mixed-platform-preview',
        expires_at: '2099-01-01T00:00:00Z',
        rows: [
          {
            index: 0,
            status: 'create',
            name: 'Verified Claude',
            platform: 'anthropic',
            group_ids: [5],
            proxy_id: 1,
            direct: false,
            credentials: { refresh_token: 'NEVER-RENDER-RESPONSE-TOKEN' },
          },
          {
            index: 1,
            status: 'update',
            account_id: 45,
            name: 'Verified OpenAI',
            platform: 'openai',
            group_ids: [7],
            proxy_id: null,
            direct: true,
          },
        ],
      })
      const wrapper = mount(AccountImport, {
        props: { vendor: importVendor as any, scope },
        global,
      })
      await flushPromises()
      const imported = {
        ...accountBundle,
        accounts: [
          {
            ...accountBundle.accounts[0],
            vendor_id: 999,
            group_ids: [999],
            proxy_id: 999,
          },
          {
            name: 'Imported OpenAI',
            platform: 'openai',
            type: 'oauth',
            credentials: { refresh_token: 'NEVER-RENDER-INPUT-TOKEN' },
            vendor_id: 999,
            group_ids: [999],
            proxy_id: 999,
          },
        ],
      }
      await wrapper.find('textarea').setValue(JSON.stringify(imported))
      await wrapper
        .get('[data-import-defaults] input[value="5"]')
        .setValue(true)
      await wrapper
        .get('[data-import-defaults] input[value="6"]')
        .setValue(true)
      await choose(wrapper, en.proxy, '1')
      const first = wrapper.get('[data-import-row="0"]')
      const second = wrapper.get('[data-import-row="1"]')
      expect(first.get('[data-import-effective]').text()).toContain(
        'Claude group · #5',
      )
      expect(first.get('[data-import-effective]').text()).not.toContain(
        'OpenAI default',
      )
      expect(second.get('[data-import-effective]').text()).toContain(
        'OpenAI default · #6',
      )
      expect(first.text()).toContain('SOCKS5H · socks5h://owned.test:1080')
      expect(second.text()).toContain(en.importUseDefaults)
      await second.get('input[type=checkbox]').setValue(true)
      expect(second.find('input[value="5"]').exists()).toBe(false)
      await second.get('input[value="6"]').setValue(false)
      await second.get('input[value="7"]').setValue(true)
      await choose(wrapper, en.importRowProxy.replace('{line}', '2'), 'direct')
      await choose(wrapper, en.importRowMode.replace('{line}', '2'), 'update')
      await wrapper
        .findAll('button')
        .find((b) => b.text() === en.preview)!
        .trigger('click')
      await flushPromises()
      expect(mocks.importAccounts).toHaveBeenCalledWith(
        {
          data: imported,
          mode: 'skip',
          group_ids: [5, 6],
          proxy_id: 1,
          direct: false,
          mappings: [
            {
              index: 0,
              group_ids: [5],
              proxy_id: 1,
              direct: false,
              mode: 'skip',
            },
            {
              index: 1,
              group_ids: [7],
              proxy_id: null,
              direct: true,
              mode: 'update',
            },
          ],
        },
        true,
        scope,
      )
      const results = wrapper.get('[data-import-result]')
      expect(results.text()).toContain('Verified Claude')
      expect(results.text()).toContain('Verified OpenAI')
      expect(results.text()).toContain('#45')
      expect(results.text()).toContain('OpenAI custom · #7')
      expect(results.text()).toContain('Owned egress · #1')
      expect(results.text()).toContain(en.direct)
      expect(results.html()).not.toMatch(
        /NEVER-RENDER|secret-value|credentials|999/,
      )
      expect(first.html()).not.toMatch(
        /NEVER-RENDER|secret-value|credentials|999/,
      )
      expect(
        wrapper
          .findAll('button')
          .find((b) => b.text() === en.confirm)!
          .attributes('disabled'),
      ).toBeUndefined()
      // A per-row edit must invalidate the server receipt, even if defaults did not change.
      await second.get('input[value="6"]').setValue(true)
      expect(wrapper.find('[data-import-result]').exists()).toBe(false)
      expect(
        wrapper
          .findAll('button')
          .find((b) => b.text() === en.confirm)!
          .attributes('disabled'),
      ).toBeDefined()
      expect(mocks.importAccounts).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    },
  )

  it.each([
    ['failed', en.failed],
    ['skipped', en.skip],
  ])(
    'localizes %s rows without inventing direct egress or allowing an empty import',
    async (status, label) => {
      mocks.importAccounts.mockResolvedValue({
        preview_id: 'empty-preview',
        rows: [
          {
            index: 0,
            status,
            name: 'Verified row',
            proxy_id: null,
            direct: false,
          },
        ],
      })
      const wrapper = mount(AccountImport, {
        props: { vendor: importVendor as any },
        global,
      })
      await flushPromises()
      await wrapper.find('textarea').setValue(JSON.stringify(accountBundle))
      await wrapper
        .get('[data-import-defaults] input[value="5"]')
        .setValue(true)
      await choose(wrapper, en.proxy, 'direct')
      await wrapper
        .findAll('button')
        .find((b) => b.text() === en.preview)!
        .trigger('click')
      await flushPromises()
      const row = wrapper.get('[data-import-result] tbody tr')
      expect(row.findAll('td')[3].text()).toBe(en.unknown)
      expect(row.findAll('td')[4].text()).toBe(label)
      expect(row.text()).not.toContain(en.direct)
      expect(
        wrapper
          .findAll('button')
          .find((b) => b.text() === en.confirm)!
          .attributes('disabled'),
      ).toBeDefined()
      wrapper.unmount()
    },
  )

  it('clears credentials and mapping state on supplier changes and rejects a late receipt', async () => {
    let finish!: (value: unknown) => void
    mocks.importAccounts.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const wrapper = mount(AccountImport, {
      props: {
        vendor: importVendor as any,
        scope: { admin: true, vendorId: 8 },
      },
      global,
    })
    await flushPromises()
    await wrapper.find('textarea').setValue(JSON.stringify(accountBundle))
    await wrapper.get('[data-import-defaults] input[value="5"]').setValue(true)
    await choose(wrapper, en.proxy, 'direct')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === en.preview)!
      .trigger('click')
    expect(mocks.importAccounts).toHaveBeenCalledTimes(1)
    await wrapper.setProps({
      scope: { admin: true, vendorId: 9 },
      vendor: { ...importVendor, id: 9 } as any,
    })
    finish({
      preview_id: 'old-owner-receipt',
      rows: [{ index: 0, status: 'create', name: 'Other owner row' }],
    })
    await flushPromises()
    expect(
      (wrapper.find('textarea').element as HTMLTextAreaElement).value,
    ).toBe('')
    expect(wrapper.find('[data-import-row]').exists()).toBe(false)
    expect(wrapper.find('[data-import-result]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Other owner row')
    expect(selector(wrapper, en.proxy).props('modelValue')).toBe('')
    expect(
      wrapper
        .findAll('button')
        .find((b) => b.text() === en.confirm)!
        .attributes('disabled'),
    ).toBeDefined()
    wrapper.unmount()
  })
})

it('keeps quick confirmation bound to the same digest/key through an ambiguous failure and prevents overlapping writes', async () => {
  let resolvePreview!: (value: object) => void
  let rejectCommit!: (reason: Error) => void
  mocks.proxyImport.mockReset()
  mocks.proxyImport.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    .mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectCommit = reject }))
    .mockResolvedValueOnce({ valid_count: 1, rows: [{ status: 'created', proxy_id: 7 }] })
  const w = mount(ProxyImport, { props: { scope: { admin: false }, embedded: true }, global })
  const input = 'socks5h://user: p%40ss:word @[2606:4700:4700::1111]:1080'
  await w.get('textarea').setValue(input)
  const form = w.get('#vendor-proxy-import-form')
  await form.trigger('submit')
  await form.trigger('submit')
  expect(mocks.proxyImport).toHaveBeenCalledTimes(1)
  resolvePreview({ input_digest: 'immutable-digest', valid_count: 1, rows: [] })
  await flushPromises()
  await form.trigger('submit')
  expect(mocks.proxyImport).toHaveBeenCalledTimes(2)
  const confirmation = mocks.proxyImport.mock.calls[1]
  expect(confirmation).toEqual([{ format: 'quick', content: input, input_digest: 'immutable-digest' }, false, expect.any(String)])
  rejectCommit(new Error('user:DO-NOT-ECHO-PASSWORD'))
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe(nativeEn.admin.proxies.failedToImport)
  expect(w.text()).not.toContain('DO-NOT-ECHO')
  await form.trigger('submit')
  await flushPromises()
  expect(mocks.proxyImport.mock.calls[2]).toEqual(confirmation)
  expect(w.emitted('saved')).toEqual([[[7]]])
  expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('')
  w.unmount()
})
it('never echoes arbitrary import row messages or codes', async () => {
  mocks.proxyImport.mockReset().mockResolvedValue({ input_digest: 'digest', valid_count: 0,
    rows: [{ line: 1, status: 'invalid', code: 'SECRET-PASSWORD', message: 'http://user:SECRET-PASSWORD@host:80' }] })
  const w = mount(ProxyImport, { props: { scope: { admin: false } }, global })
  await w.get('textarea').setValue('http://example.test:8080')
  await w.findAll('button').find(button => button.text() === en.preview)!.trigger('click')
  await flushPromises()
  expect(w.get('table').text()).not.toContain('SECRET-PASSWORD')
  expect(w.get('table').text()).toContain(en.invalidRows)
  w.unmount()
})

it('invalidates a proxy preview when its direct vendor scope changes', async () => {
  mocks.proxyImport.mockResolvedValue({ input_digest: 'scope-seven', valid_count: 1, rows: [] })
  const wrapper = mount(ProxyImport, { props: { scope: { admin: true, vendorId: 7 } }, global })
  await wrapper.get('textarea').setValue('http://user:secret@example.test:8080')
  await wrapper.findAll('button').find(b => b.text() === en.preview)!.trigger('click')
  await flushPromises()
  expect(wrapper.findAll('button').find(b => b.text() === en.confirm)!.attributes('disabled')).toBeUndefined()
  await wrapper.setProps({ scope: { admin: true, vendorId: 8 } })
  expect(wrapper.findAll('button').find(b => b.text() === en.confirm)!.attributes('disabled')).toBeDefined()
  wrapper.unmount()
})
it('renders only safe fallback error codes and blocks read-only imports', async () => {
  mocks.proxyImport.mockResolvedValue({ valid_count: 0, invalid_count: 1, rows: [
    { line: 1, status: 'invalid', code: 'PROXY_NOT_FOUND', message: 'SECRET_PASSWORD', proxy: { username: 'SECRET_USER', password: 'SECRET_PASSWORD' } }
  ] })
  const wrapper = mount(ProxyImport, { props: { scope: { admin: false } }, global })
  await wrapper.get('textarea').setValue('http://user:secret@example.test:8080')
  await wrapper.findAll('button').find(b => b.text() === en.preview)!.trigger('click')
  await flushPromises()
  expect(wrapper.get('table').text()).toContain('PROXY_NOT_FOUND')
  expect(wrapper.text()).not.toContain('SECRET')
  await wrapper.setProps({ scope: { admin: false, readOnly: true } })
  await wrapper.findAll('button').find(b => b.text() === en.preview)!.trigger('click')
  expect(mocks.proxyImport).toHaveBeenCalledTimes(1)
  wrapper.unmount()
})
