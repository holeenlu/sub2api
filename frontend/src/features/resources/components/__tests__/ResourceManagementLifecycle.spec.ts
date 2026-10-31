import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref, type Component } from 'vue'
import { managementProvide } from './managementTestServices'
import { editorTestServices } from '../../__tests__/editorTestServices'
import AccountImportDataModal from '../AccountImportDataModal.vue'
import ProxyImportDataModal from '../ProxyImportDataModal.vue'
import TLSFingerprintProfilesModal from '../TLSFingerprintProfilesModal.vue'
import ErrorPassthroughRulesModal from '../ErrorPassthroughRulesModal.vue'
import AccountActionMenu from '../AccountActionMenu.vue'
import { accountFixture } from '../../__tests__/accountFixture'

const api = vi.hoisted(() => ({
  tls: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
  rules: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn(), toggleEnabled: vi.fn() },
  importAccount: vi.fn(), importProxy: vi.fn()
}))
vi.mock('@/features/resources/tlsFingerprintProfile', () => ({ default: api.tls }))
vi.mock('@/features/resources/errorPassthrough', () => ({ default: api.rules }))
vi.mock('@/features/resources/accounts', () => ({ default: { importData: api.importAccount } }))
vi.mock('@/features/resources/proxies', () => ({ default: { importData: api.importProxy } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const Dialog = defineComponent({ props: ['show'], emits: ['close'], template: "<section v-if=\"show\"><button data-action=\"dialog-close\" @click=\"$emit('close')\">close</button><slot /><slot name=\"footer\" /></section>" })
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function harness(component: Component, props: Record<string, unknown> = { show: true }) {
  const actor = ref('operator-a')
  const services = editorTestServices()
  services.scopeKey = () => actor.value
  const wrapper = mount(component, { props, global: { provide: managementProvide({}, services), stubs: {
    BaseDialog: Dialog, ConfirmDialog: true, Icon: true, ModelWhitelistSelector: true
  } } })
  return { wrapper, actor, services }
}
async function chooseFiles(wrapper: ReturnType<typeof mount>, values: File[]) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: values })
  await input.trigger('change')
}
function file(name: string, value: unknown, read?: () => Promise<string>) {
  const source = new File([], name, { type: 'application/json' })
  Object.defineProperty(source, 'text', { value: read || (() => Promise.resolve(JSON.stringify(value))) })
  return source
}
const imported = () => ({ account_created: 1, account_failed: 0, proxy_created: 1, proxy_reused: 0, proxy_failed: 0, errors: [] })
const bundle = () => ({ type: 'sub2api-data', version: 2, proxies: [], accounts: [{ name: 'resource', platform: 'future-provider', credentials: { opaque: 'synthetic-test-value' }, extra: { future: true } }] })
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
beforeEach(() => {
  vi.resetAllMocks()
  api.tls.list.mockResolvedValue([]); api.rules.list.mockResolvedValue([])
  api.importAccount.mockResolvedValue(imported()); api.importProxy.mockResolvedValue(imported())
})

