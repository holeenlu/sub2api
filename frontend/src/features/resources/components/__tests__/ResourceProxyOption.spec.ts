import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ResourceProxySelector from '../ResourceProxySelector.vue'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import type { VendorProxy } from '@/features/supply'
import type { ResourceProxyOption } from '../../panels'

const { testProxy } = vi.hoisted(() => ({ testProxy: vi.fn() }))
vi.mock('@/features/resources/proxies', () => ({ default: { testProxy } }))
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const dialogs: VueWrapper[] = []
const supplierProxy: VendorProxy = Object.freeze({ id: 7, name: 'Supplier exit', protocol: 'socks5', host: 'proxy.example.test', port: 1080, status: 'active', config_version: 3 })
const actualOptions: readonly ResourceProxyOption[] = [supplierProxy]
beforeEach(() => testProxy.mockReset().mockResolvedValue({ success: true, message: 'ok' }))
afterEach(() => dialogs.splice(0).forEach(wrapper => wrapper.unmount()))

describe('actual supplier resource projections', () => {
  it('renders and selects the actual vendor proxy without manufactured credentials or defaults', async () => {
    const wrapper = mount(ResourceProxySelector, { props: { modelValue: null, proxies: actualOptions }, global: { stubs: { Icon: true } } })
    dialogs.push(wrapper)
    await wrapper.get('.select-trigger').trigger('click')
    const option = wrapper.findAll('.select-option').find(option => option.text().includes('Supplier exit'))
    if (!option) throw new Error('Supplier proxy option missing')
    expect(option.text()).toContain('socks5://proxy.example.test:1080')
    await option.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([7])
    expect(Object.keys(supplierProxy)).toEqual(['id', 'name', 'protocol', 'host', 'port', 'status', 'config_version'])
    expect(supplierProxy).not.toHaveProperty('username')
    expect(supplierProxy).not.toHaveProperty('password')
    expect(supplierProxy).not.toHaveProperty('account_count')
  })

  it('tests only the chosen proxy ID, with the real DTO projection untouched', async () => {
    const wrapper = mount(ResourceProxySelector, { props: { modelValue: null, proxies: [supplierProxy] }, global: { stubs: { Icon: true } } })
    dialogs.push(wrapper)
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('button.test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledWith(7)
  })

  it('accepts unknown vendor type strings without converting them to a credential category', async () => {
    const selectedTypes: string[] = ['future-vendor-kind']
    const saveBatch = vi.fn().mockResolvedValue({ success: 1, failed: 0, results: [] })
    const services = editorTestServices()
    const wrapper = mount(BulkEditAccountModal, { props: {
      show: true, accountIds: [42], selectedPlatforms: ['openai'], selectedTypes,
      proxies: [supplierProxy], groups: [], supplierScope: true, allowAuthorize: false, saveBatch
    }, global: { provide: { [resourceEditorServicesKey as symbol]: services }, stubs: {
      BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true, Select: true,
      ResourceProxySelector: true, GroupSelector: true, ConfirmDialog: true, EditAccountModal: true, ModelWhitelistSelector: true
    } } })
    dialogs.push(wrapper)
    expect(wrapper.props('selectedTypes')).toEqual(selectedTypes)
    expect(wrapper.find('#bulk-edit-openai-flatten-namespaces-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-header-override-enabled').exists()).toBe(false)
    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue('6')
    await wrapper.get('form#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(saveBatch).toHaveBeenCalledTimes(1)
    expect(saveBatch.mock.calls[0][1]).toMatchObject({ concurrency: 6 })
    expect(saveBatch.mock.calls[0][1]).not.toHaveProperty('type')
    expect(saveBatch.mock.calls[0][1]).not.toHaveProperty('credentials')
    expect(selectedTypes).toEqual(['future-vendor-kind'])
  })
})

