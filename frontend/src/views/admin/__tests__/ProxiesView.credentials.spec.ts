import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'
import Select from '@/components/common/Select.vue'

const { list, create, batchCreate, update, getAllWithCount, exportData, copyToClipboard } = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), batchCreate: vi.fn(), update: vi.fn(), getAllWithCount: vi.fn(), exportData: vi.fn(), copyToClipboard: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { list, create, batchCreate, update, getAllWithCount, exportData } } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({copyToClipboard}) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    ProxyFormFields: false, ProxyBatchInput: false,
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
    DataTable: { props: ['data'], template: '<div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /><slot name="cell-address" :row="row" /></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  } },
})
let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  vi.clearAllMocks()
  list.mockResolvedValue({ items: [{ id: 9, name: 'proxy', protocol: 'http', host: 'proxy.example', port: 8080, username: 'old-user', status: 'active' }], total: 1, pages: 1 })
  getAllWithCount.mockResolvedValue([])
  update.mockResolvedValue({})
  create.mockResolvedValue({})
  batchCreate.mockResolvedValue({ created: 1, skipped: 0 })
})
afterEach(() => wrapper?.unmount())
async function edit() {
  wrapper = mountView(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
}
async function submit() {
  await wrapper.get('#edit-proxy-form').trigger('submit'); await flushPromises()
  expect(update).toHaveBeenCalledTimes(1)
  return update.mock.calls[0][1]
}

describe('proxy credential updates', () => {
  it('copies credentials through the protected export endpoint, not from a cached row', async () => {
    exportData.mockResolvedValue({proxies:[{protocol:'http',host:'proxy.example',port:8080,username:'export-user',password:'export-password'}]})
    wrapper = mountView(); await flushPromises()
    await wrapper.get('button[title="admin.proxies.copyProxyUrl"]').trigger('click'); await flushPromises()
    expect(exportData).toHaveBeenCalledWith({ids:[9]})
    expect(copyToClipboard).toHaveBeenCalledWith('http://export-user:export-password@proxy.example:8080','admin.proxies.urlCopied')
  })
  it('sends an explicit empty username when cleared', async () => {
    await edit()
    const username = wrapper.findAll<HTMLInputElement>('#edit-proxy-form input').find(input => input.element.value === 'old-user')!
    await username.setValue('')
    const payload = await submit()
    expect(payload.username).toBe('')
    expect(payload).not.toHaveProperty('password')
  })

  it('sends an explicit empty password when the user clears the field', async () => {
    await edit()
    await wrapper.get('#edit-proxy-form input[type="password"]').setValue('')
    expect((await submit()).password).toBe('')
  })

  it('omits an untouched password when saving other settings', async () => {
    await edit()
    const payload = await submit()
    expect(payload.username).toBe('old-user')
    expect(payload).not.toHaveProperty('password')
  })

  it('keeps trimming a replacement password', async () => {
    await edit()
    await wrapper.get('#edit-proxy-form input[type="password"]').setValue(' new-password ')
    expect((await submit()).password).toBe('new-password')
  })
})

// These legacy functional cases exercise the migrated full administrator.
vi.mock("@/stores/auth", () => ({ useAuthStore: () => ({ isSuperAdmin: true, isAdmin: true, isSimpleMode: false, can: () => true }) }))

it('creates through the shared native fields with expiry and a backup proxy', async () => {
  getAllWithCount.mockResolvedValue([{ id: 12, name: 'Backup', host: 'backup.test', port: 8080 }])
  wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.createProxy')!.trigger('click')
  const form = wrapper.get('#create-proxy-form')
  expect(form.findAll('label').map(label => label.text())).toEqual([
    'admin.proxies.name', 'admin.proxies.protocol', 'admin.proxies.host', 'admin.proxies.port',
    'admin.proxies.username', 'admin.proxies.password', 'admin.proxies.expiresAt', 'admin.proxies.fallbackMode'
  ])
  await form.get('input[id$="-name"]').setValue('Native')
  await form.get('input[id$="-host"]').setValue('2001:db8::1')
  await form.get('input[type="date"]').setValue('2030-01-20')
  const fallback = wrapper.findAllComponents(Select).find(select => select.props('ariaLabel') === 'admin.proxies.fallbackMode')!
  fallback.vm.$emit('update:modelValue', 'proxy')
  await flushPromises()
  const backup = wrapper.findAllComponents(Select).find(select => select.props('ariaLabel') === 'admin.proxies.backupProxy')!
  expect(backup.props('options')).toEqual([{ label: 'Backup (backup.test:8080)', value: 12 }])
  backup.vm.$emit('update:modelValue', 12)
  await form.trigger('submit')
  await flushPromises()
  expect(create).toHaveBeenCalledWith(expect.objectContaining({
    name: 'Native', host: '2001:db8::1', expires_at: 1895097600, fallback_mode: 'proxy', backup_proxy_id: 12
  }))
})
it('uses the shared quick parser and submits IPv6 credentials without trimming them', async () => {
  wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.createProxy')!.trigger('click')
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.batchAdd')!.trigger('click')
  await wrapper.get('textarea').setValue('socks5h:// user : p%40ss:word @[2001:db8::1]:1080')
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.importProxies')!.trigger('click')
  await flushPromises()
  expect(batchCreate).toHaveBeenCalledWith([{ protocol: 'socks5h', host: '2001:db8::1', port: 1080, username: ' user ', password: ' p%40ss:word ' }])
})