describe('resource management lifetime', () => {
  for (const [name, component, apiModule] of [['TLS', TLSFingerprintProfilesModal, api.tls], ['error rules', ErrorPassthroughRulesModal, api.rules]] as const) {
    it.each(['actor', 'close', 'unmount'] as const)(name + ' ignores late list results after %s', async change => {
      const pending = deferred<unknown[]>()
      apiModule.list.mockReturnValueOnce(pending.promise)
      const h = harness(component)
      if (change === 'actor') h.actor.value = 'operator-b'
      if (change === 'close') await h.wrapper.setProps({ show: false })
      if (change === 'unmount') h.wrapper.unmount()
      await flushPromises()
      const current = change === 'unmount' ? '' : h.wrapper.html()
      pending.resolve([{ id: 7, name: 'obsolete actor data' }])
      await flushPromises()
      if (change !== 'unmount') expect(h.wrapper.html()).toBe(current)
      expect(h.services.notices.showError).not.toHaveBeenCalled()
    })
  }

  it('discards a TLS save completion when its actor changes', async () => {
    const pending = deferred<unknown>()
    api.tls.create.mockReturnValueOnce(pending.promise)
    const h = harness(TLSFingerprintProfilesModal); await flushPromises()
    await h.wrapper.findAll('button').find(button => button.text() === 'admin.tlsFingerprintProfiles.createProfile')!.trigger('click')
    await h.wrapper.get('form input[type="text"]').setValue('New profile')
    await h.wrapper.get('form').trigger('submit')
    expect(api.tls.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'New profile', cipher_suites: [] }))
    h.actor.value = 'operator-b'; await flushPromises()
    pending.resolve({ id: 9 }); await flushPromises()
    expect(h.services.notices.showSuccess).not.toHaveBeenCalled()
    expect(h.wrapper.find('form').exists()).toBe(false)
  })

  for (const [name, component, command] of [['account', AccountImportDataModal, api.importAccount], ['proxy', ProxyImportDataModal, api.importProxy]] as const) {
    it.each(['actor', 'close', 'unmount'] as const)(name + ' stops before import after pending file read and %s', async change => {
      const pending = deferred<string>()
      const h = harness(component)
      await chooseFiles(h.wrapper, [file('resources.json', {}, () => pending.promise)])
      await h.wrapper.get('form').trigger('submit')
      if (change === 'actor') h.actor.value = 'operator-b'
      if (change === 'close') await h.wrapper.setProps({ show: false })
      if (change === 'unmount') h.wrapper.unmount()
      pending.resolve(JSON.stringify(bundle())); await flushPromises()
      expect(command).not.toHaveBeenCalled()
      expect(h.services.notices.showError).not.toHaveBeenCalled()
      expect(h.wrapper.emitted('imported')).toBeUndefined()
    })

    it(name + ' discards an already-sent import completion for the old actor', async () => {
      const pending = deferred<ReturnType<typeof imported>>()
      command.mockReturnValueOnce(pending.promise)
      const h = harness(component)
      await chooseFiles(h.wrapper, [file('resources.json', bundle())])
      await h.wrapper.get('form').trigger('submit'); await flushPromises()
      expect(command).toHaveBeenCalledTimes(1)
      h.actor.value = 'operator-b'; await flushPromises()
      pending.resolve(imported()); await flushPromises()
      expect(h.services.notices.showSuccess).not.toHaveBeenCalled()
      expect(h.wrapper.emitted('imported')).toBeUndefined()
    })
  }

  it('preserves a single account bundle and skips implicit default-group binding', async () => {
    const payload = { ...bundle(), unknown_extension: { retain: true } }
    const h = harness(AccountImportDataModal)
    await chooseFiles(h.wrapper, [file('accounts.json', payload)])
    await h.wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.importAccount).toHaveBeenCalledWith({ data: payload, skip_default_group_bind: true })
    expect(h.wrapper.emitted('imported')).toHaveLength(1)
    expect(api.importProxy).not.toHaveBeenCalled()
  })

  it('merges valid account files without dropping skipped-shadow counts or resources', async () => {
    const first = { ...bundle(), version: 1, skipped_shadows: 2 }
    const second = { ...bundle(), type: 'sub2api-bundle', skipped_shadows: 3 }
    const h = harness(AccountImportDataModal)
    await chooseFiles(h.wrapper, [file('a.json', first), file('b.json', second)])
    await h.wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.importAccount).toHaveBeenCalledWith({ data: expect.objectContaining({ version: 2, skipped_shadows: 5, accounts: [...first.accounts, ...second.accounts] }), skip_default_group_bind: true })
  })

  it.each([{ type: 'unknown' }, { version: 99 }, { accounts: null }])('rejects invalid account headers before sending a merged payload: %j', async invalid => {
    const h = harness(AccountImportDataModal)
    await chooseFiles(h.wrapper, [file('valid.json', bundle()), file('invalid.json', { ...bundle(), ...invalid })])
    await h.wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.importAccount).not.toHaveBeenCalled()
    expect(h.services.notices.showError).toHaveBeenCalledWith('admin.accounts.dataImportInvalidFile')
  })

  it('keeps proxy import on its own command and preserves the complete wire payload', async () => {
    const payload = { type: 'sub2api-data', version: 2, proxies: [{ name: 'proxy', protocol: 'future', unknown: true }] }
    const h = harness(ProxyImportDataModal)
    await chooseFiles(h.wrapper, [file('proxies.json', payload)])
    await h.wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.importProxy).toHaveBeenCalledWith({ data: payload })
    expect(api.importAccount).not.toHaveBeenCalled()
    expect(h.wrapper.emitted('imported')).toHaveLength(1)
  })

  it.each([[AccountImportDataModal, api.importAccount], [ProxyImportDataModal, api.importProxy]] as const)('refreshes partial imports exactly once on close', async (component, command) => {
    command.mockResolvedValue({ ...imported(), account_failed: 1, proxy_failed: 1 })
    const h = harness(component)
    await chooseFiles(h.wrapper, [file('partial.json', bundle())])
    await h.wrapper.get('form').trigger('submit'); await flushPromises()
    expect(h.wrapper.emitted('imported')).toBeUndefined()
    await h.wrapper.get('[data-action="dialog-close"]').trigger('click')
    expect(h.wrapper.emitted('imported')).toHaveLength(1)
    expect(h.wrapper.emitted('close')).toHaveLength(1)
  })

  it('uses explicit action permissions and existing authorization service fields', async () => {
    const h = harness(AccountActionMenu, { show: true, account: accountFixture(), anchorRect: new DOMRect(), canDiagnose: false, canManage: false })
    expect(document.body.textContent).not.toContain('admin.accounts.testConnection')
    expect(document.body.textContent).not.toContain('admin.accounts.refreshToken')
    expect(document.body.textContent).toContain('admin.accounts.viewStats')
    h.actor.value = 'operator-b'; await flushPromises()
    expect(h.wrapper.emitted('close')).toHaveLength(1)
  })
})

